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

import "github.com/charmbracelet/lipgloss"

// Palette — the same family as the chat TUI (cyan/violet/green/amber on grey),
// so the app and the chat feel like one product.
var (
	cAccent = lipgloss.Color("67")  // muted steel blue — structure
	cText   = lipgloss.Color("252") // primary text
	cDim    = lipgloss.Color("242") // secondary text
	cFaint  = lipgloss.Color("238") // chrome, separators
	cTitle  = lipgloss.Color("44")  // bright cyan — titles, the active thing
	cOk     = lipgloss.Color("78")  // green — healthy/available
	cWarn   = lipgloss.Color("215") // amber — attention/confirm
	cErr    = lipgloss.Color("203") // warm red — broken/missing
	cViolet = lipgloss.Color("141") // soft violet — counts, meta
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
