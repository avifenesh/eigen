package tool

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Backgrounded shells: a long-running bash command can be DETACHED so the agent
// stops waiting and keeps working (parallelism), à la Claude Code's ctrl+b. The
// command keeps running in its own process group; its output streams into a
// bounded buffer the agent polls with bash_output and stops with kill_shell.

// maxShellBuffer bounds a backgrounded shell's retained output (a chatty server
// can't grow memory without bound; oldest bytes are dropped).
const maxShellBuffer = 256 << 10 // 256 KiB

// Shell is one backgrounded command.
type Shell struct {
	ID      string
	Command string
	Started time.Time

	mu       sync.Mutex
	buf      bytes.Buffer // rolling combined stdout+stderr (capped)
	dropped  int64        // bytes discarded from the front (cap overflow)
	status   string       // "running" | "exited" | "killed"
	exitCode int
	finished time.Time
	pgid     int // process group (for kill); 0 until known
	readOff  int // last byte offset returned by BashOutput (for incremental reads)
}

// write appends to the rolling buffer, dropping the oldest bytes past the cap.
func (s *Shell) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Write(p)
	if s.buf.Len() > maxShellBuffer {
		over := s.buf.Len() - maxShellBuffer
		s.buf.Next(over) // discard oldest
		s.dropped += int64(over)
		s.readOff -= over
		if s.readOff < 0 {
			s.readOff = 0
		}
	}
}

// snapshot returns the full retained output + status (a copy, lock-safe).
func (s *Shell) snapshot() (out, status string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String(), s.status, s.exitCode
}

// readNew returns output appended since the last readNew + status (incremental
// poll, so a repeated bash_output only shows what's new).
func (s *Shell) readNew() (out, status string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.buf.Bytes()
	if s.readOff > len(b) {
		s.readOff = len(b)
	}
	out = string(b[s.readOff:])
	s.readOff = len(b)
	return out, s.status, s.exitCode
}

func (s *Shell) setStatus(status string, code int) {
	s.mu.Lock()
	s.status = status
	s.exitCode = code
	s.finished = time.Now()
	s.mu.Unlock()
}

func (s *Shell) setPgid(pgid int) {
	s.mu.Lock()
	s.pgid = pgid
	s.mu.Unlock()
}

// running reports whether the shell is still executing.
func (s *Shell) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == "running"
}

// finishedAt returns when the shell ended (zero if still running).
func (s *Shell) finishedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished
}

// kill signals the shell's whole process group (SIGTERM then SIGKILL).
func (s *Shell) kill() {
	s.mu.Lock()
	pgid := s.pgid
	st := s.status
	s.mu.Unlock()
	if pgid <= 0 || st != "running" {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}()
}

// ShellRegistry holds the backgrounded shells for one session. Thread-safe: the
// bash tool registers + streams into shells on the turn goroutine while
// bash_output/kill_shell + the TUI panel read on others.
type ShellRegistry struct {
	mu     sync.Mutex
	seq    atomic.Int64
	shells map[string]*Shell
	order  []string // insertion order for listing
}

func NewShellRegistry() *ShellRegistry {
	return &ShellRegistry{shells: map[string]*Shell{}}
}

// add registers a new running shell and returns it.
func (r *ShellRegistry) add(command string) *Shell {
	id := fmt.Sprintf("shell-%d", r.seq.Add(1))
	s := &Shell{ID: id, Command: command, Started: time.Now(), status: "running"}
	r.mu.Lock()
	r.shells[id] = s
	r.order = append(r.order, id)
	r.mu.Unlock()
	return s
}

// Get returns a shell by id (nil if unknown).
func (r *ShellRegistry) Get(id string) *Shell {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.shells[id]
}

// List returns the shells in insertion order (running first within order).
func (r *ShellRegistry) List() []*Shell {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Shell, 0, len(r.order))
	for _, id := range r.order {
		if s := r.shells[id]; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// RunningCount returns how many shells are still executing.
func (r *ShellRegistry) RunningCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.shells {
		if s.running() {
			n++
		}
	}
	return n
}

// StatusBlock renders a concise awareness block for the agent's system prompt:
// running shells (poll/stop) + recently-finished ones (collect). Empty when
// there's nothing to surface. Keeps the agent from forgetting a shell it
// started.
func (r *ShellRegistry) StatusBlock() string {
	if r == nil {
		return ""
	}
	var running, doneRecent []string
	now := time.Now()
	for _, s := range r.List() {
		out, status, code := s.snapshot()
		switch status {
		case "running":
			line := fmt.Sprintf("  %s (running): %s", s.ID, truncShellCmd(s.Command))
			if last := lastShellLine(out); last != "" {
				line += " — last: " + last
			}
			running = append(running, line)
		default:
			if now.Sub(s.finishedAt()) < 3*time.Minute {
				doneRecent = append(doneRecent, fmt.Sprintf("  %s (%s, exit %d): %s", s.ID, status, code, truncShellCmd(s.Command)))
			}
		}
	}
	if len(running) == 0 && len(doneRecent) == 0 {
		return ""
	}
	b := "\n\nBACKGROUND SHELLS (you started these; they run independently — don't forget them):"
	if len(running) > 0 {
		b += "\nstill running (poll with bash_output, stop with kill_shell):\n" + strings.Join(running, "\n")
	}
	if len(doneRecent) > 0 {
		b += "\nfinished — collect the result with bash_output if you haven't:\n" + strings.Join(doneRecent, "\n")
	}
	return b
}

// lastShellLine returns the last non-empty line of out, truncated — a one-line
// "what's it doing now" hint for the background-shell awareness block.
func lastShellLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln != "" {
			if len(ln) > 100 {
				ln = ln[:97] + "…"
			}
			return ln
		}
	}
	return ""
}
