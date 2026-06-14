package tui

// Diff rendering for edit/multiedit/apply_patch/write tool blocks: line-level
// LCS diffs with collapsed unchanged context, intra-line change highlighting,
// and +N −M stats. diffText/diffStats produce plain text (safe to cache,
// preview, and truncate); renderDiff applies ANSI styling at render time.

import (
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
)

// maxDiffLines bounds how many diff lines an edit block renders.
const maxDiffLines = 200

// diffContextLines is how many unchanged lines are kept on each side of a
// change; longer unchanged runs collapse to a "⋯ N unchanged ⋯" marker.
const diffContextLines = 2

// diffText renders old→new as plain +/- prefixed lines (no ANSI, so it is safe
// to truncate for a collapsed preview). Unchanged lines are kept as context via
// a line-level LCS; long unchanged runs collapse to a count marker. Color and
// intra-line highlights are applied later by renderDiff.
func diffText(old, new string) string {
	o := strings.Split(old, "\n")
	n := strings.Split(new, "\n")
	ops := collapseContext(lcsDiff(o, n))
	var b strings.Builder
	count := 0
	for _, op := range ops {
		if count++; count > maxDiffLines {
			b.WriteString(fmt.Sprintf("⋯ %d more lines ⋯\n", len(ops)-maxDiffLines))
			break
		}
		b.WriteString(op + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// collapseContext folds runs of unchanged context longer than
// 2*diffContextLines+1 into "⋯ N unchanged lines ⋯", keeping diffContextLines
// on each side so changes keep their bearings.
func collapseContext(ops []string) []string {
	isCtx := func(s string) bool { return strings.HasPrefix(s, "  ") }
	var out []string
	i := 0
	for i < len(ops) {
		if !isCtx(ops[i]) {
			out = append(out, ops[i])
			i++
			continue
		}
		j := i
		for j < len(ops) && isCtx(ops[j]) {
			j++
		}
		run := j - i
		// Leading context (no change before it) keeps only the tail; trailing
		// context keeps only the head; middle runs keep both ends.
		head, tail := diffContextLines, diffContextLines
		if i == 0 {
			head = 0
		}
		if j == len(ops) {
			tail = 0
		}
		if run <= head+tail+1 {
			out = append(out, ops[i:j]...)
		} else {
			out = append(out, ops[i:i+head]...)
			out = append(out, fmt.Sprintf("⋯ %d unchanged lines ⋯", run-head-tail))
			out = append(out, ops[j-tail:j]...)
		}
		i = j
	}
	return out
}

// lcsDiff returns unified-style lines ("  ctx" / "- removed" / "+ added") using
// a longest-common-subsequence over lines.
func lcsDiff(a, b []string) []string {
	// DP table of LCS lengths.
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			out = append(out, "  "+a[i])
			i, j = i+1, j+1
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	for ; i < m; i++ {
		out = append(out, "- "+a[i])
	}
	for ; j < n; j++ {
		out = append(out, "+ "+b[j])
	}
	return out
}

// diffStats counts added/removed lines in a plain +/- diff text.
func diffStats(detail string) (add, del int) {
	for _, ln := range strings.Split(detail, "\n") {
		switch {
		case strings.HasPrefix(ln, "+ "):
			add++
		case strings.HasPrefix(ln, "- "):
			del++
		}
	}
	return add, del
}

// statsSuffix formats diffStats for a block header, e.g. " (+3 −1)".
func statsSuffix(detail string) string {
	add, del := diffStats(detail)
	if add == 0 && del == 0 {
		return ""
	}
	return fmt.Sprintf(" (+%d −%d)", add, del)
}

// renderDiff applies per-line color to +/- prefixed diff text and, when a
// removed line is paired with an added line (a modification), underlines the
// changed span within each line (common prefix/suffix split). Context-collapse
// markers render dim.
func renderDiff(s string) string { return renderDiffLang(s, "") }

// renderDiffLang renders a +/- diff like a real diff viewer: every line's CODE
// is syntax-highlighted (when lang is known), and added/removed lines carry a
// faint green/red background tint + a colored ± marker so the change is
// unmistakable while the code stays readable. Context lines are highlighted on
// no tint. Falls back to plain marker coloring when no lexer matches.
func renderDiffLang(s, lang string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, len(lines))
	// code paints a line's content with syntax highlighting on the given bg
	// (nil = none); falls back to a plain style when no lexer.
	code := func(src string, bg lipgloss.TerminalColor, plain lipgloss.Style) string {
		if lang != "" {
			if h, ok := highlightCode(src, lang, bg); ok {
				return strings.TrimRight(h, "\n")
			}
		}
		return plain.Render(src)
	}
	addBg := theme.AddBg
	delBg := theme.DelBg
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "⋯"):
			out[i] = styleReason.Render(ln)
		case strings.HasPrefix(ln, "+ "):
			marker := lipgloss.NewStyle().Foreground(theme.Ok).Background(addBg).Render("+ ")
			out[i] = marker + code(ln[2:], addBg, lipgloss.NewStyle().Foreground(theme.Ok).Background(addBg))
		case strings.HasPrefix(ln, "- "):
			marker := lipgloss.NewStyle().Foreground(theme.Err).Background(delBg).Render("- ")
			out[i] = marker + code(ln[2:], delBg, lipgloss.NewStyle().Foreground(theme.Err).Background(delBg))
		default:
			body := ln
			if strings.HasPrefix(ln, "  ") {
				body = ln[2:]
			}
			out[i] = styleGhost.Render("  ") + code(body, nil, styleReason)
		}
	}
	return strings.Join(out, "\n")
}
