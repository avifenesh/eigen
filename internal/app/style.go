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
	cWorking = theme.Working // loud "actively working"
)

var (
	sText   = lipgloss.NewStyle().Foreground(cText)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sFaint  = lipgloss.NewStyle().Foreground(cFaint)
	sTitle  = lipgloss.NewStyle().Foreground(cTitle).Bold(true)
	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sOk     = lipgloss.NewStyle().Foreground(cOk)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sErr    = lipgloss.NewStyle().Foreground(cErr)
	sViolet = lipgloss.NewStyle().Foreground(cViolet)

	// Rail item styles.
	sRailActive = lipgloss.NewStyle().Foreground(cTitle).Bold(true)
	sRailIdle   = lipgloss.NewStyle().Foreground(cDim)

	// Selected row in a list: a quiet reverse on the accent, not a loud bar.
	sRowSel = lipgloss.NewStyle().Foreground(cTitle).Bold(true)
	sRowDim = lipgloss.NewStyle().Foreground(cText)
)
