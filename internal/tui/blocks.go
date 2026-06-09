package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// blockKind distinguishes the renderable units of the transcript.
type blockKind int

const (
	blockText     blockKind = iota // user or assistant prose (always expanded)
	blockThinking                  // model reasoning (collapsible, default collapsed)
	blockTool                      // a tool call + its result (collapsible)
	blockNote                      // status/system line (always one line)
)

// toolState tracks a tool block's lifecycle for the live status glyph.
type toolState int

const (
	toolRunning toolState = iota // call started, no result yet
	toolDone                     // finished successfully
	toolFailed                   // finished with an error
)

// block is one selectable, optionally collapsible unit of the transcript.
type block struct {
	kind      blockKind
	role      string // "user" | "assistant" for text blocks
	title     string // fallback header text (resumed history / non-tool blocks)
	body      string
	collapsed bool
	isErr     bool   // tool/result errored
	result    string // tool result (shown when expanded)

	// Tool block metadata, used for rich per-tool headers and live status.
	toolName string
	toolArgs json.RawMessage
	state    toolState

	// render cache: the wrapped output and the signature it was produced from.
	// Valid because block content is append-only (body grows; result/title/args
	// are set once), so a length+flags signature uniquely identifies the render.
	rcache string
	rkey   string
}

func (b *block) collapsible() bool { return b.kind == blockThinking || b.kind == blockTool }

// renderWrapped returns the block's display text, width-wrapped, using a cache
// keyed by the fields that affect rendering. Since block content is append-only,
// a signature of (flags + content lengths + selected + width) is sufficient to
// detect a change — so repeated syncs during streaming re-render only the block
// whose body grew, not every block.
func (b *block) renderWrapped(selected bool, width int) string {
	key := fmt.Sprintf("%d|%s|%t|%t|%d|%t|%d|%d|%d|%d|%d",
		b.kind, b.role, b.collapsed, b.isErr, b.state, selected, width,
		len(b.body), len(b.result), len(b.title), len(b.toolArgs))
	if key == b.rkey && b.rcache != "" {
		return b.rcache
	}
	s := b.render(selected)
	if width > 0 {
		s = lipgloss.NewStyle().Width(width).Render(s)
	}
	b.rkey = key
	b.rcache = s
	return s
}

// statusGlyph returns the live-status marker for a tool block.
func (b *block) statusGlyph() string {
	switch b.state {
	case toolFailed:
		return styleErr.Render("✗")
	case toolDone:
		return styleStatus.Render("✓")
	default:
		return styleAsk.Render("•")
	}
}

// header returns the one-line header text for a collapsible block (without the
// ▸/▾ marker). Tool blocks get a status glyph + a tailored, per-tool summary.
func (b *block) header() string {
	if b.kind != blockTool {
		return b.title
	}
	summary := toolSummary(b.toolName, b.toolArgs)
	if summary == "" {
		summary = b.title
	}
	return b.statusGlyph() + " " + summary
}

// toolSummary renders a compact, human-readable description of a tool call from
// its name and raw arguments — e.g. `read src/main.go` instead of the raw JSON.
func toolSummary(name string, args json.RawMessage) string {
	if name == "" {
		return ""
	}
	var a struct {
		Path           string `json:"path"`
		Pattern        string `json:"pattern"`
		Command        string `json:"command"`
		URL            string `json:"url"`
		OldString      string `json:"old_string"`
		NewString      string `json:"new_string"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	_ = json.Unmarshal(args, &a)
	label := name
	switch name {
	case "read", "list", "write":
		return label + " " + a.Path
	case "edit":
		return label + " " + a.Path
	case "grep":
		if a.Path != "" {
			return label + " " + compact(a.Pattern) + " in " + a.Path
		}
		return label + " " + compact(a.Pattern)
	case "glob":
		if a.Path != "" {
			return label + " " + a.Pattern + " in " + a.Path
		}
		return label + " " + a.Pattern
	case "bash":
		return label + " " + compact(a.Command)
	case "fetch":
		return label + " " + a.URL
	default:
		return label + " " + compact(string(args))
	}
}

// maxDiffLines bounds how many diff lines an edit block renders.
const maxDiffLines = 200

// toolDetail returns tool-specific expanded content (plain text). For edit /
// multiedit it returns a +/- diff derived from the arguments; otherwise "" so
// the generic result text is used.
func (b *block) toolDetail() string {
	if b.kind != blockTool {
		return ""
	}
	switch b.toolName {
	case "edit":
		var a struct {
			Old string `json:"old_string"`
			New string `json:"new_string"`
		}
		if json.Unmarshal(b.toolArgs, &a) != nil || (a.Old == "" && a.New == "") {
			return ""
		}
		return diffText(a.Old, a.New)
	case "multiedit":
		var a struct {
			Edits []struct {
				Old string `json:"old_string"`
				New string `json:"new_string"`
			} `json:"edits"`
		}
		if json.Unmarshal(b.toolArgs, &a) != nil || len(a.Edits) == 0 {
			return ""
		}
		var parts []string
		for _, e := range a.Edits {
			parts = append(parts, diffText(e.Old, e.New))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// diffText renders old→new as plain +/- prefixed lines (no ANSI, so it is safe
// to truncate for a collapsed preview). Unchanged lines are kept as context via
// a line-level LCS. Color is applied later by colorizeDiff.
func diffText(old, new string) string {
	o := strings.Split(old, "\n")
	n := strings.Split(new, "\n")
	ops := lcsDiff(o, n)
	var b strings.Builder
	count := 0
	for _, op := range ops {
		if count++; count > maxDiffLines {
			break
		}
		b.WriteString(op + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
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

// colorizeDiff applies per-line color to +/- prefixed diff text.
func colorizeDiff(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "- "):
			lines[i] = styleErr.Render(ln)
		case strings.HasPrefix(ln, "+ "):
			lines[i] = styleStatus.Render(ln)
		}
	}
	return strings.Join(lines, "\n")
}

// render returns the block's display text given whether it is the selected
// block. Collapsible blocks show a header with a ▸/▾ marker; when collapsed
// they also show a single preview line.
func (b *block) render(selected bool) string {
	var s strings.Builder
	switch b.kind {
	case blockText:
		if b.role == "user" {
			s.WriteString(styleUser.Render("» " + b.body))
		} else {
			s.WriteString(renderProse(b.body))
		}

	case blockNote:
		s.WriteString(styleStatus.Render(b.body))

	case blockThinking, blockTool:
		marker := "▾"
		if b.collapsed {
			marker = "▸"
		}
		style := styleTool
		if b.kind == blockThinking {
			style = styleReason
		}
		if b.isErr {
			style = styleErr
		}
		header := marker + " " + b.header()
		if selected {
			s.WriteString(lipgloss.NewStyle().Bold(true).Render(style.Render("❭ " + header)))
		} else {
			s.WriteString("  " + style.Render(header))
		}

		full := b.body
		detail := b.toolDetail()
		isDiff := detail != ""
		switch {
		case isDiff:
			if full != "" {
				full += "\n"
			}
			full += detail
		case b.kind == blockTool && b.result != "":
			if full != "" {
				full += "\n"
			}
			full += b.result
		}
		if b.collapsed {
			if line := previewLine(full); line != "" {
				s.WriteString("  " + styleReason.Render(line))
			}
		} else if full != "" {
			if isDiff {
				s.WriteString("\n" + indent(colorizeDiff(full)))
			} else {
				s.WriteString("\n" + indent(style.Render(full)))
			}
		}
	}
	return s.String()
}

// renderProse renders assistant markdown for the terminal: fenced code blocks
// get a left bar + code color; headings, list items, blockquotes, and
// horizontal rules are styled per-line; inline **bold**, *italic*, and `code`
// are styled within normal text. It is deliberately lightweight (no full
// CommonMark) so it stays fast and dependency-free.
func renderProse(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)

		// Fenced code block delimiters toggle code mode.
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out = append(out, styleReason.Render(ln))
			continue
		}
		if inFence {
			out = append(out, styleCode.Render("│ "+ln))
			continue
		}

		// Horizontal rule.
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			out = append(out, dim(strings.Repeat("─", 24)))
			continue
		}

		// ATX heading: #, ##, … up to ######.
		if h := headingLevel(trimmed); h > 0 {
			text := strings.TrimSpace(trimmed[h:])
			prefix := strings.Repeat("#", h) + " "
			// Give headings breathing room: a blank line before, unless we're at
			// the very top or the previous line is already blank.
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) != "" {
				out = append(out, "")
			}
			out = append(out, styleHeading.Render(prefix+renderInline(text)))
			continue
		}

		// Blockquote.
		if strings.HasPrefix(trimmed, "> ") {
			out = append(out, styleQuote.Render("▏ "+renderInline(strings.TrimPrefix(trimmed, "> "))))
			continue
		}

		// Bullet list item: -, *, or + followed by a space (keep indentation).
		if item, indent, ok := bulletItem(ln); ok {
			out = append(out, indent+styleBullet.Render("• ")+renderInline(item))
			continue
		}

		// Ordered list item: "N. text" (keep the number).
		if num, item, indent, ok := orderedItem(ln); ok {
			out = append(out, indent+styleBullet.Render(num+". ")+renderInline(item))
			continue
		}

		// Plain paragraph line: apply inline styling.
		out = append(out, renderInline(ln))
	}
	return strings.Join(out, "\n")
}

// headingLevel returns the ATX heading level (1–6) of a trimmed line, or 0.
func headingLevel(t string) int {
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n >= 1 && n <= 6 && n < len(t) && t[n] == ' ' {
		return n
	}
	return 0
}

// bulletItem reports whether ln is a "- "/"* "/"+ " bullet, returning the item
// text and the leading indentation to preserve nesting.
func bulletItem(ln string) (text, indent string, ok bool) {
	indent = ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
	rest := ln[len(indent):]
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && rest[1] == ' ' {
		return rest[2:], indent, true
	}
	return "", "", false
}

// orderedItem reports whether ln is an "N. " ordered list item.
func orderedItem(ln string) (num, text, indent string, ok bool) {
	indent = ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
	rest := ln[len(indent):]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(rest) && rest[i] == '.' && rest[i+1] == ' ' {
		return rest[:i], rest[i+2:], indent, true
	}
	return "", "", "", false
}

// renderInline styles inline markdown spans within a single line: `code`,
// **bold**, and *italic*. Backtick spans win over emphasis so code is verbatim.
func renderInline(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString(styleInlineCode.Render(s[i+1 : i+1+end]))
				i = i + 1 + end + 1
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				b.WriteString(styleBold.Render(s[i+2 : i+2+end]))
				i = i + 2 + end + 2
				continue
			}
		case s[i] == '*':
			if end := strings.IndexByte(s[i+1:], '*'); end >= 0 && end > 0 {
				b.WriteString(styleItalic.Render(s[i+1 : i+1+end]))
				i = i + 1 + end + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func previewLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 70 {
		s = s[:70] + "…"
	}
	return s
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}
