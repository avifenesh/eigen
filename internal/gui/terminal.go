package gui

// Server-side PTY terminal bridge. The right-hand panel hosts a REAL interactive
// shell — the GUI peer of the TUI's terminal tab (internal/tui/termpanel.go). The
// GUI process runs on the host, so it owns the PTY directly: it starts the user's
// shell on a pseudo-terminal, streams the raw PTY bytes to the frontend over a
// Wails event, writes the user's keystrokes back to the PTY, and reshapes on
// resize. Unlike the TUI we do NOT run a VT emulator here — xterm.js on the
// frontend IS the emulator, so we ship raw bytes and let it interpret them.
//
// State note: the GUI hosts exactly one Bridge, but the Bridge struct lives in
// bridge.go which this file must not edit. So all terminal state lives in a
// package-level lazy singleton (termManager) guarded by its own mutex, mirroring
// how voice.go keeps its controller — just without a Bridge field. Each terminal
// owns its pty + cmd + a stopOnce; the manager map is mutex-guarded. The reader
// goroutine never touches shared manager state (it captures its own *pty and id),
// so a flooding command can't race the map.
//
// Concurrency: two goroutines per terminal — a reader (pty.Read → base64 →
// emit) and a waiter (cmd.Wait → emit exited → kill). Teardown is sync.Once-
// guarded per terminal so TerminalKill / waiter / terminalShutdownAll can all
// race harmlessly. Emits go through the Bridge's emit (terminal methods are on
// *Bridge), so they reach the frontend the same way every other GUI event does.

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// eventTerminal carries raw PTY output (and the exit signal) to the frontend.
// Payload is a TerminalEventDTO: {id, data?, exited?}. data is base64 of the raw
// PTY bytes so arbitrary control sequences survive the JSON event channel; the
// frontend base64-decodes and feeds the bytes straight into xterm.js.
const eventTerminal = "eigen:terminal"

// TerminalEventDTO is pushed on eigen:terminal. Exactly one of data/exited is
// meaningful per event: a chunk of output carries data (base64 of raw bytes);
// the final event for a terminal carries exited=true (and empty data) when the
// shell process ends or is killed. id ties the event to the TerminalStart that
// created it (the frontend may host more than one terminal).
type TerminalEventDTO struct {
	ID     string `json:"id"`
	Data   string `json:"data,omitempty"`   // base64 of raw PTY bytes (output chunk)
	Exited bool   `json:"exited,omitempty"` // the shell process ended / was killed
}

// term is one live terminal: its PTY master, the shell process, and a sync.Once
// that makes teardown idempotent across TerminalKill / the waiter / shutdown.
type term struct {
	id   string
	pty  *os.File
	cmd  *exec.Cmd
	once sync.Once // guards the single kill+close+goroutine-stop
}

// termManager owns every live terminal, keyed by id. Guarded by mu. It is a
// package-level lazy singleton (the GUI has one Bridge) so terminal state needs
// no new Bridge field — see the file header.
type termManager struct {
	mu    sync.Mutex
	terms map[string]*term
}

var (
	termMgrOnce sync.Once
	termMgr     *termManager
	termSeq     atomic.Uint64 // monotonic source for unique terminal ids
)

// terminals returns the process-wide terminal manager, built once.
func terminals() *termManager {
	termMgrOnce.Do(func() { termMgr = &termManager{terms: map[string]*term{}} })
	return termMgr
}

// TerminalStart launches the user's shell ($SHELL or /bin/bash) on a PTY sized
// cols×rows and returns a terminal id. It spawns a reader goroutine (pty.Read →
// base64 → emit eigen:terminal{id,data}) and a waiter goroutine (cmd.Wait → emit
// {id,exited:true} + tear the terminal down). The shell starts in the user's home
// (falling back to cwd) with the parent environment inherited plus a sane TERM.
func (b *Bridge) TerminalStart(cols, rows int) (string, error) {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}

	// $SHELL → bash/sh on PATH → common absolute paths. The bare "/bin/bash"
	// fallback broke in minimal environments (agent workspace / container) where
	// that path doesn't exist; resolve a shell that's actually present.
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = resolveTerminalShell()
	}
	cmd := exec.Command(shell, "-i")
	cmd.Dir = terminalStartDir()
	// Inherit the parent env; advertise a 256-color terminal so the shell + TUIs
	// emit the escape sequences xterm.js renders. (TERM is appended last so it
	// wins over any inherited value.)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// creack/pty starts the child in its own session (Setsid) with the PTY as the
	// controlling terminal, so it becomes a session + process-group leader
	// (pgid == pid). That makes job control work and lets us kill the whole group
	// via -pid — so do NOT set Setpgid ourselves.
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return "", fmt.Errorf("terminal: %w", err)
	}

	id := "term-" + strconv.FormatUint(termSeq.Add(1), 10)
	t := &term{id: id, pty: f, cmd: cmd}

	mgr := terminals()
	mgr.mu.Lock()
	mgr.terms[id] = t
	mgr.mu.Unlock()

	// Reader: drain raw PTY output → base64 → emit. Captures its own *os.File and
	// id; it never touches the manager map, so it can't race other terminals.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				b.emit(eventTerminal, TerminalEventDTO{
					ID:   id,
					Data: base64.StdEncoding.EncodeToString(buf[:n]),
				})
			}
			if rerr != nil {
				return // EOF / closed PTY (the waiter emits the exited event)
			}
		}
	}()

	// Waiter: reap the child exactly once, report the true exit, then tear down
	// (idempotent — TerminalKill may have already run, the once de-dupes).
	go func() {
		_ = cmd.Wait()
		b.emit(eventTerminal, TerminalEventDTO{ID: id, Exited: true})
		mgr.kill(id)
	}()

	return id, nil
}

// TerminalWrite forwards raw bytes to a terminal's PTY. data is the literal
// keystroke string from xterm.js's onData (already the on-the-wire bytes — NOT
// base64), so it is written through unchanged.
func (b *Bridge) TerminalWrite(id, data string) error {
	t := terminals().get(id)
	if t == nil || t.pty == nil {
		return fmt.Errorf("terminal %q not found", id)
	}
	_, err := io.WriteString(t.pty, data)
	return err
}

// TerminalResize reshapes a terminal's PTY to cols×rows (the frontend calls this
// from xterm.js's onResize / fit addon).
func (b *Bridge) TerminalResize(id string, cols, rows int) error {
	t := terminals().get(id)
	if t == nil || t.pty == nil {
		return fmt.Errorf("terminal %q not found", id)
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return pty.Setsize(t.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// TerminalKill ends a terminal's shell and releases its PTY. Idempotent: the
// per-terminal sync.Once means repeated calls (and the waiter's own teardown) are
// harmless no-ops after the first.
func (b *Bridge) TerminalKill(id string) error {
	terminals().kill(id)
	return nil
}

// terminalShutdownAll kills every live terminal. Provided for the orchestrator to
// call from Bridge.Shutdown (bridge.go is intentionally untouched here). Safe to
// call repeatedly and from any goroutine.
func terminalShutdownAll() {
	mgr := terminals()
	mgr.mu.Lock()
	ids := make([]string, 0, len(mgr.terms))
	for id := range mgr.terms {
		ids = append(ids, id)
	}
	mgr.mu.Unlock()
	for _, id := range ids {
		mgr.kill(id)
	}
}

// get returns the live terminal for id, or nil.
func (m *termManager) get(id string) *term {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.terms[id]
}

// kill removes the terminal from the map and tears it down once: close the PTY
// master (delivers SIGHUP to the foreground group + unblocks the reader on EOF),
// then escalate SIGTERM→SIGKILL to the whole process group from a detached
// goroutine so the caller never blocks. The sync.Once makes this exactly-once
// even when TerminalKill, the waiter, and terminalShutdownAll race.
func (m *termManager) kill(id string) {
	m.mu.Lock()
	t := m.terms[id]
	if t != nil {
		delete(m.terms, id)
	}
	m.mu.Unlock()
	if t == nil {
		return
	}
	t.once.Do(func() {
		f := t.pty
		var pid int
		if t.cmd != nil && t.cmd.Process != nil {
			pid = t.cmd.Process.Pid
		}
		go func() {
			if f != nil {
				_ = f.Close() // HUP the session + unblock the reader
			}
			if pid > 0 { // never -0: that would signal our OWN process group
				_ = syscall.Kill(-pid, syscall.SIGTERM)
				time.Sleep(200 * time.Millisecond)
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
}

// terminalStartDir picks the shell's working directory: the user's home, falling
// back to the GUI process cwd.
func terminalStartDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// resolveTerminalShell finds a usable interactive shell when $SHELL is unset:
// bash/sh on PATH, then common absolute paths. Falls back to "/bin/sh" (the most
// universally present), so the terminal opens even in a minimal environment
// where /bin/bash is absent.
func resolveTerminalShell() string {
	for _, name := range []string{"bash", "zsh", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, abs := range []string{"/bin/bash", "/usr/bin/bash", "/bin/sh", "/system/bin/sh"} {
		if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
			return abs
		}
	}
	return "/bin/sh"
}
