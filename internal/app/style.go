// Package app is eigen's application shell — the TUI you live in. Bare `eigen`
// opens it: a paged interface (home, projects, sessions, config, skills,
// models, providers, memory, …) with a side rail, from which chat sessions are
// launched. `eigen <path>`, `eigen "task"`, and --resume bypass it and open a
// chat directly (those flows are unchanged).
//
// Design language: restrained and informative. One accent color for structure,
// dim greys for chrome, content carries the color. Nothing decorative that
// isn't informative.
package app

import (
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// Palette — sourced from internal/theme (shared with the chat TUI), so the
// app shell and the chat are one product. Calm desaturated truecolor.
var (
	cAccent  = theme.Accent  // structure
	cText    = theme.Text    // primary text
	cDim     = theme.Dim     // secondary text / instructions
	cFaint   = theme.Faint   // chrome, separators
	cTitle   = theme.Title   // titles, the active thing
	cOk      = theme.Ok      // healthy / available
	cWarn    = theme.Warn    // attention / confirm
	cErr     = theme.Err     // broken / missing
	cViolet  = theme.Tool    // counts, meta
	cFocus   = theme.Focus   // the active/live thing — non-brand (brand rule)
	cSel     = theme.Sel     // selected row / cursor — non-brand (brand rule)
	cWorking = theme.Working // a session is working (the loud orange axis)

	// Elevation surface (shared with the chat chrome — one product). Base is
	// the default terminal canvas (no fill needed); Overlay arrives with the
	// inspector/selection work.
	cSurface = theme.Surface // lifted panels (rail)
)

var (
	sText        = lipgloss.NewStyle().Foreground(cText)
	sDim         = lipgloss.NewStyle().Foreground(cDim)
	sFaint       = lipgloss.NewStyle().Foreground(cFaint)
	sTitle       = lipgloss.NewStyle().Foreground(cTitle).Bold(true)
	sAccent      = lipgloss.NewStyle().Foreground(cAccent)
	sWorkingText = lipgloss.NewStyle().Foreground(cWorking)
	sOk          = lipgloss.NewStyle().Foreground(cOk)
	sWarn        = lipgloss.NewStyle().Foreground(cWarn)
	sErr         = lipgloss.NewStyle().Foreground(cErr)
	sViolet      = lipgloss.NewStyle().Foreground(cViolet)

	// Rail item styles. The active rail page uses Focus (non-brand) so it
	// stands apart from the brand-blue title bar / borders (the brand rule).
	sRailActive = lipgloss.NewStyle().Foreground(cFocus).Bold(true)
	sRailIdle   = lipgloss.NewStyle().Foreground(cDim)

	// Selected row in a list: the keyboard-focus highlight in the Sel role
	// (non-brand), not brand blue — a quiet bold tint, not a loud bar.
	sRowSel = lipgloss.NewStyle().Foreground(cSel).Bold(true)
	sRowDim = lipgloss.NewStyle().Foreground(cText)
)

// sectionLabel renders a page section header as "label ─────" — a lowercase
// label followed by a faint hairline rule out to width w. Mirrors the chat
// sidebar's section dividers (internal/tui), so the app shell and the chat read
// as ONE product (one section-header treatment, not two).
func sectionLabel(label string, w int) string {
	label = strings.ToLower(label)
	if w <= 0 {
		return sFaint.Render(label)
	}
	lw := lipgloss.Width(label)
	if lw+2 > w {
		return sFaint.Render(truncate(label, w))
	}
	return sFaint.Render(label+" ") + sFaint.Render(strings.Repeat("─", w-lw-1))
}
