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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/avifenesh/eigen/internal/tui"
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

	model := flag.String("model", "", "model id (default: openai.gpt-5.5 on bedrock mantle)")
	provider := flag.String("provider", envOr("EIGEN_PROVIDER", "mantle"), "provider: mantle|llama|converse")
	perm := flag.String("perm", envOr("EIGEN_PERMISSION", "gated"), "permission posture: gated|auto")
	printMode := flag.Bool("p", false, "print mode: run one task headless (no TUI) and exit")
	flag.BoolVar(printMode, "print", false, "alias for -p")
	resumeFile := flag.String("resume", "", "resume a conversation from a transcript file or 'opencode' (auto-detected)")
	from := flag.String("from", "", "force the transcript source for --resume (claude|codex|pi|hermes|opencode|eigen)")
	sessionID := flag.String("session", "", "opencode session id for --resume opencode (default: latest)")
	maxTokens := flag.Int("max-tokens", 0, "context budget before compaction (0 = auto by provider)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("eigen", llm.Version)
		return
	}

	switch agent.Permission(*perm) {
	case agent.PermGated, agent.PermAuto:
	default:
		fail(fmt.Errorf("invalid --perm %q (want gated|auto)", *perm))
	}

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))

	prov, err := llm.New(*provider, *model)
	if err != nil {
		fail(err)
	}

	policy := tool.DefaultPolicy()
	registry, err := tool.NewRegistry(
		tool.Read(policy),
		tool.List(policy),
		tool.Glob(policy),
		tool.Grep(policy),
		tool.Write(policy),
		tool.Edit(policy),
		tool.Bash(),
	)
	if err != nil {
		fail(err)
	}

	a := &agent.Agent{
		Provider:         prov,
		Tools:            registry,
		Perm:             agent.Permission(*perm),
		MaxContextTokens: contextBudget(*maxTokens, *provider, *model),
		Compactor:        llm.NewCompactor(prov),
	}

	// Optionally resume a prior conversation from any supported transcript.
	var history []llm.Message
	if *resumeFile != "" {
		src := transcript.Source(*from)
		if src == "" {
			src = transcript.Detect(*resumeFile)
		}
		var herr error
		switch {
		case *resumeFile == "eigen" || src == transcript.SourceEigen && *resumeFile == "eigen":
			path := latestEigenSession()
			if path == "" {
				fail(fmt.Errorf("resume: no saved eigen sessions in ~/.eigen/sessions"))
			}
			history, herr = transcript.Load(path)
		case src == transcript.SourceOpenCode:
			history, herr = transcript.ImportOpenCode(*resumeFile, *sessionID)
		default:
			history, herr = transcript.ImportFrom(src, *resumeFile)
		}
		if herr != nil {
			fail(fmt.Errorf("resume: %w", herr))
		}
	}

	// Interactive terminal with no -p → the full-screen REPL (the default UX).
	interactive := isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
	if !*printMode && interactive {
		res, err := tui.Run(a, task, history)
		if err != nil {
			fail(err)
		}
		if res.Rebuild {
			execResume(res.BinPath, res.SessionPath, *provider, *model, *perm)
		}
		return
	}

	// Headless print mode (or piped/non-TTY): one task, stream to stderr,
	// final answer to stdout — scriptable.
	if task == "" {
		fmt.Fprintln(os.Stderr, "usage: eigen [flags] \"task\"   (bare `eigen` opens the TUI)")
		os.Exit(2)
	}
	a.Approve = cliApprove
	streamed := false
	a.OnEvent = func(e agent.Event) {
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
		}
	}

	fmt.Fprintf(os.Stderr, "eigen · %s · perm=%s", prov.Name(), *perm)
	if len(history) > 0 {
		fmt.Fprintf(os.Stderr, " · resumed %d msgs", len(history))
	}
	fmt.Fprintln(os.Stderr)

	var out string
	if len(history) > 0 {
		out, err = a.Resume(history).Send(context.Background(), task)
	} else {
		out, err = a.Run(context.Background(), task)
	}
	if err != nil {
		fail(err)
	}
	if streamed {
		fmt.Fprintln(os.Stderr)
	}
	fmt.Println(out)
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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// contextBudget returns the token budget before compaction. An explicit flag
// wins; otherwise EIGEN_MAX_CONTEXT_TOKENS; otherwise a per-provider/model
// default that leaves headroom under the model's true context window.
func contextBudget(flagVal int, provider, model string) int {
	if flagVal > 0 {
		return flagVal
	}
	if v := os.Getenv("EIGEN_MAX_CONTEXT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	switch provider {
	case "llama", "local":
		return 40000
	case "converse", "bedrock-converse", "claude":
		// Opus supports up to 1M, but that needs the context-1m beta header
		// (not sent yet — TODO); use the 200k-window budget for now.
		return 180000
	default: // mantle / gpt-5.5 (272k window)
		return 200000
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "eigen: "+err.Error())
	os.Exit(1)
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
