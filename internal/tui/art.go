package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// greeting returns a warm, time-of-day salutation — the room feels lived-in,
// not like a cold prompt.
func greeting() string {
	switch h := time.Now().Hour(); {
	case h < 5:
		return "burning the midnight oil"
	case h < 12:
		return "good morning"
	case h < 17:
		return "good afternoon"
	case h < 22:
		return "good evening"
	default:
		return "late night"
	}
}

// welcomeView renders the empty-transcript welcome: the wordmark, a one-line
// identity, and a few starter affordances — all calm, centered to the
// transcript width. Returns "" when there's no room (tiny terminals).
func (m *model) welcomeView(width, height int) string {
	if width < 44 || height < 12 {
		// Too small for art — a quiet centered one-liner with the brand mark.
		msg := theme.SAccent.Bold(true).Render("λ ") + theme.SDim.Render("eigen · type a task to begin")
		if pad := (width - ansi.StringWidth(msg)) / 2; pad > 0 {
			msg = strings.Repeat(" ", pad) + msg
		}
		return msg
	}
	lines := make([]string, 0, len(eigenArt)+8)
	// The wordmark sweeps the brand spectrum top→bottom (teal→aqua→cyan→indigo)
	// — a signature shimmer, the eigenvalue "spectrum" made literal.
	for i, a := range eigenArt {
		c := theme.Accent
		if len(theme.Spectrum) > 0 {
			c = theme.Spectrum[i*len(theme.Spectrum)/len(eigenArt)]
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(c).Bold(true).Render(a))
	}
	lines = append(lines, "")
	lines = append(lines, theme.STitle.Bold(true).Render(greeting())+theme.SFaint.Render("  ·  ")+theme.SDim.Render("what are we building?"))
	lines = append(lines, "")
	// Starter hints: the few things worth knowing on a blank slate, each led by
	// its icon from the coherent set.
	hints := []struct{ icon, key, what string }{
		{theme.Caret, "type", "ask for anything, or describe a task"},
		{theme.ToolIcon("task"), "/help", "commands · ctrl+k for the palette"},
		{"@", "@file", "reference a file · ◉ voice to talk"},
	}
	for _, h := range hints {
		lines = append(lines,
			theme.SAccent.Render(fmt.Sprintf("%2s", h.icon))+" "+
				theme.STitle.Render(fmt.Sprintf("%-6s", h.key))+
				theme.SFaint.Render(" ·  ")+theme.SDim.Render(h.what))
	}
	// Center each line individually by its true display width (ansi.StringWidth
	// counts the em-dash and wide glyphs correctly — lipgloss block-centering
	// miscounts them and drifts).
	for i, ln := range lines {
		w := ansi.StringWidth(ln)
		if pad := (width - w) / 2; pad > 0 {
			lines[i] = strings.Repeat(" ", pad) + ln
		}
	}
	block := strings.Join(lines, "\n")
	// Nudge it down a third of the height so it sits in the optical center.
	pad := (height - len(lines)) / 3
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat("\n", pad) + block
}
