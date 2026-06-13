package theme

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Swatch renders the full design-system palette — every role with its meaning,
// the animation ramps, and the glyph vocabulary — as a single styled block.
// It's the "living swatch" from docs/design-system.md: run `eigen theme` to
// eyeball the whole system in one place and to verify a re-theme. Pure string;
// no terminal control beyond lipgloss styling.
func Swatch() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(Title).Bold(true)
	hdr := lipgloss.NewStyle().Foreground(Accent).Bold(true)
	b.WriteString(title.Render("eigen design system") + SFaint.Render("  — roles, not hues") + "\n\n")

	// Roles: a swatch chip + the role name (in its own color) + meaning.
	b.WriteString(hdr.Render("roles") + "\n")
	type role struct {
		name  string
		style lipgloss.Style
		desc  string
	}
	roles := []role{
		{"Text", SText, "primary prose, assistant answers"},
		{"Dim", SDim, "secondary: instructions, metadata, inactive rows"},
		{"Faint", SFaint, "chrome: separators, hairlines, disabled"},
		{"Accent", SAccent, "BRAND blue — borders, rules, caret, nav, wordmark"},
		{"Title", STitle, "brand cyan — headings, your voice, model name"},
		{"Focus", SFocus, "the active session THIS pane drives (non-brand)"},
		{"Sel", SSel, "selected row / cursor in a list or picker (non-brand)"},
		{"Ok", SOk, "success / available / idle-ok"},
		{"Warn", SWarn, "attention / confirm prompts"},
		{"Err", SErr, "failure / missing / error"},
		{"Tool", STool, "tool activity, counts, meta"},
		{"Code", SCode, "code + monospace spans"},
		{"Link", SLink, "links (underlined at call site)"},
		{"Working", SWorking, "the loud actively-thinking loader + ● status"},
	}
	chip := lipgloss.NewStyle().Padding(0, 1)
	for _, r := range roles {
		swatch := chip.Background(roleColor(r.name)).Render("  ")
		name := r.style.Render(fmt.Sprintf("%-9s", r.name))
		b.WriteString(fmt.Sprintf("  %s %s %s\n", swatch, name, SDim.Render(r.desc)))
	}

	// Animation ramps — show each frame's chip in sequence.
	b.WriteString("\n" + hdr.Render("ramps") + "\n")
	b.WriteString("  " + SDim.Render(fmt.Sprintf("%-9s", "Breath")) + " " + rampChips(BreathRamp) + SDim.Render("  brand-λ loader (working)") + "\n")
	b.WriteString("  " + SDim.Render(fmt.Sprintf("%-9s", "Working")) + " " + rampChips(WorkingRamp) + SDim.Render("  app live-session λ pulse") + "\n")

	// Weights.
	b.WriteString("\n" + hdr.Render("weight") + "\n")
	b.WriteString("  " + SText.Render("normal") + "   " + SText.Bold(true).Render("bold (the one thing to look at)") + "   " + SFaint.Render("faint (recede)") + "   " + SText.Italic(true).Render("italic (rare)") + "   " + SLink.Underline(true).Render("underline (links)") + "\n")

	// Glyph vocabulary.
	b.WriteString("\n" + hdr.Render("glyphs") + "\n")
	b.WriteString("  " + SAccent.Bold(true).Render("λ") + SDim.Render(" brand mark") + "    " +
		SWorking.Render("●") + SDim.Render(" working") + "  " +
		SDim.Render("○ idle") + "  " +
		SWarn.Render("◆") + SDim.Render(" approval") + "  " +
		SErr.Render("✗") + SDim.Render(" error") + "\n")
	b.WriteString("  " + SFocus.Bold(true).Render("❯") + SDim.Render(" you-are-here/prompt") + "   " +
		SDim.Render("▸/▾ collapse  ‹ back  ⏺ speak  ▶ read  ◉ voice  ⚒ tasks") + "\n")

	// Brand rule reminder.
	b.WriteString("\n" + SFaint.Render("brand rule: blue (Accent/Title) = brand + structure ONLY; selection/active/state use other roles.") + "\n")
	return b.String()
}

// rampChips renders a ramp as a row of background chips, left→right.
func rampChips(ramp []lipgloss.AdaptiveColor) string {
	var s strings.Builder
	for _, c := range ramp {
		s.WriteString(lipgloss.NewStyle().Background(c).Render("  "))
	}
	return s.String()
}

// roleColor maps a role name to its color for the swatch chip. Kept as a small
// switch (rather than reflection) so it's explicit and compile-checked.
func roleColor(name string) lipgloss.TerminalColor {
	switch name {
	case "Text":
		return Text
	case "Dim":
		return Dim
	case "Faint":
		return Faint
	case "Accent":
		return Accent
	case "Title":
		return Title
	case "Focus":
		return Focus
	case "Sel":
		return Sel
	case "Ok":
		return Ok
	case "Warn":
		return Warn
	case "Err":
		return Err
	case "Tool":
		return Tool
	case "Code":
		return Code
	case "Link":
		return Link
	case "Working":
		return Working
	}
	return Text
}
