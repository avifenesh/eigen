package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// JSON rendering — tool results and ```json blocks are very often raw JSON, and
// a flat blob reads terribly. looksLikeJSON + renderJSON pretty-print (2-space
// indent) and tint the structure: keys, string values, numbers, booleans/null,
// and punctuation each get a role color so the shape is legible at a glance.

// looksLikeJSON reports whether s is a JSON object or array (the cases worth
// pretty-printing — a bare string/number isn't). Cheap prefix check first, then
// a validity check.
func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) < 2 {
		return false
	}
	if !((t[0] == '{' && t[len(t)-1] == '}') || (t[0] == '[' && t[len(t)-1] == ']')) {
		return false
	}
	return json.Valid([]byte(t))
}

// jsonStyles — fg-only token styles; an optional Background is layered by the
// caller (e.g. on a code surface) since renderJSON is used both in the gutter
// lane (no bg) and in fenced blocks (Surface bg).
type jsonStyles struct {
	key, str, num, lit, punct lipgloss.Style
}

func jsonStylesOn(bg lipgloss.TerminalColor) jsonStyles {
	base := func(c lipgloss.TerminalColor) lipgloss.Style {
		st := lipgloss.NewStyle().Foreground(c)
		if bg != nil {
			st = st.Background(bg)
		}
		return st
	}
	return jsonStyles{
		key:   base(theme.Title),  // object keys — the structure's spine
		str:   base(theme.Ok),     // string values
		num:   base(theme.Tool),   // numbers
		lit:   base(theme.Accent), // true/false/null
		punct: base(theme.Faint),  // { } [ ] : , — quiet
	}
}

// renderJSON pretty-prints s (assumed JSON; guard with looksLikeJSON) at the
// given indent and returns it tinted line-by-line. bg is the background to pair
// tints with (nil for the gutter lane). Falls back to the raw string if
// indenting fails.
func renderJSON(s string, bg lipgloss.TerminalColor) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(strings.TrimSpace(s)), "", "  "); err != nil {
		return s
	}
	st := jsonStylesOn(bg)
	lines := strings.Split(buf.String(), "\n")
	for i, ln := range lines {
		lines[i] = tintJSONLine(ln, st)
	}
	return strings.Join(lines, "\n")
}

// tintJSONLine tints one already-indented JSON line. It scans tokens: strings
// (a "key" when followed by a colon), numbers, literals, and punctuation.
func tintJSONLine(ln string, st jsonStyles) string {
	// Preserve leading indentation verbatim.
	i := 0
	for i < len(ln) && ln[i] == ' ' {
		i++
	}
	var b strings.Builder
	b.WriteString(ln[:i])
	for i < len(ln) {
		c := ln[i]
		switch {
		case c == '"':
			// String token: find the unescaped closing quote.
			j := i + 1
			for j < len(ln) {
				if ln[j] == '\\' {
					j += 2
					continue
				}
				if ln[j] == '"' {
					break
				}
				j++
			}
			if j < len(ln) {
				j++ // include closing quote
			}
			tok := ln[i:j]
			// A string immediately followed by ':' is a key.
			k := j
			for k < len(ln) && ln[k] == ' ' {
				k++
			}
			if k < len(ln) && ln[k] == ':' {
				b.WriteString(st.key.Render(tok))
			} else {
				b.WriteString(st.str.Render(tok))
			}
			i = j
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ':' || c == ',':
			b.WriteString(st.punct.Render(string(c)))
			i++
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < len(ln) && (ln[j] == '-' || ln[j] == '+' || ln[j] == '.' || ln[j] == 'e' || ln[j] == 'E' || (ln[j] >= '0' && ln[j] <= '9')) {
				j++
			}
			b.WriteString(st.num.Render(ln[i:j]))
			i = j
		case c == 't' || c == 'f' || c == 'n': // true / false / null
			j := i
			for j < len(ln) && ln[j] >= 'a' && ln[j] <= 'z' {
				j++
			}
			b.WriteString(st.lit.Render(ln[i:j]))
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
