package tui

import (
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// Markdown table rendering — models emit GitHub-flavored tables constantly, and
// raw "| a | b |" pipes look broken. We render them as aligned, bordered tables
// on the Surface tint so a table reads as a real document element.

// lineAt safely returns lines[i] or "".
func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// isTableSep reports whether a line is a GFM table separator row:
// optional leading/trailing pipes around cells of dashes (with optional :
// alignment markers), e.g. "|---|:--:|--:|" or "--- | ---".
func isTableSep(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || !strings.Contains(s, "-") {
		return false
	}
	s = strings.Trim(s, "|")
	cells := strings.Split(s, "|")
	if len(cells) < 1 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

// splitRow splits a markdown table row into trimmed cells (drops the optional
// outer pipes).
func splitRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// renderMarkdownTable renders a table starting at lines[0] (the header row,
// with lines[1] the separator). Returns the rendered lines and how many input
// lines it consumed. consumed==0 means it wasn't a table after all.
func renderMarkdownTable(lines []string, maxW int) ([]string, int) {
	if len(lines) < 2 || !isTableSep(lineAt(lines, 1)) {
		return nil, 0
	}
	header := splitRow(lines[0])
	cols := len(header)
	if cols == 0 {
		return nil, 0
	}
	// Gather body rows until a non-table line.
	var body [][]string
	consumed := 2 // header + separator
	for i := 2; i < len(lines); i++ {
		if !strings.Contains(lines[i], "|") || strings.TrimSpace(lines[i]) == "" {
			break
		}
		body = append(body, splitRow(lines[i]))
		consumed++
	}

	// Column widths = max plain cell width, clamped so the whole table fits maxW.
	w := make([]int, cols)
	fit := func(cells []string) {
		for c := 0; c < cols; c++ {
			cv := ""
			if c < len(cells) {
				cv = cells[c]
			}
			if n := ansi.StringWidth(cv); n > w[c] {
				w[c] = n
			}
		}
	}
	fit(header)
	for _, r := range body {
		fit(r)
	}
	// Clamp total width: borders take 3 cols per column + 1 (│ a │ b │).
	budget := maxW - (cols*3 + 1)
	if budget < cols {
		budget = cols
	}
	total := 0
	for _, x := range w {
		total += x
	}
	if total > budget {
		// Shrink proportionally, min 3 each.
		for c := range w {
			scaled := w[c] * budget / total
			if scaled < 3 {
				scaled = 3
			}
			w[c] = scaled
		}
	}

	bar := func(l, m, r string) string {
		var b strings.Builder
		b.WriteString(l)
		for c := 0; c < cols; c++ {
			b.WriteString(strings.Repeat("─", w[c]+2))
			if c < cols-1 {
				b.WriteString(m)
			}
		}
		b.WriteString(r)
		return theme.SFaint.Render(b.String())
	}
	cell := func(s string, width int, header bool) string {
		s = ansi.Truncate(s, width, "…")
		pad := width - ansi.StringWidth(s)
		if pad < 0 {
			pad = 0
		}
		if header {
			s = styleTableHead.Render(s)
		} else {
			s = styleText.Render(s)
		}
		return " " + s + strings.Repeat(" ", pad) + " "
	}
	rowLine := func(cells []string, head bool) string {
		var b strings.Builder
		sep := theme.SFaint.Render("│")
		b.WriteString(sep)
		for c := 0; c < cols; c++ {
			cv := ""
			if c < len(cells) {
				cv = cells[c]
			}
			b.WriteString(cell(cv, w[c], head))
			b.WriteString(sep)
		}
		// Paint the whole row on the Surface tint.
		return fillBG(b.String(), surfaceHex(theme.Surface), 0)
	}

	out := []string{
		fillBG(bar("╭", "┬", "╮"), surfaceHex(theme.Surface), 0),
		rowLine(header, true),
		fillBG(bar("├", "┼", "┤"), surfaceHex(theme.Surface), 0),
	}
	for _, r := range body {
		out = append(out, rowLine(r, false))
	}
	out = append(out, fillBG(bar("╰", "┴", "╯"), surfaceHex(theme.Surface), 0))
	return out, consumed
}
