package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Tier 21 — right-panel notepad tab: a freeform per-session scratch pad. Notes
// are keyed by session id and persisted to ~/.eigen/notepad[-instance]/<id>.md,
// so they survive detaching the window and restarting the daemon. Editing is
// minimal-but-real (type, newline, backspace, arrows, home/end); the pad grabs
// keys only when focused (click the body or press enter on the tab), and ctrl+g
// / esc release focus back to the TUI — same contract as the terminal tab.

type notepadState struct {
	loaded    bool     // notes pulled for the current session
	loadedFor string   // session id the current text belongs to
	lines     []string // the note body, one entry per line (never empty: ≥ 1 line)
	cx, cy    int      // cursor column/row (runes)
	scroll    int      // first visible body line
	focused   bool     // owns keystrokes
	dirty     bool     // unsaved edits pending a flush
}

// notepadKeyShown is the hint shown in the (empty/unfocused) pad.
const notepadHint = "click or press enter to edit · ctrl+g to leave"

// notepadDir is the instance-aware directory holding per-session note files.
func notepadDir() string {
	home, _ := os.UserHomeDir()
	name := "notepad"
	if inst := os.Getenv("EIGEN_INSTANCE"); inst != "" {
		name += "-" + inst
	}
	return filepath.Join(home, ".eigen", name)
}

// notepadSessionID is the key for the current session's notes (a stable
// fallback when there's no daemon session id, e.g. a purely local chat).
func (m *model) notepadSessionID() string {
	if sl, ok := m.backend.(interface{ SessionID() string }); ok {
		if id := strings.TrimSpace(sl.SessionID()); id != "" {
			return id
		}
	}
	return "local"
}

func notepadPath(id string) string {
	// Sanitize the id into a safe filename.
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
	return filepath.Join(notepadDir(), safe+".md")
}

// loadNotepad pulls the saved notes for the current session into the buffer
// (once per session; switching sessions reloads). Cheap and best-effort.
func (m *model) loadNotepad() {
	id := m.notepadSessionID()
	if m.notepad.loaded && m.notepad.loadedFor == id {
		return
	}
	text := ""
	if data, err := os.ReadFile(notepadPath(id)); err == nil {
		text = string(data)
	}
	m.notepad.lines = splitNoteLines(text)
	m.notepad.loaded = true
	m.notepad.loadedFor = id
	m.notepad.cx, m.notepad.cy, m.notepad.scroll = 0, 0, 0
	m.notepad.dirty = false
}

// splitNoteLines turns stored text into the line buffer (always ≥ 1 line).
func splitNoteLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

// saveNotepad flushes the buffer to disk (atomic rename). Best-effort.
func (m *model) saveNotepad() {
	if !m.notepad.loaded {
		return
	}
	id := m.notepad.loadedFor
	if id == "" {
		id = m.notepadSessionID()
	}
	dir := notepadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	body := strings.Join(m.notepad.lines, "\n")
	// Don't persist a single empty line as a stray file; remove instead.
	if strings.TrimSpace(body) == "" {
		_ = os.Remove(notepadPath(id))
		m.notepad.dirty = false
		return
	}
	tmp := notepadPath(id) + ".tmp"
	if err := os.WriteFile(tmp, []byte(body+"\n"), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, notepadPath(id))
	m.notepad.dirty = false
}

// notepadFocused reports whether the pad owns keystrokes right now.
func (m *model) notepadFocused() bool {
	return m.rightTab == rightTabNotepad && m.changesOn && m.notepad.focused
}

// notepadKey handles a keystroke when the notepad owns input. Returns whether
// it consumed the key. ctrl+g / esc release focus (and flush). The pad never
// quits eigen or forwards to the chat while focused.
func (m *model) notepadKey(key string, msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.notepadFocused() {
		return nil, false
	}
	n := &m.notepad
	switch key {
	case "ctrl+g", "esc":
		n.focused = false
		m.saveNotepad()
		return nil, true
	case "enter":
		m.notepadInsertNewline()
	case "backspace":
		m.notepadBackspace()
	case "left":
		m.notepadMoveLeft()
	case "right":
		m.notepadMoveRight()
	case "up":
		if n.cy > 0 {
			n.cy--
			n.cx = noteClamp(n.cx, 0, len([]rune(n.lines[n.cy])))
		}
	case "down":
		if n.cy < len(n.lines)-1 {
			n.cy++
			n.cx = noteClamp(n.cx, 0, len([]rune(n.lines[n.cy])))
		}
	case "home", "ctrl+a":
		n.cx = 0
	case "end", "ctrl+e":
		n.cx = len([]rune(n.lines[n.cy]))
	case "tab":
		m.notepadInsertText("  ")
	case "space":
		m.notepadInsertText(" ")
	default:
		// Printable runes (incl. pasted text) go in verbatim.
		if msg.Type == tea.KeyRunes {
			m.notepadInsertText(string(msg.Runes))
		} else {
			return nil, true // swallow other control keys while focused
		}
	}
	n.dirty = true
	m.notepadEnsureCursorVisible()
	return nil, true
}

func (m *model) notepadInsertText(s string) {
	n := &m.notepad
	row := []rune(n.lines[n.cy])
	cx := noteClamp(n.cx, 0, len(row))
	row = append(row[:cx], append([]rune(s), row[cx:]...)...)
	n.lines[n.cy] = string(row)
	n.cx = cx + len([]rune(s))
}

func (m *model) notepadInsertNewline() {
	n := &m.notepad
	row := []rune(n.lines[n.cy])
	cx := noteClamp(n.cx, 0, len(row))
	left, right := string(row[:cx]), string(row[cx:])
	n.lines[n.cy] = left
	rest := append([]string{right}, n.lines[n.cy+1:]...)
	n.lines = append(n.lines[:n.cy+1], rest...)
	n.cy++
	n.cx = 0
}

func (m *model) notepadBackspace() {
	n := &m.notepad
	if n.cx > 0 {
		row := []rune(n.lines[n.cy])
		row = append(row[:n.cx-1], row[n.cx:]...)
		n.lines[n.cy] = string(row)
		n.cx--
		return
	}
	if n.cy == 0 {
		return // start of buffer
	}
	// Join with the previous line.
	prev := []rune(n.lines[n.cy-1])
	n.cx = len(prev)
	n.lines[n.cy-1] = string(prev) + n.lines[n.cy]
	n.lines = append(n.lines[:n.cy], n.lines[n.cy+1:]...)
	n.cy--
}

func (m *model) notepadMoveLeft() {
	n := &m.notepad
	if n.cx > 0 {
		n.cx--
	} else if n.cy > 0 {
		n.cy--
		n.cx = len([]rune(n.lines[n.cy]))
	}
}

func (m *model) notepadMoveRight() {
	n := &m.notepad
	if n.cx < len([]rune(n.lines[n.cy])) {
		n.cx++
	} else if n.cy < len(n.lines)-1 {
		n.cy++
		n.cx = 0
	}
}

// notepadEnsureCursorVisible scrolls so the cursor row is within the body window.
func (m *model) notepadEnsureCursorVisible() {
	n := &m.notepad
	h := m.notepadBodyHeight()
	if h < 1 {
		return
	}
	if n.cy < n.scroll {
		n.scroll = n.cy
	}
	if n.cy >= n.scroll+h {
		n.scroll = n.cy - h + 1
	}
	if n.scroll < 0 {
		n.scroll = 0
	}
}

// notepadBodyHeight is the editable body height (panel minus header + status).
func (m *model) notepadBodyHeight() int {
	return (m.vp.Height - 1) - 1 // header row + status row
}

// notepadLines renders the notepad as exactly h panel lines.
func (m *model) notepadLines(h int) []string {
	if !m.notepad.loaded {
		m.loadNotepad()
	}
	pw := m.rightCols()
	contentW := pw - 4
	if contentW < 1 {
		contentW = 1
	}
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(pw-2), pw))

	n := &m.notepad
	bodyH := h - 2 // header + status
	if bodyH < 1 {
		bodyH = 1
	}
	// Clamp scroll for the current body window.
	if n.scroll > len(n.lines)-bodyH {
		n.scroll = len(n.lines) - bodyH
	}
	if n.scroll < 0 {
		n.scroll = 0
	}
	emptyUnfocused := !m.notepadFocused() && strings.TrimSpace(strings.Join(n.lines, "")) == ""
	for i := 0; i < bodyH; i++ {
		li := n.scroll + i
		var s string
		if emptyUnfocused && i == 0 {
			s = dim(" " + notepadHint) // empty pad: show the hint in the body
		} else if li < len(n.lines) {
			s = n.lines[li]
			if m.notepadFocused() && li == n.cy {
				s = notepadWithCursor(s, n.cx)
			}
			s = ansiTrunc(" "+s, contentW+2)
		}
		lines = append(lines, changesPad(s, pw))
	}
	// Status row: focus state + char count.
	status := notepadHint
	if m.notepadFocused() {
		chars := 0
		for _, ln := range n.lines {
			chars += len([]rune(ln))
		}
		status = "editing · " + itoaTUI(chars) + " chars · ctrl+g to leave"
		if n.dirty {
			status = "● " + status
		}
	}
	lines = append(lines, changesPad(dim(" "+ansiTrunc(status, contentW)), pw))
	for len(lines) < h {
		lines = append(lines, changesPad("", pw))
	}
	return lines[:h]
}

// notepadWithCursor renders a reverse-video cursor cell at column cx.
func notepadWithCursor(s string, cx int) string {
	r := []rune(s)
	cx = noteClamp(cx, 0, len(r))
	left := string(r[:cx])
	cur := " "
	right := ""
	if cx < len(r) {
		cur = string(r[cx])
		right = string(r[cx+1:])
	}
	return left + styleSel.Reverse(true).Render(cur) + right
}

// notepadClickFocus focuses the pad when its body is clicked (localY ≥ 1).
func (m *model) notepadClickFocus(localY int) {
	if localY >= 1 {
		m.notepad.focused = true
	}
}

func noteClamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// itoaTUI is a tiny int→string (avoids a strconv import churn here).
func itoaTUI(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// notepadAutosaveTick periodically flushes dirty notes so a crash/detach loses
// at most a couple seconds (focus-loss already flushes synchronously).
func (m *model) notepadAutosaveTick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return notepadSaveMsg{} })
}

type notepadSaveMsg struct{}

var _ = ansi.StringWidth
