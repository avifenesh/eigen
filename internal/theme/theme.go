// Package theme is eigen's single source of truth for color and text styling.
// Both the chat TUI (internal/tui) and the app shell (internal/app) import it,
// so the two surfaces are guaranteed to look like one product — no drift.
//
// The palette is a calm, desaturated truecolor set (Nord-inspired): chosen for
// long, comfortable reading rather than maximum contrast. Roles, not hues, are
// the API — call sites ask for Text/Dim/Accent/Tool/Ok/…, so a future re-theme
// is one edit here.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette — calm, adaptive truecolor. Each role carries a Dark variant
// (Nord-inspired, the design target: ghostty/kitty on a dark background) and a
// Light variant (darker/more saturated so the same role stays legible on a
// light terminal). lipgloss picks per the terminal's detected background, and
// degrades hex to 256-color where truecolor is unavailable.
var (
	// Neutrals: the reading surface. Text is primary; Dim is secondary
	// (instructions, metadata); Faint is chrome (separators, disabled).
	Text  = lipgloss.AdaptiveColor{Dark: "#D8DEE9", Light: "#2E3440"} // primary prose
	Dim   = lipgloss.AdaptiveColor{Dark: "#9aa5b8", Light: "#4C566A"} // secondary / instructions
	Faint = lipgloss.AdaptiveColor{Dark: "#79839a", Light: "#8a93a6"} // separators, hairlines

	// Accent: the calm structural blue for borders, rules, the caret.
	Accent = lipgloss.AdaptiveColor{Dark: "#81A1C1", Light: "#3B5A82"}

	// You / titles: the "active thing" and the user's own voice.
	Title = lipgloss.AdaptiveColor{Dark: "#88C0D0", Light: "#2A7B8C"}

	// Semantic status — desaturated Aurora, so signal without glare.
	Ok   = lipgloss.AdaptiveColor{Dark: "#A3BE8C", Light: "#4F6B36"} // success / available
	Warn = lipgloss.AdaptiveColor{Dark: "#EBCB8B", Light: "#9A6B00"} // attention / confirm
	Err  = lipgloss.AdaptiveColor{Dark: "#BF616A", Light: "#A01F2B"} // failure / missing

	// Content accents.
	Tool = lipgloss.AdaptiveColor{Dark: "#B48EAD", Light: "#7A4E73"} // tool activity, counts, meta
	Code = lipgloss.AdaptiveColor{Dark: "#8FBCBB", Light: "#2A6E6C"} // code, monospace spans
	Link = lipgloss.AdaptiveColor{Dark: "#88C0D0", Light: "#2A7B8C"} // links (underlined at call site)

	// Headings: calm blue, a touch of weight added at the call site.
	Heading = lipgloss.AdaptiveColor{Dark: "#81A1C1", Light: "#3B5A82"}

	// Working: the loud "actively thinking" color for the running loader — a
	// warm orange that stands apart from the calm blues so it's unmistakable
	// at a glance (distinct from Warn's amber, which is reserved for confirms).
	Working = lipgloss.AdaptiveColor{Dark: "#D08770", Light: "#B4581F"}

	// Focus: "the session THIS pane is attached to / the thing you're driving."
	// A deliberately NON-blue role — blue (Accent/Title) is reserved for brand
	// + structural chrome, so the active session must NOT share it (with 4
	// windows open, the active one has to pop against the brand palette). A
	// calm desaturated rose/mauve: distinct from Working-orange, Tool-violet,
	// and the semantic Ok/Warn/Err, while staying in the restrained family.
	Focus = lipgloss.AdaptiveColor{Dark: "#D1A0B0", Light: "#9A4D6B"}
)

// Ready-made styles for the common roles. Call sites compose (Bold/Underline/
// Italic) on top as needed.
var (
	SText    = lipgloss.NewStyle().Foreground(Text)
	SDim     = lipgloss.NewStyle().Foreground(Dim)
	SFaint   = lipgloss.NewStyle().Foreground(Faint)
	SAccent  = lipgloss.NewStyle().Foreground(Accent)
	STitle   = lipgloss.NewStyle().Foreground(Title)
	SOk      = lipgloss.NewStyle().Foreground(Ok)
	SWarn    = lipgloss.NewStyle().Foreground(Warn)
	SErr     = lipgloss.NewStyle().Foreground(Err)
	STool    = lipgloss.NewStyle().Foreground(Tool)
	SCode    = lipgloss.NewStyle().Foreground(Code)
	SLink    = lipgloss.NewStyle().Foreground(Link)
	SHeading = lipgloss.NewStyle().Foreground(Heading)
	SWorking = lipgloss.NewStyle().Foreground(Working)
	SFocus   = lipgloss.NewStyle().Foreground(Focus)
)
