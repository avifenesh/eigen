package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/app"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/hook"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/mcp"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/remote"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/tui"
)

// runDaemon starts the long-lived session host: the real eigen app. It owns
// agent sessions (each rooted at its own dir), serves views over a Unix
// socket, and keeps running until interrupted. Views (`eigen` windows) attach,
// mirror events, and send input; a session's lifetime is independent of any
// view.
func runDaemon(cfg config.Config) {
	// Refuse to start a second daemon; clean up after a dead one.
	if pid := daemon.RunningPID(daemon.PIDPath()); pid != 0 {
		fmt.Fprintf(os.Stderr, "eigen daemon already running (pid %d)\n", pid)
		return
	}

	// Load the credential snapshot (~/.eigen/daemon.env) into the environment
	// if present. systemd's EnvironmentFile does this for the installed unit,
	// but a daemon spawned directly (ensureDaemon's auto-start, or a REMOTE
	// daemon reached via `ssh host eigen daemon stdio`) needs creds too — else
	// it can't build sessions (no AWS creds → "converse credentials" errors).
	// Existing env vars win (an explicit export overrides the snapshot).
	loadDaemonEnv()

	gmem, _ := memory.OpenGlobal()
	skills := skill.Discover(skillDirs()...)

	// Sweep leftover agent-workspace sandboxes from prior crashes/kills: the
	// workspace daemon double-forks to persist, so a killed eigen can orphan
	// X servers + apps. `workspace cleanup` is the binary's own safety-checked
	// reaper (skips RUNNING workspaces, only removes verified-orphan dirs +
	// process groups). Best-effort, async — never blocks daemon start.
	sweepStaleWorkspaces()

	host := daemon.NewPersistentHost(daemon.SessionsDir())
	// The builder turns a (dir, model) request into a fully-wired agent +
	// resource closer, reusing the same per-session construction as a chat.
	build := func(dir, model string) (*agent.Agent, func(), error) {
		if dir == "" {
			dir, _ = os.Getwd()
		}
		prov := firstNonEmpty(cfg.Provider, "converse")
		mdl := firstNonEmpty(model, cfg.Model)
		deps, err := buildSession(buildParams{
			Dir:       dir,
			Provider:  prov,
			Model:     mdl,
			Perm:      firstNonEmpty(cfg.Perm, "gated"),
			MaxTokens: cfg.MaxTokens,
			Cfg:       cfg,
			Skills:    skills,
			GlobalMem: gmem,
		})
		if err != nil {
			return nil, nil, err
		}
		return deps.Agent, deps.Close, nil
	}

	// Live model switching for daemon sessions: rebuild a provider for the new
	// model id (the same construction the local chat uses).
	host.SetModelSwitcher(func(dir, modelID string) (llm.Provider, llm.Compactor, int, error) {
		p, err := llm.New("", modelID)
		if err != nil {
			return nil, nil, 0, err
		}
		return p, llm.CompactorChain(llm.NewCompactor(smallProvider(p)), llm.NewCompactor(p)), contextBudget(cfg.MaxTokens, "", modelID), nil
	})

	// Auto-title daemon sessions on the small model (same titler as the app's
	// session pages) — names show up in the rail/home shortly after the first
	// message. smallProvider health-checks local endpoints itself.
	titler := session.ProviderTitler{P: titleProvider(nil)}
	host.SetTitler(func(ctx context.Context, head string) (string, error) {
		return titler.Title(ctx, head)
	})

	// Desktop notifier for BACKGROUNDED turns: when the user moves a running
	// turn to the background (leaves the window while it works), no TUI is left
	// to ping them on completion — so the daemon fires the configured notifier.
	if notifier := strings.Fields(notifyCmdline(cfg)); len(notifier) > 0 {
		host.SetNotifier(func(title, body string) {
			args := append(append([]string{}, notifier[1:]...), title+" — "+body)
			c := exec.Command(notifier[0], args...)
			if err := c.Start(); err == nil {
				go func() { _ = c.Wait() }() // fire-and-forget; never block the daemon
			}
		})
	}

	// Resurrect persisted sessions before accepting views: each one rebuilds
	// its agent (rooted at its dir) and resumes its saved history under the
	// same id, so a daemon restart loses nothing.
	if n := host.Restore(build); n > 0 {
		fmt.Fprintf(os.Stderr, "eigen daemon: restored %d session(s)\n", n)
	}

	srv, err := daemon.Listen(daemon.SocketPath(), host, build)
	if err != nil {
		fail(fmt.Errorf("daemon: %w", err))
	}
	if err := daemon.WritePID(daemon.PIDPath()); err != nil {
		fmt.Fprintln(os.Stderr, "eigen daemon: pid file:", err)
	}
	defer daemon.RemovePID(daemon.PIDPath())
	fmt.Fprintf(os.Stderr, "eigen daemon listening on %s (pid %d)\n", daemon.SocketPath(), os.Getpid())

	// Graceful shutdown on SIGINT/SIGTERM: interrupt every session and close.
	// Resource teardown (MCP/LSP subprocesses) can hang, so a watchdog forces
	// exit — a daemon that hangs on shutdown is an orphan with a deleted PID
	// file, unfindable by `eigen daemon stop`.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Fprintln(os.Stderr, "eigen daemon shutting down")
		go func() {
			time.Sleep(5 * time.Second)
			fmt.Fprintln(os.Stderr, "eigen daemon: shutdown timed out, forcing exit")
			daemon.RemovePID(daemon.PIDPath())
			_ = os.Remove(daemon.SocketPath())
			os.Exit(1)
		}()
		// Shutdown ≠ Remove: release resources but KEEP persisted state so
		// the next start restores every session.
		host.Shutdown()
		srv.Close()
		daemon.RemovePID(daemon.PIDPath())
	}()

	if err := srv.Serve(); err != nil {
		// A closed listener (clean shutdown) is not an error to report.
		if !isClosedErr(err) {
			fail(fmt.Errorf("daemon serve: %w", err))
		}
	}
}

// daemonControl handles `eigen daemon <status|stop|prune|install|uninstall>`;
// returns true if it handled a control subcommand (caller should return).
func daemonControl(sub string) bool {
	switch sub {
	case "status":
		label := "default"
		if inst := daemon.Instance(); inst != "" {
			label = inst
		}
		if pid := daemon.RunningPID(daemon.PIDPath()); pid != 0 {
			fmt.Printf("eigen daemon running (instance %s, pid %d) on %s\n", label, pid, daemon.SocketPath())
		} else {
			fmt.Printf("eigen daemon not running (instance %s, socket %s)\n", label, daemon.SocketPath())
		}
		return true
	case "install":
		daemonInstall()
		return true
	case "uninstall":
		daemonUninstall()
		return true
	case "prune":
		// Remove empty (0-message) sessions. Through the daemon when it's up
		// (so in-memory ghosts go too), else straight off disk.
		var pruned []string
		if c, err := daemon.Dial(daemon.SocketPath()); err == nil {
			pruned, err = c.Prune()
			c.Close()
			if err != nil {
				fail(err)
			}
		} else {
			pruned = daemon.PrunePersisted()
		}
		if len(pruned) == 0 {
			fmt.Println("no empty sessions to prune")
		} else {
			fmt.Printf("pruned %d empty session(s): %s\n", len(pruned), strings.Join(pruned, " "))
		}
		return true
	case "stop":
		pid, err := daemon.Stop(daemon.PIDPath())
		if err != nil {
			fail(err)
		}
		if pid == 0 {
			fmt.Println("eigen daemon not running")
		} else {
			fmt.Printf("stopped eigen daemon (pid %d)\n", pid)
			daemon.RemovePID(daemon.PIDPath())
			_ = os.Remove(daemon.SocketPath())
		}
		return true
	case "stdio":
		daemonStdio()
		return true
	}
	return false
}

// daemonStdio bridges this process's stdin/stdout to the local daemon socket:
// one stdio stream == one view connection. It's the transport primitive for
// remote use — `ssh host eigen daemon-stdio` gives a local TUI a pipe to a
// REMOTE daemon (the whole agent loop runs on the remote; the local side is a
// pure view). Mirrors codex's `app-server proxy`.
//
// CONTRACT: only protocol bytes go to STDOUT; everything human-readable
// (errors, logs) goes to STDERR, so ssh's separate stderr channel can't corrupt
// the byte stream the remote client reads.
func daemonStdio() {
	// Ensure a local daemon is up (auto-spawns one, restoring sessions), then
	// connect to its raw socket for byte relaying.
	c, err := ensureDaemon()
	if err != nil {
		fmt.Fprintf(os.Stderr, "eigen daemon-stdio: %v\n", err)
		os.Exit(1)
	}
	c.Close() // we only used ensureDaemon to guarantee the daemon is running

	conn, err := net.Dial("unix", daemon.SocketPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "eigen daemon-stdio: dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Relay both directions; exit as soon as either end closes (the view
	// disconnected, or the daemon went away).
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(conn, os.Stdin); done <- struct{}{} }()
	go func() { _, _ = io.Copy(os.Stdout, conn); done <- struct{}{} }()
	<-done
}

func isClosedErr(err error) bool {
	// A closed listener (clean shutdown) surfaces as "use of closed network
	// connection".
	return err != nil && strings.Contains(err.Error(), "closed")
}

// runAttach connects to the daemon and attaches the RICH chat TUI to a session
// (the same UI as a local chat — the backend seam routes everything over the
// socket). With no id it attaches to the most recently updated session, or
// creates one rooted at the current directory when the daemon has none.
// The window then lives in the view loop: alt+s hops between sessions, h goes
// home to the app — all in this one window, sessions running throughout.
func runAttach(id string, cfg config.Config) {
	c, err := ensureDaemon() // auto-start: persisted sessions restore
	if err != nil {
		fail(err)
	}
	defer c.Close()

	if id == "" {
		infos, lerr := c.List()
		if lerr != nil {
			fail(lerr)
		}
		if len(infos) == 0 {
			// No sessions yet: create one rooted here.
			cwd, _ := os.Getwd()
			nid, nerr := c.New(cwd, "")
			if nerr != nil {
				fail(nerr)
			}
			id = nid
		} else {
			id = infos[0].ID // most recent
		}
	}
	res := attachTUI(c, id, cfg, "")
	continueNav(c, res, cfg)
}

// attachTUI runs one leg of the view loop: attach the rich chat TUI to a
// daemon session, rooted at the session's project dir. /rebuild is handled
// here (it execs onto the new binary and never returns).
func attachTUI(c *daemon.Client, id string, cfg config.Config, task string) tui.Result {
	// Root the view in the session's project dir so @file completion and the
	// transcript's relative paths make sense.
	if dir := sessionDir(c, id); dir != "" {
		_ = os.Chdir(dir)
	}
	backend, err := chat.NewRemote(c, id)
	if err != nil {
		fail(err)
	}
	skills := skill.Discover(skillDirs()...)
	cwd, _ := os.Getwd()
	mem, _ := memory.Open(cwd)
	store, _ := session.Open()
	hookRunner, _ := hook.Load(hookConfigPath())
	res, err := tui.Run(backend, tui.Options{
		InitialTask:   task,
		Provider:      backend.ProviderName(),
		Model:         backend.ModelID(),
		InputMode:     cfg.InputMode,
		Memory:        mem,
		Store:         store,
		Skills:        skills,
		DreamOnIdle:   cfg.DreamOnIdle,
		IdleMinutes:   cfg.IdleMinutes,
		MaxTokens:     cfg.MaxTokens,
		NotifyCmd:     cfg.NotifyCmd,
		Router:        newAutoRouter(cfg.Route, cfg.RouteProviders, firstNonEmpty(cfg.Provider, "converse")),
		HookRunner:    hookRunner,
		NoSessionFile: true,
	})
	if err != nil {
		fail(err)
	}
	if res.Rebuild {
		c.Close()
		daemonRebuildResume(res.BinPath, id) // execs; no return
	}
	return res
}

// continueNav keeps ONE WINDOW navigating after a chat exits with an intent:
// alt+s hop → attach the next session; h home → the app shell, whose choice
// (attach / new chat / resume) feeds back into another chat leg. Sessions
// keep running in the daemon across every hop; only quit ends the loop.
func continueNav(c *daemon.Client, res tui.Result, cfg config.Config) {
	for {
		switch {
		case res.SwitchTo != "":
			res = attachTUI(c, res.SwitchTo, cfg, "")
		case res.OpenApp:
			id, task, ok := appNav(c, cfg)
			if !ok {
				return
			}
			res = attachTUI(c, id, cfg, task)
		default:
			return
		}
	}
}

// appNav opens the app shell from inside the view loop and translates its
// result into the next chat leg: which session to show (creating one for
// "new chat" / store resumes) and an optional initial task (feed starters).
// ok=false means the user quit from the app.
func appNav(c *daemon.Client, cfg config.Config) (id, task string, ok bool) {
	data := app.Load()
	data.Titler = session.ProviderTitler{P: titleProvider(nil)}
	data.Small = titleProvider(nil)
	res, err := app.Run(data)
	if data.Daemon != nil {
		data.Daemon.Close()
	}
	if err != nil {
		fail(err)
	}
	switch res.Action {
	case app.ActionAttach:
		return res.SessionID, "", true
	case app.ActionRemote:
		// Open a session on a REMOTE machine. runRemote runs its own view loop
		// over ssh; when it returns, end this local nav leg.
		runRemoteSession(res.Host, res.SessionID, cfg)
		return "", "", false
	case app.ActionOpenChat:
		dir := res.Dir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		nid, nerr := c.NewSession(dir, "", "", nil)
		if nerr != nil {
			fail(nerr)
		}
		return nid, res.Task, true
	case app.ActionResume:
		// A store session (imported/foreign): seed a NEW daemon session with
		// its history, rooted at its project. Daemon rows attach instead.
		var history []llm.Message
		if store, serr := session.Open(); serr == nil && store.Get(res.SessionID) != nil {
			history, _ = store.Load(res.SessionID)
		}
		dir := res.Dir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		nid, nerr := c.NewSession(dir, "", "", history)
		if nerr != nil {
			fail(nerr)
		}
		return nid, "", true
	}
	return "", "", false // quit
}

// sessionDir returns the project dir a daemon session is rooted at.
func sessionDir(c *daemon.Client, id string) string {
	for _, in := range mustList(c) {
		if in.ID == id {
			return in.Dir
		}
	}
	return ""
}

// daemonRebuildResume is /rebuild for daemon-hosted sessions: the new binary
// is already built, smoke-tested, and swapped into place (the TUI did that).
// Sessions are durable, so the move is: stop the old daemon (state persists),
// then exec `bin attach <id>` — attach auto-starts a daemon ON THE NEW BINARY,
// which restores every session and reattaches to this one. One honest
// limitation: live effort/search switches die with the old provider instance;
// model/perm/goal/history all survive (they're in the session meta).
func daemonRebuildResume(bin, sessionID string) {
	if pid, err := daemon.Stop(daemon.PIDPath()); err == nil && pid != 0 {
		// Wait for the old daemon to exit (its 5s shutdown watchdog forces
		// the issue if MCP teardown hangs). A forced exit skips its cleanup
		// defers, so clear the pid/socket files ourselves.
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) && daemon.RunningPID(daemon.PIDPath()) != 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	daemon.RemovePID(daemon.PIDPath())
	_ = os.Remove(daemon.SocketPath())
	// Carry the instance EXPLICITLY across the exec (flag + env) so the new
	// daemon lands on the same instance — never accidentally production.
	env := os.Environ()
	argv := []string{bin}
	if inst := daemon.Instance(); inst != "" {
		argv = append(argv, "--instance", inst)
		env = append(env, "EIGEN_INSTANCE="+inst)
	}
	argv = append(argv, "attach", sessionID)
	if err := syscall.Exec(bin, argv, env); err != nil {
		fail(fmt.Errorf("exec new build: %w", err))
	}
}

func mustList(c *daemon.Client) []daemon.SessionInfo {
	infos, err := c.List()
	if err != nil {
		return nil
	}
	return infos
}

// ensureDaemon returns a client to a running daemon, starting one if needed.
// The daemon is spawned detached (its own process group, no controlling tty)
// so it outlives this process — the app keeps living when windows close.
func ensureDaemon() (*daemon.Client, error) {
	if c, err := daemon.Dial(daemon.SocketPath()); err == nil {
		return c, nil
	}
	// Not running: spawn `eigen daemon` detached, logging to a file.
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".eigen", "daemon"+daemonInstanceSuffix()+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755) // fresh install: ~/.eigen may not exist yet
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	// Spawn the daemon with the instance EXPLICIT (not relying on inherited
	// env): a dev client must spawn a dev daemon, never accidentally a
	// production one.
	args := []string{"daemon"}
	if inst := daemon.Instance(); inst != "" {
		args = append([]string{"--instance", inst}, args...)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach: survive this process
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, fmt.Errorf("start daemon: %w", err)
	}
	logf.Close()
	_ = cmd.Process.Release()
	// Wait for the socket (restore of persisted sessions can take a moment).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := daemon.Dial(daemon.SocketPath()); err == nil {
			return c, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon did not come up (see %s)", logPath)
}

// credentialEnvKeys are the environment variables the daemon needs that a
// systemd user session won't have (provider credentials + eigen tuning).
// Single source of truth in internal/remote (shared with the app's install).
var credentialEnvKeys = remote.CredentialKeys

// loadDaemonEnv loads ~/.eigen/daemon.env (KEY=VALUE lines, the credential
// snapshot written by `eigen daemon install` / pushed by `eigen remote
// install`) into the process environment. Already-set vars are NOT overwritten
// (an explicit export wins). Best-effort: a missing file is fine. This lets a
// daemon spawned WITHOUT systemd (ensureDaemon auto-start, or a remote daemon
// reached over ssh) still find its credentials.
func loadDaemonEnv() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(home, ".eigen", "daemon.env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" || os.Getenv(k) != "" {
			continue // explicit env wins
		}
		_ = os.Setenv(k, strings.TrimSpace(v))
	}
}

// daemonInstall writes + enables a systemd user unit so the daemon starts at
// login and restarts on failure. Credentials are snapshotted from the CURRENT
// environment into ~/.eigen/daemon.env (chmod 600) — rerun install after
// rotating keys. daemonUninstall reverses it.
func daemonInstall() {
	home, _ := os.UserHomeDir()
	exe, err := os.Executable()
	if err != nil {
		fail(err)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	// Snapshot credentials the unit will need.
	envPath := filepath.Join(home, ".eigen", "daemon.env")
	_ = os.MkdirAll(filepath.Dir(envPath), 0o755)
	var envLines []string
	for _, k := range credentialEnvKeys {
		if v := os.Getenv(k); v != "" {
			envLines = append(envLines, k+"="+v)
		}
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(envLines, "\n")+"\n"), 0o600); err != nil {
		fail(fmt.Errorf("write %s: %w", envPath, err))
	}

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	_ = os.MkdirAll(unitDir, 0o755)
	unit := fmt.Sprintf(`[Unit]
Description=eigen daemon (session host)

[Service]
# rendered by 'eigen daemon install' — rerun after moving the binary
ExecStart=%s daemon
Restart=on-failure
RestartSec=3
EnvironmentFile=-%s
Environment=PATH=%%h/.local/bin:%%h/.cargo/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
`, exe, envPath)
	unitPath := filepath.Join(unitDir, "eigen-daemon.service")
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		fail(err)
	}
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", "eigen-daemon.service"},
	} {
		cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			fail(fmt.Errorf("systemctl %v: %v\n%s", args, err, out))
		}
	}
	fmt.Printf("installed %s (enabled at login)\n", unitPath)
	fmt.Printf("credentials snapshot: %s (rerun install after rotating keys)\n", envPath)
	if daemon.RunningPID(daemon.PIDPath()) != 0 {
		fmt.Println("note: a daemon is already running — it stays; systemd takes over from next login")
		fmt.Println("      (or: eigen daemon stop && systemctl --user start eigen-daemon)")
	} else {
		cmd := exec.Command("systemctl", "--user", "start", "eigen-daemon.service")
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("start failed (%v): %s\n", err, out)
		} else {
			fmt.Println("started eigen-daemon.service")
		}
	}
}

// daemonUninstall disables and removes the systemd user unit.
func daemonUninstall() {
	home, _ := os.UserHomeDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "eigen-daemon.service")
	_ = exec.Command("systemctl", "--user", "disable", "--now", "eigen-daemon.service").Run()
	_ = os.Remove(unitPath)
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	fmt.Println("removed eigen-daemon.service (daemon.env kept; delete ~/.eigen/daemon.env to purge credentials)")
}

// daemonInstanceSuffix mirrors the daemon package's path suffix for the log
// file name ("" default, "-<instance>" otherwise).
func daemonInstanceSuffix() string {
	if n := daemon.Instance(); n != "" {
		return "-" + n
	}
	return ""
}

// runDevCmd implements `eigen dev [args...]`: build the source tree's binary
// and re-exec it on the isolated "dev" instance, so iterating on eigen
// (including /rebuild) never disturbs the production daemon/sessions. Source
// dir: $EIGEN_SRC, else ~/projects/eigen. The freshly-built binary is what
// runs (no version skew with the installed production binary).
func runDevCmd(rest []string) {
	src := os.Getenv("EIGEN_SRC")
	if src == "" {
		home, _ := os.UserHomeDir()
		src = filepath.Join(home, "projects", "eigen")
	}
	if st, err := os.Stat(filepath.Join(src, "go.mod")); err != nil || st.IsDir() {
		fail(fmt.Errorf("eigen dev: no source tree at %s (set EIGEN_SRC)", src))
	}
	gobin := devFindGo()
	if gobin == "" {
		fail(fmt.Errorf("eigen dev: go toolchain not found (PATH=%s)", os.Getenv("PATH")))
	}
	bin := filepath.Join(src, "bin", "eigen")
	fmt.Fprintln(os.Stderr, "eigen dev: building", bin, "…")
	build := exec.Command(gobin, "build", "-o", bin, ".")
	build.Dir = src
	if out, err := build.CombinedOutput(); err != nil {
		fail(fmt.Errorf("eigen dev: build failed: %v\n%s", err, out))
	}
	// Re-exec the fresh binary on the dev instance (explicit flag + env).
	argv := append([]string{bin, "--instance", "dev"}, rest...)
	env := append(os.Environ(), "EIGEN_INSTANCE=dev")
	if err := syscall.Exec(bin, argv, env); err != nil {
		fail(fmt.Errorf("eigen dev: exec %s: %w", bin, err))
	}
}

// devFindGo resolves the go toolchain (PATH, then common install locations) —
// the daemon's minimal PATH often misses an nvm/asdf/local go.
func devFindGo() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".local", "bin", "go"),
		filepath.Join(home, ".local", "go", "bin", "go"),
		filepath.Join(home, "go", "bin", "go"),
		"/usr/local/go/bin/go", "/usr/lib/go/bin/go",
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// sweepStaleWorkspaces runs the agent-workspace binary's own safety-checked
// reaper to clear sandboxes orphaned by a prior crash/kill. The workspace
// daemon persists (double-forks) so it survives a killed eigen; `workspace
// cleanup` removes only stale dirs + verified-orphan process groups and SKIPS
// running workspaces, so it's safe to run unconditionally at daemon start.
// Best-effort and async — it never blocks or fails daemon startup.
func sweepStaleWorkspaces() {
	bin := mcp.WorkspaceBinary()
	if bin == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "workspace", "cleanup")
		_ = cmd.Run() // best-effort; output/errors are not fatal
	}()
}
