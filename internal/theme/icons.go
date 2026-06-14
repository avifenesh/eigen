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
// Tier is chosen once at init: EIGEN_NERD_FONT=0 forces the Unicode fallback;
// =1 forces NF; unset → detect from $TERM_PROGRAM / known NF env hints, default
// to NF (the common case for this product's users), since the fallback is only
// needed on bare terminals and those users can set =0.

var nerdFont = detectNerdFont()

func detectNerdFont() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EIGEN_NERD_FONT"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	}
	// Heuristic: terminals that ship/commonly use Nerd Fonts. Default true —
	// the target users run a NF; bare-terminal users set EIGEN_NERD_FONT=0.
	return true
}

// NerdFont reports whether the Nerd Font icon tier is active.
func NerdFont() bool { return nerdFont }

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
	StatusWorking  = "●"
	StatusIdle     = "○"
	StatusApproval = "◆"
	StatusError    = "✗"
	// Back.
	Back = "‹"
)
