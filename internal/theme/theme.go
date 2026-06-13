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

	// Sel: "the selected row / cursor in a list or picker" — the keyboard
	// focus highlight, distinct from brand blue (the brand rule). Same
	// attention family as Focus (rose) by design: "where you are" reads as one
	// idea whether it's the active session or the highlighted row. Kept a
	// separate role so call sites stay semantic and the two can diverge later.
	Sel = lipgloss.AdaptiveColor{Dark: "#D1A0B0", Light: "#9A4D6B"}

	// OnBright: text/glyph color to place ON a brightly-filled background
	// (the flash pill, any reverse badge) — a near-black on dark terminals,
	// near-white on light — so the label stays legible over Ok/Warn/Err fills.
	OnBright = lipgloss.AdaptiveColor{Dark: "#1b1f27", Light: "#F0F4F8"}

	// AccentBright: the inhale peak of the brand blue, brighter than Accent —
	// used only by the breathing-λ loader ramp (not a general role).
	AccentBright = lipgloss.AdaptiveColor{Dark: "#b3c4d8", Light: "#1f3450"}
	// FaintDim: a step below Faint — the exhale trough of the loader ramp.
	FaintDim = lipgloss.AdaptiveColor{Dark: "#4a5365", Light: "#aab3c4"}
	// WorkingDim / WorkingBright: the trough/peak of the app-shell live-session
	// λ pulse (the Working orange axis), so that ramp also lives in the theme.
	WorkingDim    = lipgloss.AdaptiveColor{Dark: "#8a5a44", Light: "#c98a63"}
	WorkingBright = lipgloss.AdaptiveColor{Dark: "#e8a583", Light: "#9a4a18"}
)

// Animation ramps — brightness cycles for the breathing/pulsing loaders. They
// live here (not at the call site) so every animated color is theme-owned too.
var (
	// BreathRamp is the brand-λ brightness cycle while a turn runs: a smooth
	// in/out (faint → dim → accent → bright → accent → dim → loop). Adaptive.
	BreathRamp = []lipgloss.AdaptiveColor{
		FaintDim,     // exhaled (trough)
		Faint,        //
		Accent,       // the mark's rest color
		AccentBright, // full inhale (peak)
		Accent,       //
		Faint,        //
	}
	// WorkingRamp is the app-shell live-session λ pulse (Working-orange axis),
	// poll-paced: dim → working → bright → working → loop.
	WorkingRamp = []lipgloss.AdaptiveColor{
		WorkingDim,
		Working,
		WorkingBright,
		Working,
	}
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
	SSel     = lipgloss.NewStyle().Foreground(Sel)
)
