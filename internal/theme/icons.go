package theme

import (
	"os"
	"strings"
)

// Icons — eigen's single, coherent icon vocabulary. Two tiers:
//
//   - Nerd Font glyphs (primary): richer, purpose-built dev icons, used when a
//     Nerd Font is present (the user runs JetBrainsMono Nerd Font in ghostty).
//   - Pure-Unicode fallback: a coherent monochrome geometric set that renders
//     everywhere, so eigen never shows tofu on a plain terminal.
//
// The rule the design system enforces: ONE icon per concept, monochrome
// line-art only — NO emoji (emoji are double-width, render per-font, and
// cheapen the luxury feel). Each tool/concept maps to exactly one glyph.
//
// Tier is chosen once at init: EIGEN_NERD_FONT=1 opts INTO the richer Nerd Font
// glyphs; 0/unset uses the pure-Unicode fallback. The fallback is the SAFE
// default — it renders on every terminal/font, so eigen never shows tofu out of
// the box. Users running a Nerd Font (e.g. JetBrainsMono NF in ghostty) set
// EIGEN_NERD_FONT=1 once for the richer icons.

var nerdFont = detectNerdFont()

func detectNerdFont() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EIGEN_NERD_FONT"))) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}
	// Best-effort hint: some setups advertise a Nerd Font in the font env.
	// Otherwise default to the Unicode fallback (no tofu anywhere).
	font := strings.ToLower(os.Getenv("EIGEN_FONT") + " " + os.Getenv("TERMINAL_FONT"))
	return strings.Contains(font, "nerd") || strings.Contains(font, "nf")
}

// NerdFont reports whether the Nerd Font icon tier is active.
func NerdFont() bool { return nerdFont }

// NerdFontMode returns the active tier as "on"/"off" — for config comparison
// (so the startup re-exec only fires when the configured tier differs).
func NerdFontMode() string {
	if nerdFont {
		return "on"
	}
	return "off"
}

// icon picks the NF glyph or the Unicode fallback for the active tier.
func icon(nf, fallback string) string {
	if nerdFont {
		return nf
	}
	return fallback
}

// Tool icons — one per tool family. NF glyphs are from the "dev" range; the
// fallbacks are a coherent geometric set (diamonds/lozenges, not emoji).
var (
	IconRead   = icon("\ue23f", "◇") //  read / view a file
	IconWrite  = icon("\uf040", "✎") //  write / create
	IconEdit   = icon("\uf044", "✎") //  edit / patch
	IconSearch = icon("\uf002", "⌕") //  grep / glob / find
	IconList   = icon("\uf03a", "≣") //  list a directory
	IconBash   = icon("\uf120", "»") //  shell / run command  (NOT ❯ — reserved for the prompt)
	IconFetch  = icon("\uf0ac", "⊕") //  fetch / web / network
	IconTask   = icon("\uf0e7", "◈") //  delegated subtask
	IconImage  = icon("\uf03e", "▦") //  image / generate
	IconTool   = icon("\uf0ad", "▪") //  generic tool
)

// ToolIcon maps a tool name to its icon (one place, used by the transcript).
func ToolIcon(name string) string {
	switch name {
	case "read":
		return IconRead
	case "write":
		return IconWrite
	case "edit", "multiedit", "apply_patch":
		return IconEdit
	case "grep", "glob", "search", "find", "retrieve":
		return IconSearch
	case "list":
		return IconList
	case "bash":
		return IconBash
	case "fetch", "websearch":
		return IconFetch
	case "task", "task_status", "task_group", "task_group_mutating":
		return IconTask
	case "generate_image":
		return IconImage
	default:
		return IconTool
	}
}

// Structural / status glyphs — the rest of the coherent vocabulary, so every
// symbol the UI uses comes from ONE place. (Status dots stay simple Unicode;
// they read well everywhere and don't need an NF tier.)
var (
	// "You are here" / the prompt caret — ONE meaning, reserved.
	Caret = "❯"
	// Expand / collapse — one pair.
	Expanded  = "▾"
	Collapsed = "▸"
	// Status: working / idle / approval-wait / error.
	// Status dots — width-1 glyphs (the plain round ● ○ and ◆ are East-Asian-
	// AMBIGUOUS, which some terminals render two cells wide, overflowing the
	// rail's column math and crossing the separator). These read one cell on
	// every terminal: filled ◉ working, dotted ◌ idle, lozenge ◊ approval.
	StatusWorking  = "◉"
	StatusIdle     = "◌"
	StatusApproval = "◊"
	StatusError    = "✗"
	// Back.
	Back = "‹"
	// Ellipsis — the ONE truncation marker. ⋯ (midline horizontal ellipsis) is
	// width-1 on every terminal; the typographic … is East-Asian-ambiguous
	// (renders 2 cells on ghostty etc.) and would overflow width-budgeted slots
	// where a truncated string is sliced to an exact width then a marker added.
	Ellipsis = "⋯"
	// Collapse-all / expand-all (the sessions-header toggle): a clear pair, not
	// a stray en-dash. ⊟ = collapse everything, ⊞ = expand everything.
	CollapseAll = "⊟"
	ExpandAll   = "⊞"
)
