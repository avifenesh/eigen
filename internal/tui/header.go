package tui

// The header bar (Tier 9 Wave 2): one subtle line at the very top of the chat
// window — the session title + project breadcrumb on the left, and right-
// aligned action buttons [home] [sessions] [+new] [config]. It is the chat
// window's chrome "title bar": always present, click-dispatched through the
// same action registry as keys, and the title is the single rename surface
// (click it = inline rename prompt).

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/charmbracelet/x/ansi"
)

// headerHeight is the rows the bordered header occupies. It is a real chrome
// frame now (top border, content row, bottom border), so computeLayout/topHeight
// shift the transcript down by 3 rows.
func (m *model) headerHeight() int { return 3 }

// headerButton is one right-aligned action affordance with its action id and
// the plain label drawn (in brackets).
type headerButton struct {
	action actionID
	label  string
}

// headerButtons are the right-aligned actions, in draw order (left to right).
func (m *model) headerButtons() []headerButton {
	return []headerButton{
		{actHome, "home"},
		{actSwitcher, "sessions"},
		{actNewSession, "+new"},
		{actConfigPanel, "config"},
	}
}

// headerTitle is the session's display name (or a calm placeholder), used as
// the left-side label and the rename target.
func (m *model) headerTitle() string {
	if m.backend == nil {
		return "eigen"
	}
	if t := strings.TrimSpace(m.backend.Title()); t != "" {
		return t
	}
	return "untitled session"
}

// headerBreadcrumb is the project directory basename shown after the title.
func (m *model) headerBreadcrumb() string {
	dir := m.sessionDir()
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// sessionDir is the working directory of the session (for the breadcrumb). It
// reads the saved meta dir when available, else the process cwd.
func (m *model) sessionDir() string {
	if m.sessionPath != "" {
		if meta, ok := transcript.LoadMeta(m.sessionPath); ok && meta.Dir != "" {
			return meta.Dir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// headerButtonsText builds the right-aligned "[home] [sessions] …" string and
// the plain column where it begins (for hit-testing), given the total width.
func (m *model) headerButtonsText(w int) (plain string, startCol int) {
	// Buttons live on the header CONTENT row inside the left/right borders.
	if w >= 2 {
		w -= 2
	}
	var parts []string
	for _, b := range m.headerButtons() {
		parts = append(parts, "["+b.label+"]")
	}
	plain = strings.Join(parts, " ")
	startCol = w - ansi.StringWidth(plain)
	if startCol < 0 {
		startCol = 0
	}
	return plain, startCol
}

// headerView renders the header line: title · breadcrumb on the left, action
// buttons right-aligned, padded to the width.
func (m *model) headerView() string {
	w := m.width
	if w <= 0 {
		return ""
	}
	innerW := w - 2
	if innerW < 0 {
		innerW = 0
	}
	btnPlain, btnStart := m.headerButtonsText(w)
	// Left side: title + dim breadcrumb, truncated so it never collides with
	// the right-aligned buttons.
	title := m.headerTitle()
	crumb := m.headerBreadcrumb()
	leftMax := btnStart - 1
	if leftMax < 0 {
		leftMax = 0
	}
	left := styleUser.Render(ansiTrunc(title, leftMax))
	used := ansi.StringWidth(ansiTrunc(title, leftMax))
	if crumb != "" && used+3 < leftMax {
		seg := "  " + crumb
		seg = ansiTrunc(seg, leftMax-used)
		left += dim(seg)
		used += ansi.StringWidth(seg)
	}
	// Styled buttons: enabled ones in the accent palette, disabled ones dim.
	var rb strings.Builder
	for i, b := range m.headerButtons() {
		if i > 0 {
			rb.WriteString(" ")
		}
		lbl := "[" + b.label + "]"
		if a, ok := actionRegistry[b.action]; ok && a.enabled != nil && !a.enabled(m) {
			rb.WriteString(dim(lbl))
		} else {
			rb.WriteString(styleAccent.Render(lbl))
		}
	}
	// Pad the gap between the left text and the right buttons.
	gap := btnStart - used
	if gap < 1 {
		gap = 1
	}
	_ = btnPlain
	content := left + strings.Repeat(" ", gap) + rb.String()
	contentW := ansi.StringWidth(ansi.Strip(content))
	if contentW < innerW {
		content += strings.Repeat(" ", innerW-contentW)
	}
	top := styleAccent.Render("╭" + strings.Repeat("─", innerW) + "╮")
	mid := styleAccent.Render("│") + content + styleAccent.Render("│")
	bot := styleAccent.Render("╰" + strings.Repeat("─", innerW) + "╯")
	return top + "\n" + mid + "\n" + bot
}

// headerActionAt resolves a click within the header rect (local coords) to an
// action: the title region opens rename, a right-aligned button its action.
func (m *model) headerActionAt(localX, localY int) actionID {
	// Only the content row is interactive; top/bottom borders are no-op.
	if localY != 1 {
		return actNone
	}
	// localX includes the left border; shift to content-local columns.
	localX--
	if localX < 0 || localX >= m.width-2 {
		return actNone
	}
	w := m.width
	btnPlain, btnStart := m.headerButtonsText(w)
	// Inside the buttons span: find which button by walking the labels.
	if localX >= btnStart && localX < btnStart+ansi.StringWidth(btnPlain) {
		col := btnStart
		for _, b := range m.headerButtons() {
			lbl := "[" + b.label + "]"
			lw := ansi.StringWidth(lbl)
			if localX >= col && localX < col+lw {
				return b.action
			}
			col += lw + 1 // + the separating space
		}
		return actNone
	}
	// Left region (the title): clicking it renames.
	if localX < btnStart {
		return actRename
	}
	return actNone
}

// ansiTrunc truncates a plain string to max display columns, appending an
// ellipsis when it was cut. Safe for the (plain) header labels.
func ansiTrunc(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	r := []rune(s)
	out := make([]rune, 0, len(r))
	w := 0
	for _, c := range r {
		cw := ansi.StringWidth(string(c))
		if w+cw > max-1 {
			break
		}
		out = append(out, c)
		w += cw
	}
	return string(out) + "…"
}
