package tui

// Diff rendering for edit/multiedit/apply_patch/write tool blocks: line-level
// LCS diffs with collapsed unchanged context, intra-line change highlighting,
// and +N −M stats. diffText/diffStats produce plain text (safe to cache,
// preview, and truncate); renderDiff applies ANSI styling at render time.

import (
	"fmt"
	"strings"

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

// Intra-line highlight styles: the changed span within a modified line gets
// underlined on top of the line's add/remove color, so paired edits read at a
// glance without inverting whole blocks.
var (
	styleDelSpan = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Underline(true)
	styleAddSpan = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Underline(true)
)

// renderDiff applies per-line color to +/- prefixed diff text and, when a
// removed line is paired with an added line (a modification), underlines the
// changed span within each line (common prefix/suffix split). Context-collapse
// markers render dim.
func renderDiff(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, len(lines))
	i := 0
	for i < len(lines) {
		ln := lines[i]
		switch {
		case strings.HasPrefix(ln, "⋯"):
			out[i] = styleReason.Render(ln)
			i++
		case strings.HasPrefix(ln, "- "):
			// Gather the -/+ run and pair them in order: k removed lines
			// followed by l added lines pairs min(k,l) modifications.
			j := i
			for j < len(lines) && strings.HasPrefix(lines[j], "- ") {
				j++
			}
			k := j
			for k < len(lines) && strings.HasPrefix(lines[k], "+ ") {
				k++
			}
			dels := lines[i:j]
			adds := lines[j:k]
			for x := range dels {
				if x < len(adds) {
					d, a := highlightPair(dels[x][2:], adds[x][2:])
					out[i+x] = styleErr.Render("- ") + d
					out[j+x] = styleStatus.Render("+ ") + a
				} else {
					out[i+x] = styleErr.Render(dels[x])
				}
			}
			for x := len(dels); x < len(adds); x++ {
				out[j+x] = styleStatus.Render(adds[x])
			}
			i = k
		case strings.HasPrefix(ln, "+ "):
			out[i] = styleStatus.Render(ln)
			i++
		default:
			out[i] = ln
			i++
		}
	}
	return strings.Join(out, "\n")
}

// highlightPair renders a removed/added line pair, underlining the changed
// middle span found by common prefix/suffix. When the lines are too dissimilar
// (changed span dominates), it falls back to whole-line coloring — underlining
// nearly everything is noise.
func highlightPair(del, add string) (string, string) {
	pre, dMid, aMid, suf := splitCommon(del, add)
	// Similarity gate: at least a third of the longer line must be common.
	common := len(pre) + len(suf)
	longest := max(len(del), len(add))
	if longest == 0 || common*3 < longest {
		return styleErr.Render(del), styleStatus.Render(add)
	}
	d := styleErr.Render(pre) + styleDelSpan.Render(dMid) + styleErr.Render(suf)
	a := styleStatus.Render(pre) + styleAddSpan.Render(aMid) + styleStatus.Render(suf)
	return d, a
}

// splitCommon splits two strings into common prefix, differing middles, and
// common suffix (byte-wise, which is fine for code).
func splitCommon(a, b string) (pre, aMid, bMid, suf string) {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return a[:p], a[p : len(a)-s], b[p : len(b)-s], a[len(a)-s:]
}
