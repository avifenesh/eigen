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

// headerHeight is the rows the bordered header occupies: a real chrome frame
// (top border, content row, bottom border). On very short terminals the
// border is dropped (1 content row) so the chrome never starves the
// transcript+input of rows. In sidebar mode (Tier 11.5) there is no header at
// all — the left command sidebar owns the chrome.
func (m *model) headerHeight() int {
	if m.sidebarVisible() {
		return 0
	}
	if m.height < headerBorderMinRows {
		return 1
	}
	return 3
}

// headerBorderMinRows is the terminal height below which the header drops its
// border frame and renders as a single line.
const headerBorderMinRows = 14

// headerButton is one right-aligned action affordance with its action id and
// the plain label drawn (in brackets).
type headerButton struct {
	action actionID
	label  string
}

// headerButtons are the right-aligned actions, in draw order (left to right).
// The trailing ◧/◨ are the side-panel layout toggles (lit = shown), the
// usual "side panel" affordance — one click opens or closes.
func (m *model) headerButtons() []headerButton {
	return []headerButton{
		{actHome, "home"},
		{actSwitcher, "sessions"},
		{actNewSession, "+new"},
		{actConfigPanel, "config"},
		{actRailToggle, "◧"},
		{actChangesToggle, "◨"},
	}
}

// headerToggleOn reports whether a header button is a panel toggle whose panel
// is currently SHOWN (rendered lit instead of action-colored).
func (m *model) headerToggleOn(a actionID) (on, isToggle bool) {
	switch a {
	case actRailToggle:
		return m.railOn && m.railVisible(), true
	case actChangesToggle:
		return m.changesOn && m.changesVisible(), true
	}
	return false, false
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

// visibleHeaderButtons is the subset of header buttons that fits the content
// width: buttons drop from the right when the terminal is too narrow (a
// truncated button row would be an unclickable lie). Render and hit-test both
// use this, so geometry can't drift.
func (m *model) visibleHeaderButtons(innerW int) []headerButton {
	btns := m.headerButtons()
	for len(btns) > 0 {
		w := 0
		for i, b := range btns {
			if i > 0 {
				w++ // separating space
			}
			w += ansi.StringWidth(b.label) + 2 // [label]
		}
		if w <= innerW-2 { // leave a couple of columns for the title
			return btns
		}
		btns = btns[:len(btns)-1]
	}
	return nil
}

// headerButtonsText builds the right-aligned "[home] [sessions] …" string and
// the plain column where it begins (for hit-testing), given the total width.
func (m *model) headerButtonsText(w int) (plain string, startCol int) {
	// Buttons live on the header CONTENT row inside the left/right borders
	// (when bordered).
	if m.headerHeight() == 3 && w >= 2 {
		w -= 2
	}
	var parts []string
	for _, b := range m.visibleHeaderButtons(w) {
		parts = append(parts, "["+b.label+"]")
	}
	plain = strings.Join(parts, " ")
	startCol = w - ansi.StringWidth(plain)
	if startCol < 0 {
		startCol = 0
	}
	return plain, startCol
}

// headerView renders the header: title · breadcrumb on the left, action
// buttons right-aligned, padded to the width. Bordered (3 rows) normally;
// a single line on very short terminals.
func (m *model) headerView() string {
	w := m.width
	if w <= 0 {
		return ""
	}
	bordered := m.headerHeight() == 3
	innerW := w
	if bordered {
		innerW = w - 2
	}
	if innerW < 0 {
		innerW = 0
	}
	_, btnStart := m.headerButtonsText(w)
	btns := m.visibleHeaderButtons(innerW)
	// Left side: title + dim breadcrumb, truncated so it never collides with
	// the right-aligned buttons.
	title := m.headerTitle()
	crumb := m.headerBreadcrumb()
	leftMax := btnStart - 1
	if len(btns) == 0 {
		leftMax = innerW
	}
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
	// Panel toggles show their state: lit (accent) when the panel is shown,
	// dim when hidden — so the header always says how to reopen a panel.
	var rb strings.Builder
	for i, b := range btns {
		if i > 0 {
			rb.WriteString(" ")
		}
		lbl := "[" + b.label + "]"
		if on, isToggle := m.headerToggleOn(b.action); isToggle {
			if on {
				rb.WriteString(styleAccent.Render(lbl))
			} else {
				rb.WriteString(dim(lbl))
			}
			continue
		}
		if a, ok := actionRegistry[b.action]; ok && a.enabled != nil && !a.enabled(m) {
			rb.WriteString(dim(lbl))
		} else {
			rb.WriteString(styleAccent.Render(lbl))
		}
	}
	content := left
	if len(btns) > 0 {
		gap := btnStart - used
		if gap < 1 {
			gap = 1
		}
		content += strings.Repeat(" ", gap) + rb.String()
	}
	contentW := ansi.StringWidth(ansi.Strip(content))
	if contentW < innerW {
		content += strings.Repeat(" ", innerW-contentW)
	} else if contentW > innerW {
		content = ansi.Truncate(content, innerW, "")
	}
	if !bordered {
		return content
	}
	top := styleAccent.Render("╭" + strings.Repeat("─", innerW) + "╮")
	mid := styleAccent.Render("│") + content + styleAccent.Render("│")
	bot := styleAccent.Render("╰" + strings.Repeat("─", innerW) + "╯")
	return top + "\n" + mid + "\n" + bot
}

// headerActionAt resolves a click within the header rect (local coords) to an
// action: the title region opens rename, a right-aligned button its action.
func (m *model) headerActionAt(localX, localY int) actionID {
	bordered := m.headerHeight() == 3
	innerW := m.width
	if bordered {
		// Only the content row is interactive; top/bottom borders are no-op.
		if localY != 1 {
			return actNone
		}
		// localX includes the left border; shift to content-local columns.
		localX--
		innerW = m.width - 2
	} else if localY != 0 {
		return actNone
	}
	if localX < 0 || localX >= innerW {
		return actNone
	}
	btnPlain, btnStart := m.headerButtonsText(m.width)
	// Inside the buttons span: find which button by walking the labels.
	if localX >= btnStart && localX < btnStart+ansi.StringWidth(btnPlain) {
		col := btnStart
		for _, b := range m.visibleHeaderButtons(innerW) {
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
		return "⋯"
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
	return string(out) + "⋯"
}
