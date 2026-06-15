package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultBashTimeout = 30 * time.Second
	maxBashTimeout     = 10 * time.Minute
)

// bashDeps carries the optional plumbing for backgrounding (the shell registry
// + a per-call detach signal). nil-safe: a plain Bash(policy) still works.
type bashDeps struct {
	shells *ShellRegistry
	// detachOf returns the detach channel for the CURRENT bash call, if any.
	// A receive on it means "stop waiting — background this command and return
	// a handle" (the user's ctrl+b / alt+ key while the command runs). Keyed by
	// nothing: there is at most one foreground bash per session turn.
	detach func() <-chan struct{}
}

// Bash returns the command-execution tool. It is mutating (Exec): in gated mode
// it requires approval. The path fence does not constrain arbitrary commands —
// that is what the approval gate is for.
func Bash(policy *Policy) Definition { return bashWith(policy, bashDeps{}) }

// BashWithShells is Bash plus backgrounding: a command can be detached (the
// `background` arg, or a runtime detach signal) so the agent stops waiting and
// keeps working; the command runs on in its own process group, its output
// streaming into the shell registry for bash_output/kill_shell.
func BashWithShells(policy *Policy, shells *ShellRegistry, detach func() <-chan struct{}) Definition {
	return bashWith(policy, bashDeps{shells: shells, detach: detach})
}

func bashWith(policy *Policy, deps bashDeps) Definition {
	desc := "Run a shell command with bash -c and return its combined stdout+stderr. Mutating: requires approval in gated mode."
	if deps.shells != nil {
		desc += " Set background=true for a long-running command (dev server, watcher, build): it returns a shell id immediately and keeps running so you can continue working — poll it with bash_output and stop it with kill_shell."
	}
	params := `{
  "type": "object",
  "properties": {
    "command": { "type": "string", "description": "Shell command to run." },
    "timeout_seconds": { "type": "integer", "description": "Max seconds to run (default 30, max 600)." }
  },
  "required": ["command"],
  "additionalProperties": false
}`
	if deps.shells != nil {
		params = `{
  "type": "object",
  "properties": {
    "command": { "type": "string", "description": "Shell command to run." },
    "timeout_seconds": { "type": "integer", "description": "Max seconds to run (default 30, max 600). Ignored when background=true." },
    "background": { "type": "boolean", "description": "Run detached: return a shell id immediately and keep it running in the background (for servers, watchers, long builds). Poll with bash_output, stop with kill_shell." }
  },
  "required": ["command"],
  "additionalProperties": false
}`
	}
	return Definition{
		Name:        "bash",
		Description: desc,
		Parameters:  json.RawMessage(params),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Command        string `json:"command"`
				TimeoutSeconds int    `json:"timeout_seconds"`
				Background     bool   `json:"background"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Command == "" {
				return "", fmt.Errorf("command is required")
			}
			dir := ""
			if policy != nil {
				dir = policy.Dir()
			}

			// Background mode: spawn detached, register a Shell, return now.
			if in.Background && deps.shells != nil {
				return startBackgroundShell(deps.shells, in.Command, dir)
			}

			timeout := defaultBashTimeout
			if in.TimeoutSeconds > 0 {
				timeout = time.Duration(in.TimeoutSeconds) * time.Second
				if timeout > maxBashTimeout {
					timeout = maxBashTimeout
				}
			}
			tctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			// A detach channel (the ctrl+b case): when the registry is wired and
			// a detach signal arrives mid-run, hand the live process off to the
			// background registry and return its handle instead of waiting.
			var detachCh <-chan struct{}
			if deps.shells != nil && deps.detach != nil {
				detachCh = deps.detach()
			}
			return runBash(tctx, in.Command, dir, timeout, deps.shells, detachCh)
		},
	}
}

// runBash runs a foreground command, streaming output into a buffer so a detach
// mid-run can hand the live process to the background registry. When detachCh
// is nil this is a plain synchronous run (the historical behavior).
func runBash(ctx context.Context, command, dir string, timeout time.Duration, shells *ShellRegistry, detachCh <-chan struct{}) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	// Own process group so a timeout/cancel/kill reaches the WHOLE tree (bash +
	// anything it spawned), not just the bash leader.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if dir != "" {
		cmd.Dir = dir
	}
	var buf safeBuffer
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return "", err
	}
	pgid := cmd.Process.Pid // Setpgid makes pid == pgid
	// Pump the pipe into the buffer; pumpDone closes when all output is drained
	// (so callers read buf only AFTER the reader finishes — otherwise the final
	// line can be missed under load, a real truncation race).
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			buf.WriteString(sc.Text() + "\n")
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	killGroup := func(sig syscall.Signal) { _ = syscall.Kill(-pgid, sig) }
	// finishOutput closes the write end (unblocking the scanner) and waits for
	// the pump to drain before returning the captured output.
	finishOutput := func() string {
		pw.Close()
		<-pumpDone
		return buf.String()
	}

	select {
	case err := <-done:
		out := finishOutput()
		if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
			return out + fmt.Sprintf("\n[exit status %d]", ee.ExitCode()), nil
		}
		if err != nil {
			return "", err
		}
		return out, nil
	case <-ctx.Done():
		// Timeout / parent cancel: kill the group, drain briefly, report.
		killGroup(syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		out := finishOutput()
		if ctx.Err() == context.DeadlineExceeded {
			return out + fmt.Sprintf("\n[timed out after %s]", timeout), nil
		}
		return out, ctx.Err()
	case <-detachCh:
		// The user backgrounded this running command: adopt the live process
		// into the shell registry (output keeps streaming there) and return a
		// handle immediately so the agent continues.
		return adoptIntoBackground(shells, command, cmd, pgid, &buf, pr, pw, done)
	}
}

// startBackgroundShell spawns command detached and registers it, returning the
// handle line immediately (background=true path).
func startBackgroundShell(shells *ShellRegistry, command, dir string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if dir != "" {
		cmd.Dir = dir
	}
	sh := shells.add(command)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		sh.setStatus("exited", -1)
		return "", err
	}
	sh.setPgid(cmd.Process.Pid)
	go pumpShell(sh, pr)
	go func() {
		err := cmd.Wait()
		pw.Close()
		finishShell(sh, err)
	}()
	return fmt.Sprintf("started background shell %s: %s\n(it keeps running; poll with bash_output %s, stop with kill_shell %s)",
		sh.ID, truncShellCmd(command), sh.ID, sh.ID), nil
}

// adoptIntoBackground converts an already-running foreground command into a
// registered background shell (the detach/ctrl+b path).
func adoptIntoBackground(shells *ShellRegistry, command string, cmd *exec.Cmd, pgid int, buf *safeBuffer, pr *io.PipeReader, pw *io.PipeWriter, done chan error) (string, error) {
	sh := shells.add(command)
	sh.setPgid(pgid)
	// Seed the shell buffer with what's been captured so far.
	sh.write([]byte(buf.String()))
	// Re-route: the foreground pump goroutine is still draining pr into `buf`;
	// switch future writes to the shell by tee-ing. Simplest: keep the existing
	// pump (writes to buf), and mirror buf→shell as it grows is complex — so
	// instead, start a fresh pump on the SAME pipe is not possible (one reader).
	// We already attached the pump to buf; redirect by having the pump write to
	// the shell from here on via buf's onWrite hook.
	buf.redirect(sh)
	go func() {
		err := <-done
		pw.Close()
		finishShell(sh, err)
	}()
	return fmt.Sprintf("backgrounded as shell %s: %s\n(it keeps running; poll with bash_output %s, stop with kill_shell %s)",
		sh.ID, truncShellCmd(command), sh.ID, sh.ID), nil
}

func pumpShell(sh *Shell, pr *io.PipeReader) {
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		sh.write([]byte(sc.Text() + "\n"))
	}
}

func finishShell(sh *Shell, err error) {
	if err == nil {
		sh.setStatus("exited", 0)
		return
	}
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		// A SIGKILL/SIGTERM from kill_shell shows as a signal, not a clean exit.
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			sh.setStatus("killed", 128+int(ws.Signal()))
			return
		}
		sh.setStatus("exited", ee.ExitCode())
		return
	}
	sh.setStatus("exited", -1)
}

func truncShellCmd(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 80 {
		return s[:77] + "…"
	}
	return s
}

// safeBuffer is a goroutine-safe string buffer with an optional redirect hook
// (used to hand a foreground command's live output to a background shell).
type safeBuffer struct {
	mu  sync.Mutex
	b   strings.Builder
	out *Shell // when set, writes also go here (after redirect)
}

func (s *safeBuffer) WriteString(p string) {
	s.mu.Lock()
	dst := s.out
	if dst == nil {
		s.b.WriteString(p)
	}
	s.mu.Unlock()
	if dst != nil {
		dst.write([]byte(p))
	}
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// redirect routes all FUTURE writes to the shell (the buffered prefix was
// already copied by the caller).
func (s *safeBuffer) redirect(sh *Shell) {
	s.mu.Lock()
	s.out = sh
	s.mu.Unlock()
}
