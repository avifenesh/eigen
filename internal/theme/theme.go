// Package theme is eigen's single source of truth for color and text styling.
// Both the chat TUI (internal/tui) and the app shell (internal/app) import it,
// so the two surfaces are guaranteed to look like one product — no drift.
//
// The palette is a calm, desaturated truecolor set (Nord-inspired): chosen for
// long, comfortable reading rather than maximum contrast. Roles, not hues, are
// the API — call sites ask for Text/Dim/Accent/Tool/Ok/…, never a raw color
// (the drift-guard test enforces this). Because every call site asks for a
// ROLE, a whole re-theme is one Palette swap: set EIGEN_THEME=<name> (or the
// config `theme` key) and the named palette below is selected at startup.
package theme

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette is the full set of role colors — the data a theme IS. Swapping the
// Palette re-themes the entire product because every style/ramp/role var below
// derives from the selected one. (Adaptive: Dark is the design target, Light
// keeps each role legible on a light terminal.)
type Palette struct {
	Name string

	Text, Dim, Faint lipgloss.AdaptiveColor
	Accent, Title    lipgloss.AdaptiveColor
	Ok, Warn, Err    lipgloss.AdaptiveColor
	Tool, Code, Link lipgloss.AdaptiveColor
	Heading          lipgloss.AdaptiveColor
	Working          lipgloss.AdaptiveColor
	Focus, Sel       lipgloss.AdaptiveColor
	OnBright         lipgloss.AdaptiveColor

	// Ramp stops (loader brightness cycles).
	AccentBright, FaintDim    lipgloss.AdaptiveColor
	WorkingDim, WorkingBright lipgloss.AdaptiveColor
}

// nordPalette — the default: calm, desaturated Nord-inspired truecolor.
var nordPalette = Palette{
	Name:          "nord",
	Text:          lipgloss.AdaptiveColor{Dark: "#D8DEE9", Light: "#2E3440"},
	Dim:           lipgloss.AdaptiveColor{Dark: "#9aa5b8", Light: "#4C566A"},
	Faint:         lipgloss.AdaptiveColor{Dark: "#79839a", Light: "#8a93a6"},
	Accent:        lipgloss.AdaptiveColor{Dark: "#81A1C1", Light: "#3B5A82"},
	Title:         lipgloss.AdaptiveColor{Dark: "#88C0D0", Light: "#2A7B8C"},
	Ok:            lipgloss.AdaptiveColor{Dark: "#A3BE8C", Light: "#4F6B36"},
	Warn:          lipgloss.AdaptiveColor{Dark: "#EBCB8B", Light: "#9A6B00"},
	Err:           lipgloss.AdaptiveColor{Dark: "#BF616A", Light: "#A01F2B"},
	Tool:          lipgloss.AdaptiveColor{Dark: "#B48EAD", Light: "#7A4E73"},
	Code:          lipgloss.AdaptiveColor{Dark: "#8FBCBB", Light: "#2A6E6C"},
	Link:          lipgloss.AdaptiveColor{Dark: "#88C0D0", Light: "#2A7B8C"},
	Heading:       lipgloss.AdaptiveColor{Dark: "#81A1C1", Light: "#3B5A82"},
	Working:       lipgloss.AdaptiveColor{Dark: "#D08770", Light: "#B4581F"},
	Focus:         lipgloss.AdaptiveColor{Dark: "#D1A0B0", Light: "#9A4D6B"},
	Sel:           lipgloss.AdaptiveColor{Dark: "#D1A0B0", Light: "#9A4D6B"},
	OnBright:      lipgloss.AdaptiveColor{Dark: "#1b1f27", Light: "#F0F4F8"},
	AccentBright:  lipgloss.AdaptiveColor{Dark: "#b3c4d8", Light: "#1f3450"},
	FaintDim:      lipgloss.AdaptiveColor{Dark: "#4a5365", Light: "#aab3c4"},
	WorkingDim:    lipgloss.AdaptiveColor{Dark: "#8a5a44", Light: "#c98a63"},
	WorkingBright: lipgloss.AdaptiveColor{Dark: "#e8a583", Light: "#9a4a18"},
}

// gruvboxPalette — the re-theme PROOF: a warmer, higher-contrast alternative.
// Same role semantics (brand blue, non-brand Focus/Sel, orange Working) in a
// different hue family — selecting it re-themes everything with zero call-site
// changes. (Gruvbox-inspired: warm greys, aqua "brand", orange working, a
// purple Focus that's still clearly non-brand.)
var gruvboxPalette = Palette{
	Name:          "gruvbox",
	Text:          lipgloss.AdaptiveColor{Dark: "#ebdbb2", Light: "#3c3836"},
	Dim:           lipgloss.AdaptiveColor{Dark: "#a89984", Light: "#665c54"},
	Faint:         lipgloss.AdaptiveColor{Dark: "#7c6f64", Light: "#928374"},
	Accent:        lipgloss.AdaptiveColor{Dark: "#83a598", Light: "#076678"}, // aqua-blue brand
	Title:         lipgloss.AdaptiveColor{Dark: "#8ec07c", Light: "#427b58"}, // aqua-green title
	Ok:            lipgloss.AdaptiveColor{Dark: "#b8bb26", Light: "#79740e"},
	Warn:          lipgloss.AdaptiveColor{Dark: "#fabd2f", Light: "#b57614"},
	Err:           lipgloss.AdaptiveColor{Dark: "#fb4934", Light: "#9d0006"},
	Tool:          lipgloss.AdaptiveColor{Dark: "#d3869b", Light: "#8f3f71"},
	Code:          lipgloss.AdaptiveColor{Dark: "#8ec07c", Light: "#427b58"},
	Link:          lipgloss.AdaptiveColor{Dark: "#83a598", Light: "#076678"},
	Heading:       lipgloss.AdaptiveColor{Dark: "#83a598", Light: "#076678"},
	Working:       lipgloss.AdaptiveColor{Dark: "#fe8019", Light: "#af3a03"}, // orange
	Focus:         lipgloss.AdaptiveColor{Dark: "#d3869b", Light: "#8f3f71"}, // purple (non-brand)
	Sel:           lipgloss.AdaptiveColor{Dark: "#d3869b", Light: "#8f3f71"},
	OnBright:      lipgloss.AdaptiveColor{Dark: "#1d2021", Light: "#fbf1c7"},
	AccentBright:  lipgloss.AdaptiveColor{Dark: "#bdddd0", Light: "#024450"},
	FaintDim:      lipgloss.AdaptiveColor{Dark: "#504945", Light: "#bdae93"},
	WorkingDim:    lipgloss.AdaptiveColor{Dark: "#a85a1f", Light: "#d98a3d"},
	WorkingBright: lipgloss.AdaptiveColor{Dark: "#fea95a", Light: "#8a2f02"},
}

// palettes is the registry of named themes (the re-theme menu).
var palettes = map[string]Palette{
	nordPalette.Name:    nordPalette,
	gruvboxPalette.Name: gruvboxPalette,
}

// PaletteNames lists the available theme names (for config option lists / docs).
func PaletteNames() []string { return []string{"nord", "gruvbox"} }

// Active is the palette in force (selected at init from EIGEN_THEME, default
// nord). Read-only after init.
var Active = selectPalette(os.Getenv("EIGEN_THEME"))

func selectPalette(name string) Palette {
	if p, ok := palettes[strings.ToLower(strings.TrimSpace(name))]; ok {
		return p
	}
	return nordPalette
}

// Role colors — assigned from the Active palette. Call sites reference these.
// (Kept as package vars, not Active.* accessors, so existing call sites and the
// S* styles below are unchanged; the re-theme happens by Active selection at
// init, before any importer's vars initialize.)
var (
	Text    = Active.Text
	Dim     = Active.Dim
	Faint   = Active.Faint
	Accent  = Active.Accent
	Title   = Active.Title
	Ok      = Active.Ok
	Warn    = Active.Warn
	Err     = Active.Err
	Tool    = Active.Tool
	Code    = Active.Code
	Link    = Active.Link
	Heading = Active.Heading
	Working = Active.Working
	Focus   = Active.Focus
	Sel     = Active.Sel

	OnBright      = Active.OnBright
	AccentBright  = Active.AccentBright
	FaintDim      = Active.FaintDim
	WorkingDim    = Active.WorkingDim
	WorkingBright = Active.WorkingBright
)

// Animation ramps — brightness cycles for the breathing/pulsing loaders. They
// live here (not at the call site) so every animated color is theme-owned too.
var (
	// BreathRamp is the brand-λ brightness cycle while a turn runs: a smooth
	// in/out (faint → dim → accent → bright → accent → dim → loop). Adaptive.
	BreathRamp = []lipgloss.AdaptiveColor{FaintDim, Faint, Accent, AccentBright, Accent, Faint}
	// WorkingRamp is the app-shell live-session λ pulse (Working-orange axis),
	// poll-paced: dim → working → bright → working → loop.
	WorkingRamp = []lipgloss.AdaptiveColor{WorkingDim, Working, WorkingBright, Working}
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
