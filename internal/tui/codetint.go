package tui

import (
	"regexp"
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// Lightweight syntax tinting for fenced code — NOT a parser, just tasteful
// token coloring so code reads like an editor instead of flat teal. Three
// passes that are safe across most languages: comments, string literals, and a
// common keyword set. Everything is painted with the Surface background so the
// tints sit on the code-block surface (fillBG re-asserts Surface after resets,
// but spans carry their own bg so they never tear a hole).

var (
	// fg-on-surface styles for code tokens.
	tintComment = lipgloss.NewStyle().Foreground(theme.Ghost).Background(theme.Surface).Italic(true)
	tintString  = lipgloss.NewStyle().Foreground(theme.Ok).Background(theme.Surface)
	tintKeyword = lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface).Bold(true)
	tintNumber  = lipgloss.NewStyle().Foreground(theme.Tool).Background(theme.Surface)
)

// codeKeywords is a cross-language set — common enough to read as "highlighting"
// without per-language grammars. Matched as whole words only.
var codeKeywords = map[string]bool{
	"func": true, "return": true, "if": true, "else": true, "for": true,
	"range": true, "var": true, "const": true, "type": true, "struct": true,
	"interface": true, "package": true, "import": true, "go": true, "defer": true,
	"map": true, "chan": true, "select": true, "switch": true, "case": true,
	"break": true, "continue": true, "fallthrough": true, "default": true,
	"def": true, "class": true, "lambda": true, "async": true, "await": true,
	"let": true, "fn": true, "use": true, "mut": true, "pub": true, "impl": true,
	"match": true, "while": true, "true": true, "false": true, "nil": true,
	"null": true, "None": true, "self": true, "this": true, "new": true,
	"throw": true, "try": true, "catch": true, "export": true, "from": true,
	"yield": true, "with": true, "in": true, "and": true, "or": true, "not": true,
}

var reCodeNumber = regexp.MustCompile(`\b\d+(\.\d+)?\b`)

// tintCodeLine tints one already-tab-expanded code line. base is the default
// fg-on-surface style for untinted code. It handles a line comment (// or #),
// double/single-quoted strings, then keywords/numbers in the remainder.
func tintCodeLine(line string, base lipgloss.Style) string {
	// Split off a trailing line comment (// or #) — only when not inside a
	// string. Cheap heuristic: find the first // or # that isn't quoted.
	code, comment := splitComment(line)

	var b strings.Builder
	tintCodeSegment(&b, code, base)
	if comment != "" {
		b.WriteString(tintComment.Render(comment))
	}
	return b.String()
}

// splitComment returns (code, comment) splitting at the first unquoted // or #.
func splitComment(s string) (string, string) {
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'' || c == '`':
			inStr = c
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			return s[:i], s[i:]
		case c == '#':
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// tintCodeSegment tints strings, then keywords/numbers in the non-comment code.
func tintCodeSegment(b *strings.Builder, s string, base lipgloss.Style) {
	i := 0
	for i < len(s) {
		c := s[i]
		// String literal: copy verbatim, tinted.
		if c == '"' || c == '\'' || c == '`' {
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(s) {
				j++ // include closing quote
			}
			b.WriteString(tintString.Render(s[i:j]))
			i = j
			continue
		}
		// Word (keyword?) or number, else a run of other chars.
		if isWordByte(c) {
			j := i
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
			word := s[i:j]
			switch {
			case codeKeywords[word]:
				b.WriteString(tintKeyword.Render(word))
			case reCodeNumber.MatchString(word) && word[0] >= '0' && word[0] <= '9':
				b.WriteString(tintNumber.Render(word))
			default:
				b.WriteString(base.Render(word))
			}
			i = j
			continue
		}
		// Other char (punctuation/space): base style, batched.
		j := i
		for j < len(s) && !isWordByte(s[j]) && s[j] != '"' && s[j] != '\'' && s[j] != '`' {
			j++
		}
		b.WriteString(base.Render(s[i:j]))
		i = j
	}
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}
