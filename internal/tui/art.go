package tui

import (
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// eigenArt is the wordmark shown on an empty transcript — a calm welcome that
// gives the app a face instead of a blank void. Rendered in the accent color,
// centered, with a tagline and a couple of starter hints below.
var eigenArt = []string{
	"  ███████  ██   ██████  ███████ ███    ██",
	"  ██       ██  ██       ██      ████   ██",
	"  █████    ██  ██   ███ █████   ██ ██  ██",
	"  ██       ██  ██    ██ ██      ██  ██ ██",
	"  ███████  ██   ██████  ███████ ██   ████",
}

// welcomeView renders the empty-transcript welcome: the wordmark, a one-line
// identity, and a few starter affordances — all calm, centered to the
// transcript width. Returns "" when there's no room (tiny terminals).
func (m *model) welcomeView(width, height int) string {
	if width < 44 || height < 12 {
		// Too small for art — a quiet one-liner keeps it from looking broken.
		return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).
			Render(theme.SDim.Render("eigen · type a task to begin"))
	}
	var b strings.Builder
	art := theme.SAccent.Render(strings.Join(eigenArt, "\n"))
	b.WriteString(art)
	b.WriteString("\n\n")
	b.WriteString(theme.SDim.Render("your coding agent — sessions live in the daemon, windows are views"))
	b.WriteString("\n\n")
	// Starter hints: the few things worth knowing on a blank slate.
	hints := []struct{ key, what string }{
		{"type", "ask for anything, or describe a task"},
		{"/help", "commands · ctrl+k for the palette"},
		{"@", "reference a file · ◉ voice to talk"},
	}
	for _, h := range hints {
		b.WriteString(theme.SAccent.Render(fmt.Sprintf("%8s", h.key)) +
			theme.SFaint.Render("  ·  ") + theme.SDim.Render(h.what) + "\n")
	}
	block := b.String()
	// Center the whole block horizontally; nudge it down a third of the height
	// so it sits in the optical center, not jammed at the top.
	centered := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(block)
	pad := (height - lipgloss.Height(block)) / 3
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat("\n", pad) + centered
}
