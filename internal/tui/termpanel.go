package tui

// Terminal tab (Tier 11): a REAL terminal embedded in the right panel — a PTY
// running your shell, interpreted by a VT emulator and rendered as panel lines.
// This is the standard recipe (creack/pty + a VT emulator), the same one tmux,
// zellij, and editors' :terminal use; we don't reinvent it. Interactive
// programs (vim, less, top, htop) work because they get a genuine TTY.
//
// The model never drives this panel — it's the user's terminal. When the term
// tab is focused, keystrokes are encoded to the PTY (incl. esc/ctrl+c, so vim
// and job control work); ctrl+g RELEASES focus so the rest of the TUI gets its
// keys back, leaving the shell running. The shell lives in THIS view process
// (one per window), so it is torn down when the window closes — it never
// accumulates in the daemon.
//
// Concurrency: two goroutines per shell generation — a reader (f.Read → the
// internally-synchronized emulator; never touches model fields) and a waiter
// (cmd.Wait → reaps the child, then reports exit). ALL m.term fields are owned
// by the Update goroutine. gen is bumped on every (re)start and stop, so any
// message from an old shell is dropped by gen mismatch. Repaint is paced by one
// self-re-arming tick per generation (guarded so it can't multiply); a flooding
// command can't drown the event loop, and resize happens in Update, never View.

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// termRefresh paces terminal-panel repaints while it is the visible tab.
const termRefresh = 70 * time.Millisecond

// termState holds the embedded terminal's PTY + emulator. A nil pty means the
// terminal hasn't started yet (lazy: started when the tab is first shown). All
// fields are owned by the Update goroutine.
type termState struct {
	pty     *os.File
	cmd     *exec.Cmd
	emu     *vt.SafeEmulator
	focused bool
	started bool
	exited  bool
	ticking bool // a refresh tick loop is live (prevents duplicate chains)
	cols    int
	rows    int
	gen     int // bumped on each (re)start AND stop; stale messages drop on mismatch
}

// termExitedMsg reports the shell process exited (from cmd.Wait); gen ties it to
// that shell instance.
type termExitedMsg struct{ gen int }

// termTickMsg paces terminal-panel repaints while the tab is visible.
type termTickMsg struct{ gen int }

// termFocused reports whether the embedded terminal currently owns keystrokes.
// (Includes the exited state so 'enter' can restart the shell from the panel.)
func (m *model) termFocused() bool {
	return m.rightTab == rightTabTerminal && m.changesOn && m.term.focused && m.term.started
}

// startTerm lazily launches the shell on a PTY sized to the panel. When already
// running it just resizes + ensures the repaint tick. Returns the commands that
// drive the terminal (reader + waiter + paced repaint).
func (m *model) startTerm(rows int) tea.Cmd {
	if m.term.started && !m.term.exited {
		m.ensureTermSize(rows)
		if m.term.ticking {
			return nil
		}
		return m.termTick()
	}
	// (Re)start: tear down any previous shell first (e.g. restart after exit).
	m.killShell()

	cols := termCols()
	if rows < 1 {
		rows = 1
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	cmd := exec.Command(shell, "-i")
	cmd.Dir = m.sessionDir()
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	// Do NOT set Setpgid here: creack/pty already starts the child in its own
	// session (Setsid) with the PTY as controlling terminal, which makes it a
	// session+process-group leader (pgid == pid) suitable for job control and
	// for killing the whole group via -pid.
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		m.note("terminal: " + err.Error())
		return nil
	}
	emu := vt.NewSafeEmulator(cols, rows) // fresh emulator per generation
	m.term.pty = f
	m.term.cmd = cmd
	m.term.emu = emu
	m.term.started = true
	m.term.exited = false
	m.term.focused = true
	m.term.cols = cols
	m.term.rows = rows
	m.term.gen++
	gen := m.term.gen

	// Reader: drain the PTY into the emulator (run as a goroutine by the
	// bubbletea runtime). Never touches model fields.
	reader := func() tea.Msg {
		drainPTY(f, emu)
		return nil
	}
	// Waiter: reap the child exactly once, then report the true process exit.
	waiter := func() tea.Msg {
		_ = cmd.Wait()
		return termExitedMsg{gen: gen}
	}
	return tea.Batch(reader, waiter, m.termTick())
}

// drainPTY copies PTY output into the (thread-safe) emulator until close/EOF.
// It writes partial reads even when the final read returns an error.
func drainPTY(f *os.File, emu *vt.SafeEmulator) {
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			emu.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// termCols is the emulator's column count inside the panel's "│ " gutter.
func termCols() int {
	c := rightPanelWidthCols - 2
	if c < 1 {
		c = 1
	}
	return c
}

// termTick schedules the next paced repaint. The ticking guard ensures only one
// tick chain is live per generation (the review's "ticks can multiply" risk).
func (m *model) termTick() tea.Cmd {
	m.term.ticking = true
	gen := m.term.gen
	return tea.Tick(termRefresh, func(time.Time) tea.Msg { return termTickMsg{gen: gen} })
}

// ensureTermSize reshapes the PTY + emulator when the panel dimensions change.
// Called only from Update (window resize / tab switch / tick), never from View.
func (m *model) ensureTermSize(rows int) {
	if m.term.pty == nil || m.term.exited {
		return
	}
	cols := termCols()
	if rows < 1 {
		rows = 1
	}
	if cols == m.term.cols && rows == m.term.rows {
		return
	}
	m.term.cols, m.term.rows = cols, rows
	_ = pty.Setsize(m.term.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if m.term.emu != nil {
		m.term.emu.Resize(cols, rows)
	}
}

// killShell tears down the current shell: close the PTY master (delivers SIGHUP
// to the foreground group), then escalate SIGTERM→SIGKILL to the process group
// from a detached goroutine (non-blocking, so Update never sleeps). Bumps gen so
// any in-flight message from this shell becomes stale. Idempotent.
func (m *model) killShell() {
	f := m.term.pty
	var pid int
	if m.term.cmd != nil && m.term.cmd.Process != nil {
		pid = m.term.cmd.Process.Pid
	}
	m.term.pty = nil
	m.term.cmd = nil
	m.term.gen++
	if f == nil && pid <= 0 {
		return
	}
	go func() {
		if f != nil {
			_ = f.Close() // HUP the session
		}
		if pid > 0 { // never -0: that would signal our OWN process group
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}

// stopTerm releases the embedded terminal entirely (on TUI exit / teardown).
func (m *model) stopTerm() {
	m.killShell()
	m.term.started = false
	m.term.exited = true
	m.term.focused = false
	m.term.ticking = false
}

// termLines renders the emulator screen as exactly h panel lines (header + the
// VT grid). Pure: it only reads the current emulator snapshot.
func (m *model) termLines(h int) []string {
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(rightPanelWidthCols-2), rightPanelWidthCols))
	gridRows := h - 1 // header takes one row
	if gridRows < 1 {
		gridRows = 1
	}
	if !m.term.started {
		lines = append(lines, changesPad(dim("starting "+termShellName()+"…"), rightPanelWidthCols))
		for len(lines) < h {
			lines = append(lines, changesPad("", rightPanelWidthCols))
		}
		return lines
	}
	var grid []string
	if m.term.emu != nil {
		grid = strings.Split(m.term.emu.Render(), "\n")
	}
	contentW := rightPanelWidthCols - 2
	for i := 0; i < gridRows; i++ {
		var row string
		if i < len(grid) {
			row = ansiTrunc(grid[i], contentW)
		}
		if m.term.exited && i == 0 {
			row = dim("[exited — enter restarts]")
		}
		lines = append(lines, changesPad(row, rightPanelWidthCols))
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", rightPanelWidthCols))
	}
	return lines
}

func termShellName() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return "bash"
	}
	if i := strings.LastIndexByte(sh, '/'); i >= 0 {
		return sh[i+1:]
	}
	return sh
}

// termKey handles a keystroke when the terminal tab owns input. The terminal
// grabs keys only when active+focused; ctrl+g RELEASES focus (so esc/ctrl+c go
// to the shell — vim and job control work). Returns (cmd, handled).
func (m *model) termKey(key string, msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.rightTab != rightTabTerminal || !m.changesOn || !m.term.focused {
		return nil, false
	}
	// ctrl+g releases focus to the TUI (the panel stays open, shell keeps
	// running). This is the one chord the terminal does not forward.
	if key == "ctrl+g" {
		m.term.focused = false
		return nil, true
	}
	if m.term.exited {
		if key == "enter" {
			return m.startTerm(m.term.rows), true
		}
		return nil, true
	}
	if m.term.pty == nil {
		return nil, true
	}
	if data := encodeKey(key, msg); data != "" {
		_, _ = io.WriteString(m.term.pty, data)
	}
	return nil, true
}

// encodeKey maps a bubbletea key event to the bytes a PTY expects.
func encodeKey(key string, msg tea.KeyMsg) string {
	switch key {
	case "enter":
		return "\r"
	case "tab":
		return "\t"
	case "backspace":
		return "\x7f"
	case "delete":
		return "\x1b[3~"
	case "esc":
		return "\x1b"
	case "up":
		return "\x1b[A"
	case "down":
		return "\x1b[B"
	case "right":
		return "\x1b[C"
	case "left":
		return "\x1b[D"
	case "home":
		return "\x1b[H"
	case "end":
		return "\x1b[F"
	case "pgup":
		return "\x1b[5~"
	case "pgdown":
		return "\x1b[6~"
	case "space":
		return " "
	}
	// Ctrl chords: ctrl+a..ctrl+z → 0x01..0x1a.
	if strings.HasPrefix(key, "ctrl+") && len(key) == 6 {
		c := key[5]
		if c >= 'a' && c <= 'z' {
			return string([]byte{c - 'a' + 1})
		}
	}
	// Printable input: use the event's runes (handles pasted/multi-rune text).
	if len(msg.Runes) > 0 {
		if msg.Alt {
			return "\x1b" + string(msg.Runes)
		}
		return string(msg.Runes)
	}
	return ""
}
