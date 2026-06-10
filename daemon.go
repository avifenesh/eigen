package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/memory"
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

	gmem, _ := memory.OpenGlobal()
	skills := skill.Discover(skillDirs()...)

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
		for _, in := range host.List() {
			host.Remove(in.ID) // interrupt turns + release resources
		}
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

// daemonControl handles `eigen daemon <status|stop>`; returns true if it
// handled a control subcommand (caller should return).
func daemonControl(sub string) bool {
	switch sub {
	case "status":
		if pid := daemon.RunningPID(daemon.PIDPath()); pid != 0 {
			fmt.Printf("eigen daemon running (pid %d) on %s\n", pid, daemon.SocketPath())
		} else {
			fmt.Println("eigen daemon not running")
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
	}
	return false
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
func runAttach(id string, cfg config.Config) {
	c, err := daemon.Dial(daemon.SocketPath())
	if err != nil {
		fail(err)
	}
	defer c.Close()

	var dir string
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
			id, dir = nid, cwd
		} else {
			id = infos[0].ID // most recent
			dir = infos[0].Dir
		}
	} else {
		for _, in := range mustList(c) {
			if in.ID == id {
				dir = in.Dir
			}
		}
	}
	// Root the view in the session's project dir so @file completion and the
	// transcript's relative paths make sense.
	if dir != "" {
		_ = os.Chdir(dir)
	}
	backend, err := chat.NewRemote(c, id)
	if err != nil {
		fail(err)
	}
	skills := skill.Discover(skillDirs()...)
	mem, _ := memory.Open(dir)
	if _, err := tui.Run(backend, tui.Options{
		Provider:      backend.ProviderName(),
		Model:         backend.ModelID(),
		Memory:        mem,
		Skills:        skills,
		NoSessionFile: true,
	}); err != nil {
		fail(err)
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
	logPath := filepath.Join(home, ".eigen", "daemon.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "daemon")
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
