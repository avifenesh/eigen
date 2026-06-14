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

	// Elevation surfaces — the "construction" the user wants (Warp-like): depth
	// without shadows. Base is the canvas; Surface lifts panels (rail, right
	// panel, code blocks) a hair; Overlay lifts popovers/selection higher still.
	// SurfaceText/OverlayText are the readable fg on those tints when needed.
	Base, Surface, Overlay lipgloss.AdaptiveColor

	// Neutral text tiers (primary → ghost). Ghost is below Faint: the quietest
	// legible text (placeholders, disabled, deep metadata).
	Text, Dim, Faint, Ghost lipgloss.AdaptiveColor

	Accent, Title    lipgloss.AdaptiveColor
	Ok, Warn, Err    lipgloss.AdaptiveColor
	Tool, Code, Link lipgloss.AdaptiveColor
	Heading          lipgloss.AdaptiveColor
	Working          lipgloss.AdaptiveColor
	Focus, Sel       lipgloss.AdaptiveColor
	OnBright         lipgloss.AdaptiveColor

	// Diff line backgrounds — faint red/green-tinted surfaces behind removed/
	// added lines so a diff reads like a real diff viewer (the code on the line
	// stays syntax-highlighted; the tint signals the change).
	AddBg, DelBg lipgloss.AdaptiveColor

	// Ramp stops (loader brightness cycles).
	AccentBright, FaintDim    lipgloss.AdaptiveColor
	WorkingDim, WorkingBright lipgloss.AdaptiveColor

	// Spectrum — the brand's signature axis (eigenvalues = a matrix's spectrum).
	// A cyan→indigo→violet sweep the λ shimmers across in signature moments
	// (welcome, loader peak). Cool, with clear direction.
	Spectrum []lipgloss.AdaptiveColor
}

// nordPalette — the default: calm, desaturated Nord-inspired truecolor.
// deepTealPalette — the DEFAULT (user-chosen 2026-06-14): rich petrol/jewel
// teal brand on deep near-black, with a warm clay Focus so "where you are"
// pops against the cool brand. Bold via presence, not saturation. Elevation:
// base (canvas) → surface (panels) → overlay (selection/popovers). Dark-only
// for now; Light mirrors the Dark values (light mode is out of scope, but the
// AdaptiveColor needs a value).
var deepTealPalette = Palette{
	Name:     "deepteal",
	Base:     lipgloss.AdaptiveColor{Dark: "#0B0E0F", Light: "#0B0E0F"},
	Surface:  lipgloss.AdaptiveColor{Dark: "#11171A", Light: "#11171A"},
	Overlay:  lipgloss.AdaptiveColor{Dark: "#1A2428", Light: "#1A2428"},
	Text:     lipgloss.AdaptiveColor{Dark: "#DDE4E3", Light: "#DDE4E3"},
	Dim:      lipgloss.AdaptiveColor{Dark: "#7E8E8B", Light: "#7E8E8B"},
	Faint:    lipgloss.AdaptiveColor{Dark: "#52605E", Light: "#52605E"},
	Ghost:    lipgloss.AdaptiveColor{Dark: "#37423F", Light: "#37423F"},
	Accent:   lipgloss.AdaptiveColor{Dark: "#3E9E96", Light: "#2A6E68"},
	Title:    lipgloss.AdaptiveColor{Dark: "#69C2B8", Light: "#2A6E68"},
	Ok:       lipgloss.AdaptiveColor{Dark: "#7BA86B", Light: "#4F6B36"},
	Warn:     lipgloss.AdaptiveColor{Dark: "#C9A24B", Light: "#8A6B12"},
	Err:      lipgloss.AdaptiveColor{Dark: "#C06A5E", Light: "#9A3B2E"},
	Tool:     lipgloss.AdaptiveColor{Dark: "#9E7BA6", Light: "#6E4E76"},
	Code:     lipgloss.AdaptiveColor{Dark: "#5FA89E", Light: "#2A6E68"},
	Link:     lipgloss.AdaptiveColor{Dark: "#69C2B8", Light: "#2A6E68"},
	Heading:  lipgloss.AdaptiveColor{Dark: "#3E9E96", Light: "#2A6E68"},
	Working:  lipgloss.AdaptiveColor{Dark: "#D08C5E", Light: "#A85E2A"},
	Focus:    lipgloss.AdaptiveColor{Dark: "#D08C5E", Light: "#A85E2A"},
	Sel:      lipgloss.AdaptiveColor{Dark: "#D08C5E", Light: "#A85E2A"},
	OnBright: lipgloss.AdaptiveColor{Dark: "#0B0E0F", Light: "#F0F4F3"},
	AddBg:    lipgloss.AdaptiveColor{Dark: "#10261C", Light: "#DCEFE0"},
	DelBg:    lipgloss.AdaptiveColor{Dark: "#2A1517", Light: "#F2DCDC"},
	// Loader ramp stops (brand-teal brightness cycle).
	AccentBright: lipgloss.AdaptiveColor{Dark: "#8AD6CC", Light: "#1F5650"},
	FaintDim:     lipgloss.AdaptiveColor{Dark: "#33403D", Light: "#9AACA8"},
	// Working pulse (warm clay axis).
	WorkingDim:    lipgloss.AdaptiveColor{Dark: "#8A5E3E", Light: "#C98A63"},
	WorkingBright: lipgloss.AdaptiveColor{Dark: "#E8A878", Light: "#8A4A18"},
	// Spectrum — teal→aqua→cyan→indigo sweep for the λ's signature moments.
	Spectrum: []lipgloss.AdaptiveColor{
		{Dark: "#3E9E96", Light: "#2A6E68"},
		{Dark: "#69C2B8", Light: "#2A7B72"},
		{Dark: "#5BB6C9", Light: "#2A6E80"},
		{Dark: "#6F9BD0", Light: "#3B5A82"},
	},
}

var nordPalette = Palette{
	Name:          "nord",
	Base:          lipgloss.AdaptiveColor{Dark: "#1b1f27", Light: "#F0F4F8"},
	Surface:       lipgloss.AdaptiveColor{Dark: "#222734", Light: "#E3E9F2"},
	Overlay:       lipgloss.AdaptiveColor{Dark: "#2b3140", Light: "#D6DEEC"},
	Ghost:         lipgloss.AdaptiveColor{Dark: "#5b657a", Light: "#9aa3b6"},
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
	AddBg:         lipgloss.AdaptiveColor{Dark: "#1e2a22", Light: "#DCEFE0"},
	DelBg:         lipgloss.AdaptiveColor{Dark: "#2e2026", Light: "#F2DCDC"},
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
	AddBg:         lipgloss.AdaptiveColor{Dark: "#28280f", Light: "#e6e2b0"},
	DelBg:         lipgloss.AdaptiveColor{Dark: "#321a16", Light: "#f0d8cc"},
	AccentBright:  lipgloss.AdaptiveColor{Dark: "#bdddd0", Light: "#024450"},
	FaintDim:      lipgloss.AdaptiveColor{Dark: "#504945", Light: "#bdae93"},
	WorkingDim:    lipgloss.AdaptiveColor{Dark: "#a85a1f", Light: "#d98a3d"},
	WorkingBright: lipgloss.AdaptiveColor{Dark: "#fea95a", Light: "#8a2f02"},
}

// palettes is the registry of named themes (the re-theme menu).
var palettes = map[string]Palette{
	deepTealPalette.Name: deepTealPalette,
	nordPalette.Name:     nordPalette,
	gruvboxPalette.Name:  gruvboxPalette,
}

// PaletteNames lists the available theme names (for config option lists / docs).
func PaletteNames() []string { return []string{"deepteal", "nord", "gruvbox"} }

// Active is the palette in force (selected at init from EIGEN_THEME, default
// deepteal — the user-chosen luxury palette). Read-only after init.
var Active = selectPalette(os.Getenv("EIGEN_THEME"))

func selectPalette(name string) Palette {
	if p, ok := palettes[strings.ToLower(strings.TrimSpace(name))]; ok {
		return p
	}
	return deepTealPalette
}

// Role colors — assigned from the Active palette. Call sites reference these.
// (Kept as package vars, not Active.* accessors, so existing call sites and the
// S* styles below are unchanged; the re-theme happens by Active selection at
// init, before any importer's vars initialize.)
var (
	Base    = Active.Base
	Surface = Active.Surface
	Overlay = Active.Overlay
	Text    = Active.Text
	Dim     = Active.Dim
	Faint   = Active.Faint
	Ghost   = Active.Ghost
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
	AddBg         = Active.AddBg
	DelBg         = Active.DelBg
	AccentBright  = Active.AccentBright
	FaintDim      = Active.FaintDim
	WorkingDim    = Active.WorkingDim
	WorkingBright = Active.WorkingBright

	Spectrum = Active.Spectrum
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
	SText  = lipgloss.NewStyle().Foreground(Text)
	SGhost = lipgloss.NewStyle().Foreground(Ghost)
	// Surface styles: foreground tiers ON the lifted panel tint. Use these for
	// content that sits inside a Surface-filled region so fg/bg stay paired.
	SSurface = lipgloss.NewStyle().Background(Surface)
	SOverlay = lipgloss.NewStyle().Background(Overlay)
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
