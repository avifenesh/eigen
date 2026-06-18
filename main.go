// Command eigen is a coding agent you own end to end.
//
// Usage:
//
//	eigen [--model ID] [--perm gated|auto] "task"
//
// It drives the configured model through a tool-use loop. Today it ships the
// read tool; write/edit/bash/grep/glob follow.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/app"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/dream"
	"github.com/avifenesh/eigen/internal/harness"
	"github.com/avifenesh/eigen/internal/hook"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/lsp"
	"github.com/avifenesh/eigen/internal/mcp"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/observe"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/telegram"
	"github.com/avifenesh/eigen/internal/theme"
	"github.com/avifenesh/eigen/internal/tool"
	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/avifenesh/eigen/internal/tui"
	"github.com/avifenesh/eigen/internal/workflow"
	"github.com/mattn/go-isatty"
)

func main() {
	// Load credentials only from the trusted user config, never from a
	// project-local .env (an untrusted repo must not be able to set the
	// permission posture, provider creds, or model config).
	home, _ := os.UserHomeDir()
	if err := config.LoadEnvFiles(filepath.Join(home, ".eigen", ".env")); err != nil {
		fmt.Fprintln(os.Stderr, "eigen: env:", err)
	}
	// Optional ~/.eigen/config.json supplies defaults; flags/env override it.
	cfg := config.Load()
	// Theme (config.theme): the named palette is selected at package init from
	// EIGEN_THEME, which happens before main() — so if the config picks a
	// non-default theme and the env isn't already set, re-exec ourselves once
	// with EIGEN_THEME set so the whole process (and its styles) initialize on
	// the chosen palette. Cheap, and only when a non-default theme is configured.
	if cfg.Theme != "" && cfg.Theme != theme.Active.Name {
		if _, set := os.LookupEnv("EIGEN_THEME"); !set {
			if exe, err := os.Executable(); err == nil {
				env := append(os.Environ(), "EIGEN_THEME="+cfg.Theme)
				if e := syscall.Exec(exe, os.Args, env); e != nil {
					// Re-exec failed — fall through on the default palette
					// rather than abort; the app still works, just unthemed.
					os.Setenv("EIGEN_THEME", cfg.Theme)
				}
			}
		}
	}
	// Nerd Font tier (config.nerd_font): same story as theme — the icon tier is
	// chosen at package init from EIGEN_NERD_FONT, so re-exec once with it set
	// when the config picks a tier the env hasn't.
	if cfg.NerdFont != "" && cfg.NerdFont != theme.NerdFontMode() {
		if _, set := os.LookupEnv("EIGEN_NERD_FONT"); !set {
			if exe, err := os.Executable(); err == nil {
				env := append(os.Environ(), "EIGEN_NERD_FONT="+cfg.NerdFont)
				if e := syscall.Exec(exe, os.Args, env); e != nil {
					os.Setenv("EIGEN_NERD_FONT", cfg.NerdFont)
				}
			}
		}
	}
	if cfg.TTSCmd != "" {
		if _, set := os.LookupEnv("EIGEN_TTS_CMD"); !set {
			os.Setenv("EIGEN_TTS_CMD", cfg.TTSCmd)
		}
	}
	// Daemon request-timeout override (config.daemon_timeout, seconds). The
	// daemon client reads EIGEN_DAEMON_TIMEOUT lazily per request, so exporting
	// it here (before any daemon op) is enough — no re-exec needed. An explicit
	// env var still wins.
	if cfg.DaemonTimeout > 0 {
		if _, set := os.LookupEnv("EIGEN_DAEMON_TIMEOUT"); !set {
			os.Setenv("EIGEN_DAEMON_TIMEOUT", strconv.Itoa(cfg.DaemonTimeout))
		}
	}
	// Default reasoning effort (config.effort) becomes the per-model default
	// unless a session meta or env var already set it; providers read
	// EIGEN_REASONING_EFFORT at construction and validate against the model's
	// level set.
	if cfg.Effort != "" {
		if _, set := os.LookupEnv("EIGEN_REASONING_EFFORT"); !set {
			os.Setenv("EIGEN_REASONING_EFFORT", cfg.Effort)
		}
	}
	if len(cfg.SkillsDirs) > 0 {
		merged := append(cfg.SkillsDirs, splitNonEmpty(os.Getenv("EIGEN_SKILLS_DIRS"))...)
		os.Setenv("EIGEN_SKILLS_DIRS", strings.Join(merged, ":"))
	}
	// Subtask lifecycle windows (config; 0 = built-in 2-min defaults). Process-
	// global in the agent package; set once at startup.
	agent.SetLifecycle(cfg.FrontWindowMin, cfg.StallIdleMin)

	model := flag.String("model", cfg.Model, "model id, or provider:id ref (e.g. mantle:us.openai.gpt-5.5; default: openai.gpt-5.5)")
	provider := flag.String("provider", firstNonEmpty(os.Getenv("EIGEN_PROVIDER"), cfg.Provider, "mantle"), "provider: mantle|llama|converse|anthropic|grok|glm (usually inferred from the model)")
	perm := flag.String("perm", firstNonEmpty(os.Getenv("EIGEN_PERMISSION"), cfg.Perm, "gated"), "permission posture: gated|auto")
	printMode := flag.Bool("p", false, "print mode: run one task headless (no TUI) and exit")
	flag.BoolVar(printMode, "print", false, "alias for -p")
	promptFile := flag.String("prompt-file", "", "read the task from this file (re-read each run; for cron/systemd automation loops)")
	resumeFile := flag.String("resume", "", "resume a conversation from a transcript file or 'opencode' (auto-detected)")
	continueLatest := flag.Bool("continue", false, "continue the latest eigen session")
	flag.BoolVar(continueLatest, "c", false, "alias for --continue")
	from := flag.String("from", "", "force the transcript source for --resume (claude|codex|pi|hermes|opencode|eigen)")
	sessionID := flag.String("session", "", "opencode session id for --resume opencode (default: latest)")
	maxTokens := flag.Int("max-tokens", cfg.MaxTokens, "context-budget ceiling before compaction (0 = auto from the model's window; capped by min(this, window−headroom))")
	showVersion := flag.Bool("version", false, "print version and exit")
	listSessions := flag.Bool("list", false, "list resumable sessions (id, date, title) and exit")
	listSkills := flag.Bool("list-skills", false, "list discovered skills (name, description) and exit")
	listTools := flag.Bool("list-tools", false, "list available tools (name, posture, description) and exit")
	instanceFlag := flag.String("instance", "", "daemon instance to use (default: production). A named instance (e.g. dev) gets its own socket/sessions/tasks so rebuilding never touches your production sessions.")
	remoteHost := flag.String("remote", "", "attach to an eigen daemon on a remote host over ssh: user@host[:dir] (bootstrap it first with `eigen remote install`)")
	sockPath := flag.String("sock", "", "attach to a daemon at an explicit socket path (e.g. an ssh -L forwarded ~/.eigen/daemon.sock); does NOT auto-spawn a local daemon")
	var wfVars multiFlag
	flag.Var(&wfVars, "var", "workflow variable k=v for `eigen run` (repeatable)")
	var addDirs multiFlag
	flag.Var(&addDirs, "add-dir", "grant the tools an extra working directory beyond the session root (repeatable)")
	var skillPaths multiFlag
	flag.Var(&skillPaths, "skill", "load a skill from an explicit path (a SKILL.md file or a directory containing one), beyond the configured skill dirs (repeatable)")
	flag.Parse()

	// Resolve the daemon instance (flag wins, then $EIGEN_INSTANCE) ONCE, before
	// any daemon path is computed. An invalid name fails closed — silently
	// falling back to production would be dangerous (you'd think you're on dev).
	inst, ok := daemon.ResolveInstance(*instanceFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "eigen: invalid --instance/EIGEN_INSTANCE name (use letters/digits/._- , ≤32 chars)\n")
		os.Exit(2)
	}
	daemon.SetInstance(inst)
	// Propagate to spawned daemons + the rebuild exec via env (set explicitly
	// from the RESOLVED value, not whatever the shell had).
	if inst != "" {
		os.Setenv("EIGEN_INSTANCE", inst)
	}

	// Ref form: --model mantle:us.openai.gpt-5.5 names both in one flag; an
	// explicit tag beats --provider (one field is the source of truth).
	if tag, id := llm.ParseRef(*model); tag != "" {
		*provider, *model = tag, id
	}

	if *showVersion {
		fmt.Println("eigen", llm.Version)
		return
	}

	// `eigen --remote user@host[:dir]`: attach a local view to a REMOTE eigen
	// daemon over ssh (the agent loop runs there; this is a pure view).
	if *remoteHost != "" {
		runRemote(*remoteHost, cfg)
		return
	}

	// `eigen models`: probe providers with credentials and report models the
	// catalog doesn't know yet (read-only; runs before any provider/MCP init).
	if flag.Arg(0) == "models" {
		runModelsCmd()
		return
	}

	// `eigen harness <status|install>`: manage Eigen-bundled helper binaries
	// (computer-use + isolated workspace) from embedded source.
	if flag.Arg(0) == "harness" {
		runHarnessCmd(flag.Arg(1))
		return
	}
	if flag.Arg(0) == "orientation" {
		runOrientationCmd(flag.Args()[1:])
		return
	}
	// `eigen computer-use <status|install>`: manage the built-in Linux
	// computer-use MCP server.
	if flag.Arg(0) == "computer-use" || flag.Arg(0) == "computer" {
		runComputerUseCmd(flag.Arg(1))
		return
	}
	// `eigen workspace <status|build>`: manage the built-in agent-workspace
	// capability (detect/build the binary), then exit.
	if flag.Arg(0) == "workspace" {
		runWorkspaceCmd(flag.Arg(1))
		return
	}
	if flag.Arg(0) == "chrome" {
		runChromeCmd()
		return
	}
	// `eigen dev [args...]`: build the source tree's binary and re-exec it on a
	// SEPARATE "dev" instance (its own daemon/sessions/tasks). Iterating on
	// eigen — including /rebuild — then never touches your production sessions.
	if flag.Arg(0) == "dev" {
		runDevCmd(flag.Args()[1:])
		return
	}

	// `eigen daemon [status|stop]`: run / inspect / stop the long-lived session
	// host (the real app). Windows attach to it as views; sessions keep running
	// with no window.
	if flag.Arg(0) == "daemon" {
		if daemonControl(flag.Arg(1)) {
			return
		}
		runDaemon(cfg)
		return
	}

	// `eigen attach [session-id]`: attach a view to a daemon session (the
	// session runs in the daemon; this window just mirrors + sends input).
	if flag.Arg(0) == "attach" {
		// --sock may appear after `attach` (Go's flag pkg stops at the first
		// positional, so scan the attach args ourselves).
		sub := flag.Args()[1:]
		if sp, id := extractSockFlag(sub); sp != "" {
			runAttachSock(sp, id, cfg)
			return
		}
		if *sockPath != "" {
			runAttachSock(*sockPath, flag.Arg(1), cfg)
			return
		}
		runAttach(flag.Arg(1), cfg)
		return
	}

	// `eigen version`: print version (used by `eigen remote install` to verify
	// a freshly-installed remote binary; mirrors the --version flag).
	if flag.Arg(0) == "version" {
		fmt.Println("eigen", llm.Version)
		return
	}

	// `eigen theme`: print the design-system swatch (every role + glyph +
	// weight) — the living reference from docs/design-system.md.
	if flag.Arg(0) == "theme" {
		fmt.Print(theme.Swatch())
		return
	}

	// `eigen remote <install|...>`: bootstrap/manage eigen on remote hosts.
	if flag.Arg(0) == "remote" {
		runRemoteCmd(flag.Args()[1:])
		return
	}

	skills := skill.Discover(skillDirs()...)
	for _, p := range skillPaths {
		if added, err := skills.AddPath(p); err != nil {
			fmt.Fprintf(os.Stderr, "eigen: --skill %s: %v\n", p, err)
		} else if len(added) == 0 {
			fmt.Fprintf(os.Stderr, "eigen: --skill %s: no new skills loaded (already present or empty)\n", p)
		}
	}
	if *listSkills {
		printSkills(skills)
		return
	}

	// `eigen skill <add|list> ...`: manage skills from the CLI, then exit.
	if flag.Arg(0) == "skill" {
		runSkillCmd(flag.Args()[1:], *provider, *model)
		return
	}

	// `eigen marketplace <add|list|remove|delete|enable|disable|update>` and
	// `eigen plugin <install|list|remove|delete|enable|disable>`: the Tier 27
	// plugin/marketplace layer. User-command only — the agent cannot install
	// plugins (same rule as /add-dir); untrusted bundle code is scanned before
	// install.
	if flag.Arg(0) == "marketplace" {
		runMarketplaceCmd(flag.Args()[1:])
		return
	}
	if flag.Arg(0) == "plugin" {
		runPluginCmd(flag.Args()[1:], *provider, *model)
		return
	}

	if flag.Arg(0) == "telegram" {
		runTelegram(cfg)
		return
	}

	if flag.Arg(0) == "plan" {
		runPlan(cfg, *provider, *model, strings.Join(flag.Args()[1:], " "))
		return
	}

	switch agent.Permission(*perm) {
	case agent.PermGated, agent.PermAuto:
	default:
		fail(fmt.Errorf("invalid --perm %q (want gated|auto)", *perm))
	}

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))
	appRequested := flag.Arg(0) == "app"
	appPage := flag.Arg(1)
	if appRequested {
		// `eigen app [page]` opens the app shell directly at a page. Without this,
		// "app observe" is interpreted as a chat task, which is exactly the kind of
		// app-view affordance mismatch the observability pass is meant to remove.
		task = ""
	}

	// `eigen run <workflow> [--var k=v ...]`: run an authored multi-step
	// workflow headlessly (exit-coded). Captured here; executed in the headless
	// section once the agent is built. The workflow name is flag.Arg(1).
	workflowName := ""
	if flag.Arg(0) == "run" {
		workflowName = flag.Arg(1)
		if workflowName == "" {
			fmt.Fprintln(os.Stderr, "usage: eigen run <workflow> [--var k=v ...]   (workflows live in ~/.eigen/workflows/)")
			os.Exit(2)
		}
		*printMode = true
		task = "" // the workflow supplies the prompts
	}

	// Task sources for automation: --prompt-file (re-read each run, so a
	// cron/systemd loop picks up edited work), else piped stdin when no
	// positional task was given. Both imply headless print mode.
	if *promptFile != "" && !appRequested {
		data, perr := os.ReadFile(*promptFile)
		if perr != nil {
			fail(fmt.Errorf("prompt-file: %w", perr))
		}
		task = strings.TrimSpace(string(data))
		*printMode = true
	} else if !appRequested && task == "" && !isatty.IsTerminal(os.Stdin.Fd()) {
		if data, rerr := io.ReadAll(os.Stdin); rerr == nil {
			if piped := strings.TrimSpace(string(data)); piped != "" {
				task = piped
				*printMode = true
			}
		}
	}

	// --continue is shorthand for --resume eigen (the latest eigen session).
	if *continueLatest && *resumeFile == "" {
		*resumeFile = "eigen"
	}

	// `eigen <path>`: a directory argument roots the chat there (not a task).
	startedInDir := false
	if task != "" {
		if st, err := os.Stat(task); err == nil && st.IsDir() {
			if err := os.Chdir(task); err != nil {
				fail(err)
			}
			task = ""
			startedInDir = true
		}
	}

	// Bare `eigen` in a terminal (no task, no resume, not print mode) opens
	// the APP — the paged shell (home/projects/sessions/config/…) — instead of
	// dropping straight into a chat. `eigen .`, `eigen <path>`, a task, or
	// --resume/-c all bypass it.
	appInteractive := isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
	if (appRequested || (task == "" && !startedInDir)) && *resumeFile == "" && !*printMode && appInteractive {
		appData := app.Load()
		appData.Titler = session.ProviderTitler{P: titleProvider(nil)}
		appData.Small = titleProvider(nil)
		var res app.Result
		var err error
		if appRequested {
			res, err = app.RunPage(appData, appPage)
		} else {
			res, err = app.Run(appData)
		}
		if err != nil {
			fail(err)
		}
		switch res.Action {
		case app.ActionQuit:
			return
		case app.ActionOpenChat:
			if res.Dir != "" {
				if err := os.Chdir(res.Dir); err != nil {
					fail(err)
				}
			}
			// A feed item carries a ready-made task: start the chat with it
			// pre-submitted (the one-keystroke session starter).
			if res.Task != "" {
				task = res.Task
			}
			// fall through to a fresh chat rooted here
		case app.ActionResume:
			*resumeFile = res.SessionID
			// Root the chat in the session's project (the app may have been
			// launched from anywhere).
			if res.Dir != "" {
				_ = os.Chdir(res.Dir)
			}
		case app.ActionAttach:
			// Attach a view to a daemon session: the agent runs in the daemon;
			// this window only mirrors + sends input.
			runAttach(res.SessionID, cfg)
			return
		case app.ActionRemote:
			// Open a session on a REMOTE machine over ssh.
			runRemoteSession(res.Host, res.SessionID, cfg)
			return
		}
	}

	// Restore the live config from a resumed eigen session so the conversation
	// continues exactly as it was (same provider/model/perm/effort/search),
	// unless the user explicitly overrode a flag this run. Only eigen-native
	// sessions carry a sidecar meta; foreign transcripts have none.
	resumedGoal := ""
	resumedTitle := ""
	resumedLoopPrompt := ""
	var resumedLoopEvery time.Duration
	if *resumeFile != "" {
		set := map[string]bool{}
		flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
		metaSrc := *resumeFile
		if metaSrc == "eigen" {
			metaSrc = latestEigenSession()
		}
		if metaSrc != "" && transcript.Detect(metaSrc) == transcript.SourceEigen {
			if meta, ok := transcript.LoadMeta(metaSrc); ok {
				if meta.Provider != "" && !set["provider"] {
					*provider = meta.Provider
				}
				if meta.Model != "" && !set["model"] {
					*model = meta.Model
				}
				if meta.Perm != "" && !set["perm"] {
					*perm = meta.Perm
				}
				// Effort/search are applied via the env vars the providers read at
				// construction (a non-empty env always wins, so an explicit env
				// override still takes precedence over the sidecar).
				if meta.Effort != "" && os.Getenv("EIGEN_REASONING_EFFORT") == "" {
					os.Setenv("EIGEN_REASONING_EFFORT", meta.Effort)
				}
				if meta.Search != "" {
					if os.Getenv("EIGEN_GROK_SEARCH") == "" {
						os.Setenv("EIGEN_GROK_SEARCH", meta.Search)
					}
					if os.Getenv("EIGEN_GLM_SEARCH") == "" {
						os.Setenv("EIGEN_GLM_SEARCH", meta.Search)
					}
				}
				resumedGoal = meta.Goal
				resumedTitle = meta.Title
				resumedLoopPrompt = meta.LoopPrompt
				if meta.LoopEvery != "" {
					if d, derr := time.ParseDuration(meta.LoopEvery); derr == nil {
						resumedLoopEvery = d
					}
				}
			}
		}
	}

	// THE DEFAULT: every interactive chat is a daemon session — "a chat like
	// any other chat". This branch runs BEFORE the in-process agent is built,
	// so no duplicate MCP/LSP servers spin up just to be ignored. The daemon
	// auto-starts if needed; closing the window leaves the session running.
	// EIGEN_NO_DAEMON=1 keeps the in-process agent (needed by /rebuild's
	// exec-replace flow and when hacking on the daemon itself).
	subcommand := flag.Arg(0) == "dream" || flag.Arg(0) == "memory" || flag.Arg(0) == "observe"
	if !*printMode && !subcommand && os.Getenv("EIGEN_NO_DAEMON") == "" &&
		isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd()) {
		if dc, derr := ensureDaemon(); derr == nil {
			// `--resume <daemon id>` (s1, s2, …) attaches to the durable
			// session itself — never forks a copy.
			if *resumeFile != "" {
				for _, in := range mustList(dc) {
					if in.ID == *resumeFile {
						dc.Close()
						runAttach(*resumeFile, cfg)
						return
					}
				}
			}
			store, _ := session.Open()
			history := importResume(store, *resumeFile, *from, *sessionID)
			cwd, _ := os.Getwd()
			sid, nerr := dc.NewSession(cwd, *model, *perm, history)
			if nerr != nil {
				fail(fmt.Errorf("daemon session: %w", nerr))
			}
			// User-granted extra working dirs (--add-dir, repeatable).
			for _, d := range addDirs {
				if root, aerr := dc.AddDir(sid, expandTilde(d)); aerr != nil {
					fmt.Fprintf(os.Stderr, "eigen: --add-dir %s: %v\n", d, aerr)
				} else {
					fmt.Fprintln(os.Stderr, "eigen: added working dir", root)
				}
			}
			backend, berr := chat.NewRemote(dc, sid)
			if berr != nil {
				fail(berr)
			}
			mem, _ := memory.Open("")
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
				MaxTokens:     resolveUserMaxTokens(*maxTokens),
				NotifyCmd:     cfg.NotifyCmd,
				Router:        newAutoRouter(cfg.Route, cfg.RouteProviders, firstNonEmpty(cfg.Provider, "converse")),
				HookRunner:    hookRunner,
				NoSessionFile: true, // the daemon persists
			})
			if err != nil {
				dc.Close()
				fail(err)
			}
			if res.Rebuild {
				// Sessions are durable: restart the daemon on the new binary
				// and reattach to this same session.
				dc.Close()
				daemonRebuildResume(res.BinPath, sid)
			}
			// alt+s hop / h home: keep navigating in THIS window.
			continueNav(dc, res, cfg)
			dc.Close()
			return
		} else {
			fmt.Fprintf(os.Stderr, "eigen: daemon unavailable (%v) — running in-process\n", derr)
		}
	}

	// `eigen observe [summary]`: inspect metadata-only observability logs. Keep it
	// before tool/provider setup so it is fast and never needs credentials/MCP.
	if flag.Arg(0) == "observe" {
		runObserveCmd(flag.Args()[1:])
		return
	}

	policy := tool.DefaultPolicy()
	// User-granted extra working dirs (--add-dir, repeatable). AddRoot
	// re-validates (must be an existing, non-denied dir).
	for _, d := range addDirs {
		if root, err := policy.AddRoot(expandTilde(d)); err != nil {
			fmt.Fprintf(os.Stderr, "eigen: --add-dir %s: %v\n", d, err)
		} else {
			fmt.Fprintln(os.Stderr, "eigen: added working dir", root)
		}
	}
	mem, _ := memory.Open("")
	gmem, _ := memory.OpenGlobal()
	// Sub-agent delegation: the task tool runs a subtask on a fresh session of
	// the same agent (events suppressed; recursion bounded).
	var a *agent.Agent
	// Observability: structured activity log (metadata only) for long-term
	// learning + debugging. Best-effort — a log failure never blocks a run.
	var obsLog *observe.Logger
	if cfg.ObserveEnabled() {
		if lg, err := observe.Open(observe.DefaultPath(), ""); err == nil {
			obsLog = lg
			defer obsLog.Close()
		}
	}
	// Hooks: user commands triggered on lifecycle events (hooks.json).
	hookRunner, herr := hook.Load(hookConfigPath())
	if herr != nil {
		fmt.Fprintln(os.Stderr, "eigen: hooks:", herr)
	}
	if hookRunner != nil && obsLog != nil {
		hookRunner.SetObserver(obsLog.HookObserver())
	}
	// eventChain composes observability + hooks under a front-end sink.
	eventChain := func(next agent.EventSink) agent.EventSink {
		return obsLog.Wrap(hookRunner.Wrap(next, ""))
	}
	// Auto-router (opt-in): per-task model selection, declared early so the
	// review/task tools can capture it; configured below.
	router := newAutoRouter(cfg.Route, cfg.RouteProviders, *provider)
	taskRun := func(ctx context.Context, t string, opts tool.TaskOpts, background bool) (string, error) {
		if a == nil {
			return "", fmt.Errorf("subtasks unavailable")
		}
		if strings.TrimSpace(opts.Role) != "" {
			if _, ok := agent.LookupRole(opts.Role); !ok {
				return "", fmt.Errorf("unknown task role %q (built-ins: %s; installed plugin agents: %s)", opts.Role, strings.Join(agent.RoleNames(), ", "), strings.Join(agent.PluginRoleNames(), ", "))
			}
		}
		aopts := agent.SubtaskOpts{Kind: opts.Kind, Difficulty: opts.Difficulty, Model: opts.Model, Role: opts.Role}
		if background {
			return a.SubtaskBackground(ctx, t, aopts)
		}
		return a.SubtaskWith(ctx, t, aopts)
	}
	taskStatus := func(ctx context.Context, id string, all, verbose bool, tail int) (string, error) {
		if a == nil || a.Bg == nil {
			return "", fmt.Errorf("background tasks unavailable")
		}
		return formatTaskStatus(a.Bg, id, all, verbose, tail), nil
	}
	taskPromote := func(ctx context.Context, id string) (string, error) {
		if a == nil || a.Bg == nil {
			return "", fmt.Errorf("background tasks unavailable")
		}
		return promoteTaskTranscript(a.Bg, id)
	}
	taskGroup := func(ctx context.Context, subs []tool.GroupSubtaskArg, workers int, synthesize string) (string, error) {
		if a == nil {
			return "", fmt.Errorf("task_group unavailable")
		}
		gs := make([]agent.GroupSubtask, len(subs))
		for i, s := range subs {
			gs[i] = agent.GroupSubtask{Task: s.Task, Role: s.Role, Kind: s.Kind, Difficulty: s.Difficulty, Model: s.Model}
		}
		return a.TaskGroup(ctx, gs, workers, synthesize)
	}
	taskGroupMut := func(ctx context.Context, subs []tool.GroupSubtaskArg, workers int) (string, error) {
		if a == nil {
			return "", fmt.Errorf("task_group_mutating unavailable")
		}
		gs := make([]agent.GroupSubtask, len(subs))
		for i, s := range subs {
			gs[i] = agent.GroupSubtask{Task: s.Task, Kind: s.Kind, Difficulty: s.Difficulty, Model: s.Model}
		}
		approve := func(ctx context.Context, summary string, diff []byte) (bool, error) {
			// Auto mode applies without prompting; only a gated session gates
			// the apply (mirrors normal tool gating).
			if a.CurrentPerm() != agent.PermGated || a.Approve == nil {
				return true, nil
			}
			args, _ := json.Marshal(map[string]string{"summary": summary, "diffstat": agent.PatchStat(diff)})
			return a.Approve(ctx, "task_group_mutating (apply)", args)
		}
		return a.TaskGroupMutating(ctx, gs, workers, approve)
	}
	// goalJudge verifies goal-achievement claims and clears the goal on a
	// confirmed verdict. The judge is an INDEPENDENT model: by default the
	// other vendor (GPT judges Claude's claims, Claude judges GPT's — never
	// self-judge, same as review), falling back to the main model if no
	// cross-vendor model is credentialed. EIGEN_JUDGE_MODEL / config judge_model
	// pin a specific judge.
	var judgeProv llm.Provider
	if jm := firstNonEmpty(os.Getenv("EIGEN_JUDGE_MODEL"), cfg.JudgeModel); jm != "" {
		if jp, err := llm.New("", jm); err == nil {
			judgeProv = jp
		} else {
			fmt.Fprintf(os.Stderr, "eigen: judge model %q: %v (falling back to the main model)\n", jm, err)
		}
	}
	goalJudge := func(ctx context.Context, evidence string) (bool, string, error) {
		if a == nil {
			return false, "", fmt.Errorf("goal judging unavailable")
		}
		judge := judgeProv
		if judge == nil {
			// Cross-vendor by default: judge with the other vendor's model.
			author := effectiveModel(*provider, *model)
			cands := llm.AllCredentialedModels()
			if rev := llm.CrossReviewer(author, cands); rev != "" {
				if jp, err := router.providerFor(rev); err == nil {
					judge = jp
				}
			}
		}
		// nil judge → JudgeGoal falls back to the agent's live provider.
		return a.JudgeGoal(ctx, judge, evidence)
	}
	// Cross-vendor reviewer: GPT reviews Claude, Claude reviews GPT (never
	// self-review). Reads the live model id so it tracks /model + routing.
	reviewRun := router.crossReviewer(func() string {
		return effectiveModel(*provider, *model)
	})
	// Adversarial cross-vendor planning: author = active model, adversary =
	// the other vendor. Tracks /model + routing like the reviewer.
	planRun := router.councilRunner(func() string {
		return effectiveModel(*provider, *model)
	})
	// Backgrounded shells (bash background=true / on-demand detach).
	shells := tool.NewShellRegistry()
	defs := []tool.Definition{
		tool.Read(policy),
		tool.List(policy),
		tool.Glob(policy),
		tool.Grep(policy),
		tool.Symbols(policy),
		tool.Tree(policy),
		tool.Diff(policy),
		tool.Write(policy),
		tool.Edit(policy),
		tool.MultiEdit(policy),
		tool.Patch(policy),
		tool.Move(policy),
		tool.BashWithShells(policy, shells, func() <-chan struct{} { return a.BashDetachCh() }),
		tool.BashOutput(shells),
		tool.KillShell(shells),
		tool.Fetch(),
		tool.Todo(),
		tool.Skill(skills),
		tool.Memory(mem, gmem),
		tool.Task(taskRun),
		tool.TaskStatus(taskStatus),
		tool.TaskPromote(taskPromote),
		tool.TaskGroup(taskGroup),
		tool.TaskGroupMutating(taskGroupMut),
		tool.Retrieve(retrieveRunner(wdOrDot())),
		tool.GenerateImage(imageGenRunner(wdOrDot())),
		tool.GoalAchieved(goalJudge),
		tool.Review(reviewRun),
		tool.Plan(planRun),
		tool.WebSearch(), // always available: keyless fallback chain; keyed/SearXNG preferred
	}
	// Plugins: external-command tools defined in plugins.json. A plugin whose
	// name collides with a built-in is skipped (built-ins win).
	builtin := map[string]bool{}
	for _, d := range defs {
		builtin[d.Name] = true
	}
	if plugins, perr := tool.LoadPlugins(pluginPaths()...); perr != nil {
		fmt.Fprintln(os.Stderr, "eigen: plugins:", perr)
	} else {
		for _, p := range plugins {
			if builtin[p.Name] {
				fmt.Fprintf(os.Stderr, "eigen: plugin %q shadows a built-in tool; skipping\n", p.Name)
				continue
			}
			defs = append(defs, p)
			builtin[p.Name] = true
		}
	}
	// MCP: connect to servers in mcp.json and expose their tools.
	mcpDefs, mcpClients, mcpErrs := mcp.LoadTools(context.Background(), mcpConfigPath())
	for _, e := range mcpErrs {
		fmt.Fprintln(os.Stderr, "eigen: mcp:", e)
	}
	defer func() {
		for _, c := range mcpClients {
			_ = c.Close()
		}
	}()
	mcpTokens := 0
	for _, d := range mcpDefs {
		if builtin[d.Name] {
			fmt.Fprintf(os.Stderr, "eigen: mcp tool %q shadows an existing tool; skipping\n", d.Name)
			continue
		}
		d.Niche = true // progressive disclosure: unlock via search_tools (no per-request schema bloat)
		defs = append(defs, d)
		builtin[d.Name] = true
		mcpTokens += (len(d.Description) + len(d.Parameters)) / 4
	}
	// MCP tools are NICHE (progressive disclosure): their schemas are withheld
	// from each request and unlocked on demand via search_tools, so a heavy MCP
	// setup no longer bloats every request. Just note the count.
	if len(mcpDefs) > 12 {
		fmt.Fprintf(os.Stderr, "eigen: mcp: %d tools available (schemas withheld; the model unlocks them with search_tools — no per-request bloat)\n", len(mcpDefs))
	}
	// LSP: language servers from lsp.json provide go-to-definition, references,
	// hover, document symbols, and diagnostics as native tools. Servers start
	// lazily on first use; the manager is kept alive and closed on exit.
	cwd, _ := os.Getwd()
	lspDefs, lspMgr, lspErrs := lsp.LoadTools(cwd, lspConfigPath())
	for _, e := range lspErrs {
		fmt.Fprintln(os.Stderr, "eigen: lsp:", e)
	}
	if lspMgr != nil {
		defer lspMgr.Close()
	}
	for _, d := range lspDefs {
		if builtin[d.Name] {
			fmt.Fprintf(os.Stderr, "eigen: lsp tool %q shadows an existing tool; skipping\n", d.Name)
			continue
		}
		d.Niche = true // disclose via search_tools
		defs = append(defs, d)
		builtin[d.Name] = true
	}
	// search_tools (progressive disclosure): reveal + unlock the niche tools.
	defs = append(defs, tool.SearchTools(func() *tool.Registry {
		if a != nil {
			return a.Tools
		}
		return nil
	}, func(names []string) {
		if a != nil {
			a.UnlockTools(names)
		}
	}))
	registry, err := tool.NewRegistry(defs...)
	if err != nil {
		fail(err)
	}

	if *listTools {
		printTools(registry)
		return
	}

	prov, err := llm.New(*provider, *model)
	if err != nil {
		fail(err)
	}
	// Keep *provider in sync with what New actually built (the catalog may have
	// reconciled e.g. mantle+claude-model → converse), so the status bar, the
	// context budget, the TUI's live provider, and rebuild-resume all agree.
	*provider = llm.ResolveProvider(*provider, *model)

	// `eigen dream`: reflect over recent sessions into project memory, then exit.
	if flag.Arg(0) == "dream" {
		runDream(titleProvider(prov), mem, gmem)
		return
	}

	// `eigen memory <consolidate|show|backups> [--global]`: curate memory.
	if flag.Arg(0) == "memory" {
		runMemoryCmd(flag.Args()[1:], prov, mem, gmem)
		return
	}

	// Compaction summaries go to the small model first (summarization is a
	// task small models do well, and the summary call happens when the context
	// is at its largest/most expensive), falling back to the main provider.
	smallCompactor := llm.NewCompactor(smallProvider(prov))

	// Compose ExtraSystem: skills catalog + the repo's AGENTS.md guidance.
	extraSystem := skills.Catalog()
	guideDir, _ := os.Getwd()
	if g := agentsGuidance(guideDir); g != "" {
		if extraSystem != "" {
			extraSystem += "\n\n"
		}
		extraSystem += g
	}

	a = &agent.Agent{
		Provider:         prov,
		Tools:            registry,
		Perm:             agent.Permission(*perm),
		MaxContextTokens: contextBudget(*maxTokens, *provider, *model),
		Compactor:        llm.CompactorChain(smallCompactor, llm.NewCompactor(prov)),
		ExtraSystem:      extraSystem,
		Memory:           memory.Sections(gmem, mem),
		Goal:             resumedGoal,
		Router:           router.Route,
		ModelProvider:    router.providerFor,
		Bg:               agent.NewBgRegistry(agent.TasksDir()),
		SessionDir:       wdOrDot(),
		WorktreeTools:    worktreeTools,
		Policy:           policy,
		Shells:           shells,
	}

	// Session store: discover all sources (lazy) and title untitled ones in the
	// background with a small/local model.
	store, _ := session.Open()
	if store != nil {
		_ = store.Discover()
		store.TitleUntitled(context.Background(), session.ProviderTitler{P: titleProvider(prov)}, 40)
	}
	if *listSessions {
		printSessions(store)
		return
	}

	// Optionally resume a prior conversation: by store id, the 'eigen'/'opencode'
	// keywords, or a transcript file path.
	history := importResume(store, *resumeFile, *from, *sessionID)

	// Interactive terminal with no -p → the in-process REPL (EIGEN_NO_DAEMON,
	// or the daemon was unavailable).
	interactive := isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
	if !*printMode && interactive {
		res, err := tui.Run(chat.NewLocal(a, nil, *model), tui.Options{
			InitialTask:    task,
			History:        history,
			Store:          store,
			Provider:       *provider,
			Model:          *model,
			InputMode:      cfg.InputMode,
			Memory:         mem,
			Skills:         skills,
			DreamOnIdle:    cfg.DreamOnIdle,
			IdleMinutes:    cfg.IdleMinutes,
			MaxTokens:      resolveUserMaxTokens(*maxTokens),
			SmallCompactor: smallCompactor,
			NotifyCmd:      cfg.NotifyCmd,
			LoopPrompt:     resumedLoopPrompt,
			LoopEvery:      resumedLoopEvery,
			Title:          resumedTitle,
			Router:         router,
			EventWrap:      eventChain,
			HookRunner:     hookRunner,
		})
		if err != nil {
			fail(err)
		}
		if res.Rebuild {
			// Resume exactly as the conversation was: the user may have switched
			// model, permission, effort, or search live, so carry the LIVE config
			// forward (falling back to the launch flags), not the original ones.
			rp := firstNonEmpty(res.Provider, *provider)
			rm := firstNonEmpty(res.Model, *model)
			rperm := firstNonEmpty(res.Perm, *perm)
			// Effort/search are reapplied via the env vars the providers read at
			// construction, so the resumed process rebuilds the same provider state.
			if res.Effort != "" {
				os.Setenv("EIGEN_REASONING_EFFORT", res.Effort)
			}
			if res.Search != "" {
				os.Setenv("EIGEN_GROK_SEARCH", res.Search)
				os.Setenv("EIGEN_GLM_SEARCH", res.Search)
			}
			execResume(res.BinPath, res.SessionPath, rp, rm, rperm)
		}
		return
	}

	// Headless print mode (or piped/non-TTY): one task, stream to stderr,
	// final answer to stdout — scriptable. `eigen run <workflow>` has no
	// positional task (the workflow supplies the prompts).
	if task == "" && workflowName == "" {
		fmt.Fprintln(os.Stderr, "usage: eigen [flags] \"task\"   (bare `eigen` opens the TUI)")
		os.Exit(2)
	}
	a.Approve = cliApprove
	streamed := false
	headlessSink := func(e agent.Event) {
		switch e.Kind {
		case agent.EventTextDelta, agent.EventReasoningDelta:
			fmt.Fprint(os.Stderr, e.Text)
			if e.Kind == agent.EventTextDelta {
				streamed = true
			}
		case agent.EventToolStart:
			fmt.Fprintf(os.Stderr, "\n  step %d → %s\n", e.Step+1, e.ToolName)
		case agent.EventToolResult:
			if e.IsError {
				fmt.Fprintf(os.Stderr, "  ↳ %s: %s\n", e.ToolName, firstLine(e.Result))
			}
		case agent.EventNote:
			fmt.Fprintf(os.Stderr, "\n  note: %s\n", e.Text)
		}
	}
	a.OnEvent = eventChain(headlessSink)
	hookRunner.Fire(hook.Payload{Event: hook.OnSessionStart})
	defer hookRunner.Fire(hook.Payload{Event: hook.OnSessionStop})

	fmt.Fprintf(os.Stderr, "eigen · %s · perm=%s", prov.Name(), *perm)
	if len(history) > 0 {
		fmt.Fprintf(os.Stderr, " · resumed %d msgs", len(history))
	}
	fmt.Fprintln(os.Stderr)

	// Autosave headless runs too, so any `-p` session is resumable, and record
	// the session meta so a later --resume continues with the same config.
	savePath := newSessionPath()
	saveMeta := func() {
		wd, _ := os.Getwd()
		_ = transcript.SaveMeta(savePath, transcript.SessionMeta{
			Dir:      wd,
			Provider: *provider,
			Model:    *model,
			Perm:     *perm,
			Effort:   os.Getenv("EIGEN_REASONING_EFFORT"),
		})
	}
	a.Persist = func(msgs []llm.Message) {
		_ = transcript.Save(savePath, msgs)
		saveMeta()
	}

	// `eigen run <workflow>`: execute an authored multi-step workflow over ONE
	// carried session (each step's prompt sent to the same session, so step N
	// sees prior work). Exit-coded for automation.
	if workflowName != "" {
		runWorkflowHeadless(a, workflowName, parseVars(wfVars))
		return
	}

	// The headless top-level model is explicit too; routing applies inside the
	// agent when it delegates subtasks, not to this orchestrator turn.
	sess := a.NewSession()
	if len(history) > 0 {
		sess = a.Resume(history)
	}
	out, err := sess.Send(context.Background(), task)
	if err != nil {
		fail(err)
	}
	if streamed {
		fmt.Fprintln(os.Stderr)
	}
	fmt.Println(out)
	fmt.Fprintln(os.Stderr, "session saved →", savePath)
}

// firstLine returns the first line of s, truncated, for compact error display.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// cliApprove prompts for a gated mutating tool call. It reads from the
// controlling terminal (/dev/tty), not stdin, so piped input cannot auto-answer
// it, and fails closed when there is no terminal. Arguments are truncated and
// flattened so a tool's payload cannot spoof the prompt.
func cliApprove(ctx context.Context, name string, args json.RawMessage) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, nil // no terminal: fail closed
	}
	defer tty.Close()

	shown := strings.ReplaceAll(string(args), "\n", " ")
	if len(shown) > 200 {
		shown = shown[:200] + "…"
	}
	fmt.Fprintf(tty, "approve %s %s ? [y/N] ", name, shown)
	line, _ := bufio.NewReader(tty).ReadString('\n')
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y"), nil
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitNonEmpty splits a colon-separated list, dropping empties.
func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ":") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// contextBudget returns the token budget before compaction. It is capped by
// min(user setting, model's actual context window minus headroom): an explicit
// --max-tokens flag, else EIGEN_MAX_CONTEXT_TOKENS, else the config's
// max_tokens provide the user ceiling, and the catalog window (via
// llm.ContextBudget) provides the model ceiling — the smaller wins. The user
// ceiling can only ever lower the budget, never push it past what the model
// accepts. With no user setting and an unknown model, a per-provider default
// stands in for the window.
func contextBudget(flagVal int, provider, model string) int {
	userMax := resolveUserMaxTokens(flagVal)
	effective := model
	if effective == "" {
		effective = llm.DefaultModel(provider)
	}
	return llm.ContextBudget(userMax, effective, providerContextDefault(provider))
}

// resolveUserMaxTokens returns the user's context-budget ceiling: the
// --max-tokens flag (or config max_tokens, which seeds the flag default), else
// EIGEN_MAX_CONTEXT_TOKENS. 0 means unset (auto from the model window).
func resolveUserMaxTokens(flagVal int) int {
	if flagVal > 0 {
		return flagVal
	}
	if v := os.Getenv("EIGEN_MAX_CONTEXT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// providerContextDefault is the fallback budget for a provider whose resolved
// model is not in the catalog (no known window). These are already
// headroom-adjusted conversation budgets, not raw windows.
func providerContextDefault(provider string) int {
	switch provider {
	case "llama", "local":
		return 40000
	case "converse", "bedrock-converse", "claude":
		return 180000
	default: // mantle / gpt-5.5 (272k window)
		return 200000
	}
}

// smallProvider picks a small/fast/cheap model for background chores (session
// titling, dreaming, skill vulnerability scans, compaction summaries). A LOCAL
// model is used ONLY when the user opted in (config local_background, passed as
// localBg) AND the local server is up AND READY to serve — otherwise the usual
// small model (grok/haiku) handles it. Falling through on "configured but not
// ready" is deliberate: the local server may be down, or up-but-loading another
// model, in which case background work must not stall on it.
func smallProvider(main llm.Provider) llm.Provider {
	return smallProviderFor(main, localBackgroundEnabled())
}

// smallProviderFor is smallProvider with the local opt-in passed explicitly
// (testable; the package-level smallProvider reads the config).
func smallProviderFor(main llm.Provider, localBg bool) llm.Provider {
	if localBg {
		if base := os.Getenv("EIGEN_LLAMA_BASE_URL"); base != "" && localReady(base) {
			if lp, err := llm.New("llama", os.Getenv("EIGEN_TITLE_MODEL")); err == nil {
				return lp
			}
		}
	}
	// Prefer an explicit small model; else grok composer when credentialed
	// (faster + cheaper + the user's own account, not Bedrock); else Haiku.
	if sm := os.Getenv("EIGEN_SMALL_MODEL"); sm != "" {
		if p, err := llm.New("", sm); err == nil {
			return p
		}
	}
	if llm.ProviderAvailable("grok") {
		if gp, err := llm.New("grok", "grok-composer-2.5-fast"); err == nil {
			return gp
		}
	}
	if hp, err := llm.New("converse", "us.anthropic.claude-haiku-4-5-20251001-v1:0"); err == nil {
		return hp
	}
	return main
}

// localBackgroundEnabled reports the config's local_background opt-in (env
// EIGEN_LOCAL_BACKGROUND overrides for a one-off run).
func localBackgroundEnabled() bool {
	if v := os.Getenv("EIGEN_LOCAL_BACKGROUND"); v != "" {
		return v == "1" || strings.EqualFold(v, "true")
	}
	return config.Load().LocalBackground
}

// localReady reports whether the local llama server is up AND ready to serve —
// not merely that the port is open. A llama-server that's still loading a model
// (or busy chaining another) answers /health with a non-OK status; we require a
// ready signal so background jobs don't stall on a not-yet-serving server.
// Checks /health at the HOST ROOT (llama.cpp serves it there, not under /v1),
// then falls back to /v1/models for servers without /health.
func localReady(base string) bool {
	root := strings.TrimRight(base, "/")
	root = strings.TrimSuffix(root, "/v1")
	client := &http.Client{Timeout: 600 * time.Millisecond}
	// llama.cpp /health: 200 + {"status":"ok"} when ready; 503 while loading.
	if resp, err := client.Get(root + "/health"); err == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK:
			s := strings.ToLower(string(body))
			// Ready unless the body explicitly says loading/error.
			return !strings.Contains(s, "loading") && !strings.Contains(s, "error")
		case resp.StatusCode == http.StatusNotFound:
			// No /health endpoint — fall through to the /v1/models probe below.
		default:
			// 503 (loading) or any other status → up but not serving yet.
			return false
		}
	}
	// No /health (or no endpoint): accept a 200 from /v1/models as "serving".
	if resp, err := client.Get(strings.TrimRight(base, "/") + "/models"); err == nil {
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	return false
}

// titleProvider is retained as an alias for smallProvider (session titling uses
// the same small model as the other background chores).
func titleProvider(main llm.Provider) llm.Provider { return smallProvider(main) }

// notifyCmdline returns the external desktop-notifier command (config notify_cmd,
// else EIGEN_NOTIFY_CMD), empty when none is configured.
func notifyCmdline(cfg config.Config) string {
	if cfg.NotifyCmd != "" {
		return cfg.NotifyCmd
	}
	return os.Getenv("EIGEN_NOTIFY_CMD")
}

// printSessions lists resumable sessions newest-first for the headless --list.
func printSessions(store *session.Store) {
	if store == nil {
		return
	}
	for _, m := range store.List() {
		title := m.Title
		if title == "" {
			title = "(untitled)"
		}
		when := time.Unix(0, m.Updated).Format("2006-01-02 15:04")
		fmt.Printf("%s  %-16s  %-8s  %s\n", m.ID, when, m.Source, title)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "eigen: "+err.Error())
	os.Exit(1)
}

func runObserveCmd(args []string) {
	cmd := "summary"
	rest := args
	if len(args) > 0 {
		cmd = args[0]
		rest = args[1:]
	}
	switch cmd {
	case "summary", "stats", "":
		limit := 5000
		for _, a := range rest {
			if strings.HasPrefix(a, "--limit=") {
				if n, err := strconv.Atoi(strings.TrimPrefix(a, "--limit=")); err == nil && n >= 0 {
					limit = n
				}
			}
		}
		path := observe.DefaultPath()
		s, err := observe.ReadSummary(path, limit)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("no observability log yet at " + path)
				return
			}
			fail(fmt.Errorf("observe summary: %w", err))
		}
		fmt.Println(observe.FormatSummary(s))
	default:
		fmt.Fprintln(os.Stderr, "usage: eigen observe [summary] [--limit=N]")
		os.Exit(2)
	}
}

// skillDirs returns the directories scanned for SKILL.md skills: the per-user
// store, the current project, and any colon-separated EIGEN_SKILLS_DIRS.
func skillDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".eigen", "skills"),
		filepath.Join(".eigen", "skills"),
	}
	if extra := os.Getenv("EIGEN_SKILLS_DIRS"); extra != "" {
		dirs = append(dirs, strings.Split(extra, ":")...)
	}
	return dirs
}

// pluginPaths returns the plugins.json files scanned for external-command
// tools: the per-user store and the current project.
func pluginPaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".eigen", "plugins.json"),
		filepath.Join(".eigen", "plugins.json"),
	}
}

// mcpConfigPath returns the project mcp.json if present, else the per-user one.
func mcpConfigPath() string {
	if _, err := os.Stat(filepath.Join(".eigen", "mcp.json")); err == nil {
		return filepath.Join(".eigen", "mcp.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "mcp.json")
}

// lspConfigPath returns the project lsp.json if present, else the per-user one.
func lspConfigPath() string {
	if _, err := os.Stat(filepath.Join(".eigen", "lsp.json")); err == nil {
		return filepath.Join(".eigen", "lsp.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "lsp.json")
}

// runDream reflects over the most recent eigen sessions and appends any distilled
// learnings to the current project's memory.
// runMemoryCmd implements `eigen memory <consolidate|show|backups>`: curating
// the project memory file. Consolidation rewrites the append-only notes via the
// model (dedup, supersession, contradiction resolution), shows the diff, and
// asks for confirmation before writing; a timestamped backup is always taken.
func runMemoryCmd(args []string, prov llm.Provider, mem, gmem *memory.Store) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	// --global targets the cross-project store; default is the project store.
	store := mem
	rest := make([]string, 0, len(args))
	for i, a := range args {
		if i == 0 {
			continue // the subcommand
		}
		if a == "--global" || a == "-g" {
			if gmem != nil {
				store = gmem
			}
			continue
		}
		rest = append(rest, a)
	}
	mem = store
	args = append([]string{sub}, rest...)
	switch sub {
	case "show", "":
		content := mem.Read()
		if strings.TrimSpace(content) == "" {
			fmt.Printf("no memory yet at %s\n", mem.Path())
			return
		}
		fmt.Printf("%s (%d bytes)\n\n%s", mem.Path(), len(content), content)
	case "backups":
		baks := mem.Backups()
		if len(baks) == 0 {
			fmt.Println("no backups")
			return
		}
		for _, b := range baks {
			fmt.Println(b)
		}
	case "consolidate":
		yes := false
		for _, a := range args[1:] {
			if a == "--yes" || a == "-y" {
				yes = true
			}
		}
		current := mem.Read()
		if strings.TrimSpace(current) == "" {
			fmt.Println("no memory to consolidate")
			return
		}
		fmt.Fprintf(os.Stderr, "consolidating %s (%d bytes) with %s…\n", mem.Path(), len(current), prov.Name())
		out, err := dream.Consolidate(context.Background(), prov, current)
		if err != nil {
			fail(fmt.Errorf("memory consolidate: %w", err))
		}
		// Show the proposed change as a unified diff (best-effort via git).
		showMemoryDiff(current, out)
		fmt.Fprintf(os.Stderr, "\n%d bytes → %d bytes. ", len(current), len(out))
		if !yes {
			fmt.Fprint(os.Stderr, "apply? [y/N] ")
			var ans string
			fmt.Scanln(&ans)
			if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
				fmt.Println("aborted; memory unchanged")
				return
			}
		}
		if err := mem.Rewrite(out); err != nil {
			fail(fmt.Errorf("memory consolidate: %w", err))
		}
		fmt.Printf("consolidated %s (backup kept: %s)\n", mem.Path(), lastBackup(mem))
	default:
		fmt.Fprintln(os.Stderr, "usage: eigen memory [show|backups|consolidate [--yes]] [--global]")
		os.Exit(2)
	}
}

// showMemoryDiff prints a unified diff between old and new memory contents,
// via `git diff --no-index` when available, else a crude before/after dump.
func showMemoryDiff(oldC, newC string) {
	dir, err := os.MkdirTemp("", "eigen-mem-diff")
	if err == nil {
		defer os.RemoveAll(dir)
		oldP := filepath.Join(dir, "memory.old.md")
		newP := filepath.Join(dir, "memory.new.md")
		if os.WriteFile(oldP, []byte(oldC), 0o600) == nil && os.WriteFile(newP, []byte(newC), 0o600) == nil {
			cmd := exec.Command("git", "diff", "--no-index", "--color", oldP, newP)
			cmd.Stdout = os.Stdout
			cmd.Stderr = io.Discard
			_ = cmd.Run() // exit status 1 just means "files differ"
			return
		}
	}
	fmt.Println("--- proposed memory ---")
	fmt.Println(newC)
}

// lastBackup returns the most recent backup path (or "none").
func lastBackup(mem *memory.Store) string {
	baks := mem.Backups()
	if len(baks) == 0 {
		return "none"
	}
	return baks[len(baks)-1]
}

// newMemoryPipeline wires the dream package's model-facing steps into a
// memory.Pipeline for the given scope (avoids a memory→dream import cycle).
func newMemoryPipeline(prov llm.Provider, mem *memory.Store, idx *memory.Index) *memory.Pipeline {
	return &memory.Pipeline{
		Store: mem,
		Index: idx,
		Stage1: func(ctx context.Context, sessionID, transcript string) (memory.Stage1Result, bool, error) {
			r, ok, err := dream.Stage1(ctx, prov, transcript)
			if err != nil || !ok {
				return memory.Stage1Result{}, false, err
			}
			when := time.Now()
			return memory.Stage1Result{
				RawMemory:      r.RawMemory(sessionID, when),
				RolloutSummary: r.Markdown(sessionID, when),
				RolloutSlug:    r.Slug(),
				Outcome:        r.Outcome,
			}, true, nil
		},
		Consolidate: func(ctx context.Context, current string) (string, error) {
			return dream.Consolidate(ctx, prov, current)
		},
		Summarize: func(ctx context.Context, memText string) (string, error) {
			return dream.Summarize(ctx, prov, memText)
		},
	}
}

func memoryScopeKey(mem *memory.Store) string {
	if mem == nil {
		return ""
	}
	if mem.IsGlobal() {
		return "global"
	}
	return filepath.Base(mem.Dir())
}

func refreshMemorySummary(ctx context.Context, prov llm.Provider, mem *memory.Store, idx *memory.Index) (bool, error) {
	if mem == nil {
		return false, nil
	}
	pipe := newMemoryPipeline(prov, mem, idx)
	if idx == nil {
		if did, err := pipe.MaybeConsolidate(ctx, true); err != nil {
			return false, err
		} else if did {
			return pipe.RegenSummary(ctx)
		}
		return pipe.RegenSummary(ctx)
	}
	if err := idx.Enqueue(memory.JobConsolidate, memoryScopeKey(mem), "scope"); err != nil {
		return false, err
	}
	if err := idx.Enqueue(memory.JobSummary, memoryScopeKey(mem), "scope"); err != nil {
		return false, err
	}
	report, err := pipe.RunQueued(ctx, 4)
	return strings.Contains(report, "memory_summary.md"), err
}

func runDream(prov llm.Provider, mem, gmem *memory.Store) {
	paths := recentEigenSessions(8)
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "eigen: dream: no eigen sessions to reflect on")
		return
	}
	idx, err := memory.OpenIndex()
	if err != nil {
		fail(fmt.Errorf("dream: open memory index: %w", err))
	}
	defer idx.Close()
	pipe := newMemoryPipeline(prov, mem, idx)

	// Build the session list (id + watermark so unchanged sessions are skipped).
	var sessions []memory.Session
	var transcripts []string // kept for skill synthesis below
	for _, p := range paths {
		msgs, lerr := transcript.Load(p)
		if lerr != nil {
			continue
		}
		t := dream.RenderSession(msgs)
		if t == "" {
			continue
		}
		transcripts = append(transcripts, t)
		id := strings.TrimSuffix(filepath.Base(p), ".eigen.jsonl")
		wm := int64(0)
		if fi, e := os.Stat(p); e == nil {
			wm = fi.ModTime().Unix() ^ fi.Size()
		}
		sessions = append(sessions, memory.Session{ID: id, Transcript: t, Watermark: wm})
	}

	report, rerr := pipe.Run(context.Background(), sessions)
	if report == "" {
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "eigen: dream: reflection failed (%v) — the small model may be unavailable; set EIGEN_SMALL_MODEL to a working model\n", rerr)
		} else {
			fmt.Fprintln(os.Stderr, "eigen: dream: nothing new worth remembering")
		}
	} else {
		fmt.Printf("dreamed: %s → %s\n", report, mem.Dir())
	}

	// Skill synthesis: PROPOSE a reusable skill (never auto-install) when the
	// sessions show recurring, generalizable friction. Feed the structured
	// rollout summaries (incl. their FAILURES sections) so synthesis sees
	// patterns across sessions, not just one transcript. The user accepts via
	// `eigen skill accept <name>`.
	corpus := mem.RawSummaries(12)
	if len(corpus) == 0 {
		corpus = transcripts
	}
	if gmem != nil && len(corpus) > 0 {
		if notes, err := dream.DistillGlobal(context.Background(), prov, corpus, gmem.Read()); err == nil && len(notes) > 0 {
			for _, n := range notes {
				_ = gmem.Append(n)
			}
			if did, serr := refreshMemorySummary(context.Background(), prov, gmem, idx); serr == nil && did {
				fmt.Printf("global memory: %d new note(s), regenerated memory_summary.md → %s\n", len(notes), gmem.Dir())
			} else {
				fmt.Printf("global memory: %d new note(s) → %s\n", len(notes), gmem.Dir())
			}
			memory.CommitMemory(fmt.Sprintf("dream: global profile — %d new", len(notes)))
		}
	}
	if draft, ok, serr := dream.SynthesizeSkill(context.Background(), prov, corpus); serr == nil && ok {
		if path, werr := skill.Propose(draft.Name, draft.Description, draft.Body); werr == nil && path != "" {
			fmt.Printf("proposed skill %q → %s\n  review: eigen skill proposed · accept: eigen skill accept %s\n", draft.Name, path, draft.Name)
		}
	}
}

// recentEigenSessions returns up to n newest eigen session file paths.
func recentEigenSessions(n int) []string {
	home, _ := os.UserHomeDir()
	matches, _ := filepath.Glob(filepath.Join(home, ".eigen", "sessions", "*.eigen.jsonl"))
	sort.Slice(matches, func(i, j int) bool {
		fi, e1 := os.Stat(matches[i])
		fj, e2 := os.Stat(matches[j])
		if e1 != nil || e2 != nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})
	if len(matches) > n {
		matches = matches[:n]
	}
	return matches
}

// printSkills lists discovered skills for --list-skills.
func printSkills(set *skill.Set) {
	skills := set.List()
	if len(skills) == 0 {
		fmt.Fprintln(os.Stderr, "no skills found (looked in:", strings.Join(skillDirs(), ", ")+")")
		return
	}
	for _, s := range skills {
		fmt.Printf("%-24s %s\n", s.Name, s.Description)
	}
}

// userSkillsDir is where `eigen skill add` installs by default: ~/.eigen/skills.
func userSkillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "skills")
}

// runSkillCmd implements `eigen skill <add|list> ...`.
//
//	eigen skill list
//	eigen skill add <path | owner/repo[/subdir][@ref]> [--name X] [--force] [--overwrite] [--no-scan]
//
// A skill pulled from GitHub (or a path) is scanned by the small "haiku" model
// for content that would be dangerous for the agent to follow; a RISKY verdict
// aborts unless --force.
func runSkillCmd(args []string, provider, model string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: eigen skill <add|list> …")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		printSkills(skill.Discover(skillDirs()...))
		return
	case "add", "install":
		// Accept flags in any position (Go's flag pkg stops at the first
		// positional, so split the source out first).
		src, rest := splitSource(args[1:])
		fs := flag.NewFlagSet("skill add", flag.ExitOnError)
		name := fs.String("name", "", "override the skill name")
		force := fs.Bool("force", false, "install even if the security scan flags it")
		overwrite := fs.Bool("overwrite", false, "replace an existing skill of the same name")
		noScan := fs.Bool("no-scan", false, "skip the vulnerability scan (not recommended)")
		_ = fs.Parse(rest)
		if src == "" {
			fmt.Fprintln(os.Stderr, "usage: eigen skill add <path | owner/repo[/subdir][@ref]> [--name X] [--force] [--overwrite] [--no-scan]")
			os.Exit(2)
		}

		opts := skill.InstallOptions{
			Dir:       userSkillsDir(),
			Name:      *name,
			Force:     *force,
			Overwrite: *overwrite,
		}
		// The vulnerability scan uses the small model, unless disabled.
		if !*noScan {
			prov, err := llm.New(provider, model)
			if err != nil {
				fail(fmt.Errorf("skill add: %w", err))
			}
			opts.Scanner = skill.ProviderScanner{P: smallProvider(prov)}
		}

		res, err := installSkill(src, opts)
		if err != nil {
			fail(fmt.Errorf("skill add: %w", err))
		}
		if !res.Scan.Safe {
			fmt.Printf("⚠ installed %q despite scan flags:\n", res.Name)
			for _, r := range res.Scan.Reasons {
				fmt.Println("  - " + r)
			}
		} else if opts.Scanner != nil {
			fmt.Printf("✓ scan clean — installed %q → %s\n", res.Name, res.Path)
		} else {
			fmt.Printf("installed %q → %s (scan skipped)\n", res.Name, res.Path)
		}
		return
	case "proposed", "proposals":
		props := skill.Proposals()
		if len(props) == 0 {
			fmt.Println("no proposed skills (dreaming proposes them from recurring friction)")
			return
		}
		fmt.Printf("proposed skills (accept: eigen skill accept <name>):\n")
		for _, p := range props {
			fmt.Printf("  %s — %s\n    %s\n", p.Name, p.Description, p.Path)
		}
		return
	case "accept":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: eigen skill accept <name>")
			os.Exit(2)
		}
		path, err := skill.Accept(args[1])
		if err != nil {
			fail(fmt.Errorf("skill accept: %w", err))
		}
		fmt.Printf("accepted skill %q → %s (now active)\n", args[1], path)
		return
	case "reject":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: eigen skill reject <name>")
			os.Exit(2)
		}
		if err := skill.Reject(args[1]); err != nil {
			fail(fmt.Errorf("skill reject: %w", err))
		}
		fmt.Printf("rejected proposed skill %q\n", args[1])
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown skill subcommand %q (want: add | list | proposed | accept | reject)\n", args[0])
		os.Exit(2)
	}
}

// installSkill dispatches to a path or GitHub install based on the source: a
// source that exists on disk is treated as a path; otherwise it is parsed as a
// GitHub owner/repo reference.
func installSkill(src string, opts skill.InstallOptions) (skill.Installed, error) {
	if _, err := os.Stat(src); err == nil {
		return skill.InstallFromPath(context.Background(), src, opts)
	}
	ref, err := skill.ParseGitHubRef(src)
	if err != nil {
		return skill.Installed{}, err
	}
	return skill.InstallFromGitHub(context.Background(), ref, skill.DefaultFetcher, opts)
}

// splitSource separates the first non-flag argument (the skill source) from the
// remaining flag arguments, so flags may appear before or after the source.
func splitSource(args []string) (src string, rest []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			src = a
			rest = append(rest, args[:i]...)
			rest = append(rest, args[i+1:]...)
			return src, rest
		}
	}
	return "", args
}

// printTools lists registered tools for --list-tools.
func printTools(r *tool.Registry) {
	for _, d := range r.Definitions() {
		posture := "mutating"
		if d.ReadOnly {
			posture = "read-only"
		}
		fmt.Printf("%-12s %-10s %s\n", d.Name, posture, d.Description)
	}
}

// execResume replaces the running process with the already-built-and-validated
// binary, resuming the saved conversation — the success half of live-replace.
// (The build + smoke-test + fence happen in the TUI so a failed build never
// kills the running session.)
func execResume(bin, sessionPath, provider, model, perm string) {
	argv := []string{bin, "--resume", sessionPath, "--provider", provider, "--perm", perm}
	if model != "" {
		argv = append(argv, "--model", model)
	}
	if err := syscall.Exec(bin, argv, os.Environ()); err != nil {
		fail(fmt.Errorf("exec new build: %w", err))
	}
}

// newSessionPath returns a fresh timestamped eigen session file path.
func newSessionPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "sessions")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, time.Now().Format("20060102-150405")+".eigen.jsonl")
}

// latestEigenSession returns the most recently modified eigen session file.
func latestEigenSession() string {
	home, _ := os.UserHomeDir()
	matches, _ := filepath.Glob(filepath.Join(home, ".eigen", "sessions", "*.eigen.jsonl"))
	var newest string
	var newestMod int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().UnixNano() > newestMod {
			newestMod, newest = fi.ModTime().UnixNano(), m
		}
	}
	return newest
}

// runModelsCmd lists the curated catalog, then probes each credentialed
// provider for models the catalog doesn't know yet ("eigen models").
func runModelsCmd() {
	fmt.Println("catalog (curated; /model <id> to use):")
	for _, mi := range llm.Models() {
		win := ""
		if mi.ContextWindow > 0 {
			win = fmt.Sprintf("%dk", mi.ContextWindow/1000)
		}
		fmt.Printf("  %-44s %-10s %s\n", mi.ID, mi.Provider, win)
	}
	fmt.Println("\nprobing providers for new models…")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	found := false
	for _, d := range llm.Discover(ctx) {
		if d.Err != nil {
			fmt.Printf("  %s: error: %v\n", d.Provider, d.Err)
			continue
		}
		if len(d.New) == 0 {
			fmt.Printf("  %s: no new models (%d known)\n", d.Provider, len(d.Known))
			continue
		}
		found = true
		fmt.Printf("  %s: %d new model(s) not in the catalog:\n", d.Provider, len(d.New))
		for _, id := range d.New {
			fmt.Printf("    %s\n", id)
		}
	}
	if found {
		fmt.Println("\nnew models can be used directly (--model <id> or /model <id>);")
		fmt.Println("add catalog entries (internal/llm/catalog.go) for window/caching/thinking metadata.")
	}
}

// effectiveModel resolves the model id for a (provider, model) pair, filling in
// the provider's default when model is empty.
func effectiveModel(provider, model string) string {
	if model != "" {
		return model
	}
	return llm.DefaultModel(provider)
}

// hookConfigPath returns the project hooks.json if present, else the per-user one.
func hookConfigPath() string {
	if _, err := os.Stat(filepath.Join(".eigen", "hooks.json")); err == nil {
		return filepath.Join(".eigen", "hooks.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "hooks.json")
}

func runHarnessCmd(sub string) {
	switch sub {
	case "", "status":
		fmt.Println("eigen harness helpers:")
		reportHelper("computer-use", mcp.ComputerUseBinary(), "computer-use-linux", "computer_use MCP server")
		reportHelper("workspace", mcp.WorkspaceBinary(), "agent-workspace-linux", "workspace MCP server")
		reportChromeHelper()
		reportOrientationHelper()
		fmt.Println("install: eigen harness install  # installs orientation, Chrome connector, and bundled desktop helpers")
	case "install", "build":
		installOrientationHarness()
		installChromeHarness()
		installHarnessComponent("computer-use")
		installHarnessComponent("workspace")
		fmt.Println("harness helpers installed. Restart eigen/daemon to auto-register their MCP tools.")
	default:
		fmt.Fprintf(os.Stderr, "usage: eigen harness <status|install>\n")
		os.Exit(2)
	}
}

func reportHelper(label, path, binary, desc string) {
	if path != "" {
		fmt.Printf("  %s: available → %s (%s)\n", label, path, desc)
		return
	}
	fmt.Printf("  %s: not installed (bundled source available; installs %s)\n", label, binary)
}

func reportChromeHelper() {
	if harness.ChromeBridgeInstalled() {
		fmt.Printf("  chrome: available → %s (connector-only bridge)\n", harness.ChromeBridgeMCPScript())
		return
	}
	fmt.Println("  chrome: not installed (connector-only bridge source bundled; install writes native host + extension files)")
}

func reportOrientationHelper() {
	if harness.OrientationInstalled() {
		fmt.Printf("  orientation: available → %s (history/provenance engine)\n", harness.OrientationHome())
		return
	}
	fmt.Println("  orientation: not installed (native Go capability available; install writes wrapper + hooks)")
}

func installOrientationHarness() {
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	dst := filepath.Join(home, ".local", "bin")
	if err := harness.InstallOrientation(exe, dst); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "installed orientation → %s\n", harness.OrientationHome())
	if err := harness.InstallOrientationHooks(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "orientation hooks not installed:", err)
	}
}

func installChromeHarness() {
	extensionDir, manifests, extensionID, err := harness.InstallChromeBridge()
	if err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "installed chrome connector → %s\n", harness.ChromeBridgeHome())
	fmt.Fprintf(os.Stderr, "chrome extension id: %s\n", extensionID)
	for _, p := range manifests {
		fmt.Fprintf(os.Stderr, "native host manifest: %s\n", p)
	}
	fmt.Fprintf(os.Stderr, "load unpacked extension from: %s\n", extensionDir)
}

func installHarnessComponent(name string) {
	c := harness.Components[name]
	fmt.Fprintf(os.Stderr, "building %s from Eigen-bundled source…\n", c.Description)
	home, _ := os.UserHomeDir()
	dst := filepath.Join(home, ".local", "bin")
	if err := harness.Install(context.Background(), name, dst); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "installed %s → %s\n", name, dst)
}

// runComputerUseCmd implements `eigen computer-use <status|install>`: the
// built-in Linux Computer Use MCP server. The source is embedded in eigen; Rust
// is required only when explicitly installing the helper binary.
func runOrientationCmd(args []string) {
	if err := harness.RunOrientation(context.Background(), args); err != nil {
		fail(err)
	}
}

func runComputerUseCmd(sub string) {
	switch sub {
	case "", "status":
		if bin := mcp.ComputerUseBinary(); bin != "" {
			fmt.Println("computer use: available →", bin)
			fmt.Println("(auto-registered as the `computer_use` MCP server; real Linux desktop control)")
		} else {
			fmt.Println("computer use: not installed")
			fmt.Println("install: `eigen computer-use install` or `eigen harness install` (builds Eigen-bundled source)")
		}
	case "install", "build":
		installHarnessComponent("computer-use")
	default:
		fmt.Fprintf(os.Stderr, "usage: eigen computer-use <status|install>\n")
		os.Exit(2)
	}
}

// runWorkspaceCmd implements `eigen workspace <status|install>`: the built-in
// agent-workspace capability (isolated Linux desktop / computer-use). status
// reports whether the binary is present; install builds Eigen's embedded
// agent-workspace-linux source into ~/.local/bin.
func runWorkspaceCmd(sub string) {
	switch sub {
	case "", "status":
		if bin := mcp.WorkspaceBinary(); bin != "" {
			fmt.Println("agent workspace: available →", bin)
			fmt.Println("(auto-registered as the `workspace` MCP server; 27 curated tools)")
		} else {
			fmt.Println("agent workspace: not installed")
			fmt.Println("install: `eigen workspace install` or `eigen harness install` (builds Eigen-bundled source)")
		}
	case "install", "build":
		installHarnessComponent("workspace")
	default:
		fmt.Fprintf(os.Stderr, "usage: eigen workspace <status|install>\n")
		os.Exit(2)
	}
}

// runChromeCmd implements `eigen chrome <status|install>` for the connector-only
// Chrome bridge (extension + native messaging host + MCP connector, no chat UI).
func runChromeCmd() {
	sub := flag.Arg(1)
	switch sub {
	case "", "status":
		script, node := mcp.ChromeBridge()
		if script == "" {
			fmt.Println("chrome connector: not installed")
			fmt.Println("install: `eigen chrome install` or `eigen harness install`")
			return
		}
		if node == "" {
			fmt.Println("chrome connector: installed, but no node runtime")
			fmt.Println("script:", script)
			fmt.Println("set EIGEN_NODE_BIN to a node executable (the daemon's PATH may miss an nvm install)")
			return
		}
		fmt.Println("chrome connector: available →", script)
		fmt.Println("node:", node)
		fmt.Println("extension:", harness.ChromeBridgeExtensionDir())
		fmt.Println("(auto-registered as the `chrome` MCP server; connector only, no side-panel chat)")
	case "install", "build":
		installChromeHarness()
	default:
		fmt.Fprintf(os.Stderr, "usage: eigen chrome <status|install>\n")
		os.Exit(2)
	}
}

// importResume loads a resumed conversation: by store id, the 'eigen'
// keyword, or a transcript file path (any supported source).
func importResume(store *session.Store, resumeFile, from, sessionID string) []llm.Message {
	if resumeFile == "" {
		return nil
	}
	var history []llm.Message
	var herr error
	switch {
	case store != nil && store.Get(resumeFile) != nil:
		history, herr = store.Load(resumeFile)
	case resumeFile == "eigen":
		path := latestEigenSession()
		if path == "" {
			fail(fmt.Errorf("resume: no saved eigen sessions in ~/.eigen/sessions"))
		}
		history, herr = transcript.Load(path)
	default:
		src := transcript.Source(from)
		if src == "" {
			src = transcript.Detect(resumeFile)
		}
		if src == transcript.SourceOpenCode {
			history, herr = transcript.ImportOpenCode(resumeFile, sessionID)
		} else {
			history, herr = transcript.ImportFrom(src, resumeFile)
		}
	}
	if herr != nil {
		fail(fmt.Errorf("resume: %w", herr))
	}
	return history
}

// wdOrDot returns the current working directory, or "." if it can't be read.
func wdOrDot() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// expandTilde resolves a leading ~/ to the user's home directory (for --add-dir).
func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// multiFlag collects repeatable string flags (eigen run --var k=v --var k2=v2).
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// parseVars turns ["k=v", "x=y"] into a map; entries without '=' are ignored.
func parseVars(pairs []string) map[string]string {
	out := map[string]string{}
	for _, p := range pairs {
		if k, v, ok := strings.Cut(p, "="); ok {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out
}

// runWorkflowHeadless executes an authored workflow over ONE carried agent
// session (Tier 17). Each step's prompt is sent to the same session (so step N
// sees prior steps' work); a step's optional check is judged cross-vendor;
// on_failure governs stop/continue/retry. Exits 0 when all steps end ok, 2 when
// a stop-on-failure step fails, 1 on a hard error.
func runWorkflowHeadless(a *agent.Agent, name string, vars map[string]string) {
	wf, err := workflow.Load(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen run:", err)
		avail := workflow.List()
		if len(avail) > 0 {
			fmt.Fprintln(os.Stderr, "available workflows:", strings.Join(avail, ", "))
		} else {
			fmt.Fprintln(os.Stderr, "no workflows yet — author one at", filepath.Join(workflow.Dir(), name+".md"))
		}
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "eigen workflow: %s — %d step(s)\n", wf.Name, len(wf.Steps))

	// ONE carried session across steps.
	sess := a.NewSession()
	runStep := func(ctx context.Context, prompt, model string) (string, error) {
		// Per-step model override: a one-shot subtask on the chosen model keeps
		// the step isolated to its model while still carrying context is the
		// trade-off — v1 keeps it simple and runs every step on the main
		// session's model (model override is recorded but applied via the
		// router only when set). Inherit when model=="".
		if model != "" {
			return a.SubtaskWith(ctx, prompt, agent.SubtaskOpts{Model: model})
		}
		return sess.Send(ctx, prompt)
	}
	judge := func(ctx context.Context, condition, output string) (bool, string, error) {
		return a.JudgeClaim(ctx, nil, condition, output)
	}
	report := func(ev workflow.Event) {
		switch ev.Kind {
		case "step":
			fmt.Fprintf(os.Stderr, "\n▸ step %s\n", ev.StepID)
		case "retry":
			fmt.Fprintf(os.Stderr, "  ↻ retry %d\n", ev.Attempt)
		case "check":
			mark := "✓"
			if !ev.OK {
				mark = "✗"
			}
			fmt.Fprintf(os.Stderr, "  %s check: %s\n", mark, ev.Text)
		case "error":
			fmt.Fprintf(os.Stderr, "  ✗ %s\n", ev.Text)
		case "done":
			if ev.OK {
				fmt.Fprintln(os.Stderr, "\n✓ workflow complete")
			}
		}
	}

	res, err := wf.Run(context.Background(), workflow.RunOpts{
		Vars: vars, Run: runStep, Judge: judge, Report: report,
	})
	// Print the last step's output to stdout (scriptable).
	if res != nil && len(res.Completed) > 0 {
		if out := res.Outputs[res.Completed[len(res.Completed)-1]]; out != "" {
			fmt.Println(out)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen workflow:", err)
		os.Exit(2)
	}
}

// runTelegram runs the Telegram phone bridge: a view onto the local daemon's
// sessions over Telegram (long-poll, no inbound listener). Token + chat
// allowlist come from config (telegram_token / telegram_allow) or env
// (EIGEN_TELEGRAM_TOKEN / EIGEN_TELEGRAM_ALLOW=comma,separated,ids).
func runTelegram(cfg config.Config) {
	// The bridge serves the PRODUCTION (default) instance only: you drive your
	// real sessions from your phone, and a single bot can have just one poller,
	// so a dev instance must never run (or squat) the bridge. A non-default
	// instance exits immediately — so a dev daemon's auto-spawned bridge frees
	// the singleton lock for the prod one. Override with EIGEN_TELEGRAM_FORCE=1.
	if inst := strings.TrimSpace(os.Getenv("EIGEN_INSTANCE")); inst != "" && os.Getenv("EIGEN_TELEGRAM_FORCE") == "" {
		fmt.Fprintf(os.Stderr, "eigen telegram: instance %q is not production — the bridge serves prod only (set EIGEN_TELEGRAM_FORCE=1 to override). Exiting.\n", inst)
		return
	}
	token := strings.TrimSpace(os.Getenv("EIGEN_TELEGRAM_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(cfg.TelegramToken)
	}
	if token == "" {
		fail(fmt.Errorf("no Telegram bot token: set telegram_token in config or EIGEN_TELEGRAM_TOKEN (create a bot with @BotFather)"))
	}
	allow := cfg.TelegramAllow
	if env := strings.TrimSpace(os.Getenv("EIGEN_TELEGRAM_ALLOW")); env != "" {
		for _, p := range strings.Split(env, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
				allow = append(allow, id)
			}
		}
	}
	if len(allow) == 0 {
		fmt.Fprintln(os.Stderr, "eigen telegram: WARNING — no chat allowlist (telegram_allow / EIGEN_TELEGRAM_ALLOW). DM the bot /whoami to get your chat id, then add it.")
	}
	// Ensure the daemon is up (the bridge dials it per chat).
	if c, err := ensureDaemon(); err != nil {
		fail(fmt.Errorf("telegram: daemon unavailable: %w", err))
	} else {
		c.Close()
	}
	// Singleton: only ONE bridge may poll a given bot (Telegram 409s a second
	// getUpdates). Take an exclusive lock so a supervisor restart + a manual run
	// can never double-poll; a second instance exits cleanly.
	lock, err := acquireTelegramLock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen telegram: another bridge already holds the bot — exiting")
		os.Exit(3) // distinct code so the daemon supervisor backs off hard
	}
	defer lock.Close()
	bot := telegram.New(token)
	br := telegram.NewBridge(bot, func() (*daemon.Client, error) {
		return ensureDaemon()
	}, allow)

	ctx, cancel := signalContext()
	defer cancel()
	if err := br.Run(ctx); err != nil && ctx.Err() == nil {
		fail(fmt.Errorf("telegram: %w", err))
	}
}

// signalContext returns a context cancelled on SIGINT/SIGTERM (for long-running
// foreground commands like the Telegram bridge).
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}

// acquireTelegramLock takes an exclusive flock so only ONE bridge polls the bot
// at a time. The lock is GLOBAL (not instance-scoped): Telegram allows a single
// getUpdates poller per bot token, so a dev daemon and a prod daemon must not
// both run a bridge for the same bot. The returned file must stay open for the
// lock to hold; closing it releases the lock.
func acquireTelegramLock() (*os.File, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".eigen", "telegram.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// runPlan runs the adversarial cross-vendor planning council from the CLI:
// `eigen plan <task>`. The active model authors a plan and the other vendor's
// model critiques/hardens it; prints the converged plan.
func runPlan(cfg config.Config, provider, model, task string) {
	task = strings.TrimSpace(task)
	if task == "" {
		fmt.Fprintln(os.Stderr, "usage: eigen plan <task description>")
		os.Exit(2)
	}
	router := newAutoRouter(cfg.Route, cfg.RouteProviders, provider)
	author := effectiveModel(provider, model)
	run := router.councilRunner(func() string { return author })
	fmt.Fprintf(os.Stderr, "planning (author %s, adversary = other vendor)…\n", author)
	out, err := run(context.Background(), task, "")
	if err != nil {
		fail(fmt.Errorf("plan: %w", err))
	}
	fmt.Println(out)
}
