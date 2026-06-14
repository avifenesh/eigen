package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
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
	wrapW  int // width available at last renderWrapped (for code-block surfaces)
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
	if key == b.rkey && b.rcache != "" && b.state != toolRunning {
		return b.rcache
	}
	b.wrapW = width // available width, for surface-filled regions (code blocks)
	s := b.render(selected)
	// Expand tabs BEFORE width-wrapping: the wrap (and the side panels'
	// padding math) counts \t as one column, but the terminal expands it to
	// the next 8-col stop at render time — so a tab-bearing line drawn next
	// to a side panel drifts and scrambles the whole row.
	if strings.ContainsRune(s, '\t') {
		s = strings.ReplaceAll(s, "\t", "    ")
	}
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
		// Running: a synced braille spinner (orange, like the loader) so an
		// in-progress tool visibly WORKS in the transcript.
		return styleWorking.Render(railSpinnerFrames[animFrame%len(railSpinnerFrames)])
	}
}

// animFrame is the shared animation frame for transcript spinners, advanced by
// the in-turn spinner tick (m.brandTick). Package-level so block methods (which
// don't hold the model) can read it; only meaningful while a turn runs.
var animFrame int

// toolIcon returns a small glyph that signals the KIND of action at a glance —
// a pen for writes, a book for reads, a prompt for shell, a lens for search.
// Plain Unicode (renders in any modern terminal, no Nerd Font needed), calm
// rather than loud.
func toolIcon(name string) string { return theme.ToolIcon(name) }

// header returns the one-line header text for a collapsible block (without the
// ▸/▾ marker). Tool blocks get a status glyph + an action icon + a tailored,
// per-tool summary.
func (b *block) header() string {
	if b.kind != blockTool {
		return b.title
	}
	summary := toolSummary(b.toolName, b.toolArgs)
	if summary == "" {
		summary = b.title
	}
	// Edit-family tools get +N −M stats in the header so the change size reads
	// at a glance even when collapsed.
	switch b.toolName {
	case "edit", "multiedit", "apply_patch", "write":
		summary += statsSuffix(b.toolDetail())
	}
	icon := toolIcon(b.toolName)
	if icon != "" {
		return b.statusGlyph() + " " + icon + "  " + summary
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
		// Render like an actual shell line: the bash icon leads (the prompt),
		// so the command stands alone (no redundant "bash" word).
		return compact(a.Command)
	case "fetch":
		return label + " " + a.URL
	default:
		return label + " " + compact(string(args))
	}
}

// editPath returns the file path an edit/write/apply_patch block targets, for
// language-aware diff highlighting. "" when unknown.
func (b *block) editPath() string {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(b.toolArgs, &a)
	return a.Path
}

// codeResult renders a tool result that is SOURCE CODE (e.g. `read` of a code
// file) as a framed, syntax-tinted block on the Surface tint — the same
// document treatment as a fenced code block — instead of a flat blob. Returns
// "" when the result isn't code (so the caller falls back to plain text).
// renderCodeBlock renders source code as a framed block on the Surface tint: a
// lang chip on Overlay, then each code line filled on Surface. It uses real
// chroma syntax highlighting (mapped to our palette) when a lexer matches the
// language/content, falling back to the heuristic tintCodeLine otherwise.
func renderCodeBlock(code, lang string, codeW int) string {
	if codeW < 8 {
		codeW = 8
	}
	chipLang := lang
	if chipLang == "" {
		chipLang = "code"
	}
	out := []string{fillBG(styleSurfaceBrand.Render(" "+chipLang+" "), surfaceHex(theme.Overlay), codeW)}
	if hl, ok := highlightCode(code, lang, theme.Surface); ok {
		// chroma emits a token per newline, so styled lines split cleanly.
		for _, ln := range strings.Split(strings.TrimRight(hl, "\n"), "\n") {
			out = append(out, fillBG("  "+expandTabs(ln), surfaceHex(theme.Surface), codeW))
		}
	} else {
		for _, ln := range strings.Split(code, "\n") {
			out = append(out, fillBG("  "+tintCodeLine(expandTabs(ln), styleCodeOnSurface), surfaceHex(theme.Surface), codeW))
		}
	}
	out = append(out, fillBG("", surfaceHex(theme.Surface), codeW))
	return strings.Join(out, "\n")
}

func (b *block) codeResult(full string) string {
	if b.kind != blockTool || full == "" || b.toolName != "read" {
		return ""
	}
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(b.toolArgs, &a)
	if !isCodePath(a.Path) {
		return ""
	}
	codeW := b.wrapW // fill the transcript width edge-to-edge (no base-sliver gap)
	if codeW < 8 {
		codeW = 8
	}
	return renderCodeBlock(full, langForPath(a.Path), codeW)
}

// isCodePath reports whether a file path looks like source code worth syntax
// framing (by extension).
func isCodePath(path string) bool {
	path = strings.ToLower(path)
	dot := strings.LastIndexByte(path, '.')
	if dot < 0 {
		return false
	}
	switch path[dot+1:] {
	case "go", "py", "js", "ts", "jsx", "tsx", "rs", "c", "h", "cpp", "cc", "hpp",
		"java", "kt", "rb", "php", "swift", "scala", "sh", "bash", "zsh", "lua",
		"sql", "html", "css", "scss", "vue", "svelte", "json", "yaml", "yml",
		"toml", "md", "proto", "graphql", "tf", "zig", "ml", "ex", "exs", "clj":
		return true
	}
	return false
}

// langForPath maps a file extension to a short language label for the chip.
func langForPath(path string) string {
	dot := strings.LastIndexByte(path, '.')
	if dot < 0 {
		return "code"
	}
	switch ext := strings.ToLower(path[dot+1:]); ext {
	case "py":
		return "python"
	case "js", "jsx":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "rs":
		return "rust"
	case "rb":
		return "ruby"
	case "sh", "bash", "zsh":
		return "shell"
	case "yml":
		return "yaml"
	case "md":
		return "markdown"
	default:
		return ext
	}
}

// toolDetail returns tool-specific expanded content (plain text). For edit /
// multiedit it returns a +/- diff derived from the arguments; for apply_patch
// the patch text with collapsed context; for write an all-added preview.
// Otherwise "" so the generic result text is used.
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
		for i, e := range a.Edits {
			if len(a.Edits) > 1 {
				parts = append(parts, fmt.Sprintf("edit %d/%d:", i+1, len(a.Edits)))
			}
			parts = append(parts, diffText(e.Old, e.New))
		}
		return strings.Join(parts, "\n")
	case "apply_patch":
		var a struct {
			Patch string `json:"patch"`
		}
		if json.Unmarshal(b.toolArgs, &a) != nil || a.Patch == "" {
			return ""
		}
		return normalizePatch(a.Patch)
	case "write":
		var a struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(b.toolArgs, &a) != nil || a.Content == "" {
			return ""
		}
		// A write is all additions: render the new content as + lines so it
		// gets the same diff styling and stats as edits.
		lines := strings.Split(strings.TrimRight(a.Content, "\n"), "\n")
		out := make([]string, 0, len(lines)+1)
		for i, ln := range lines {
			if i >= maxDiffLines {
				out = append(out, fmt.Sprintf("⋯ %d more lines ⋯", len(lines)-maxDiffLines))
				break
			}
			out = append(out, "+ "+ln)
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// normalizePatch reformats unified-diff text to the block diff dialect:
// file headers and hunk markers render as dim context; +/- lines get the
// standard two-character prefixes so renderDiff styles them.
func normalizePatch(patch string) string {
	lines := strings.Split(strings.TrimRight(patch, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		if i >= maxDiffLines {
			out = append(out, fmt.Sprintf("⋯ %d more lines ⋯", len(lines)-maxDiffLines))
			break
		}
		switch {
		case strings.HasPrefix(ln, "--- ") || strings.HasPrefix(ln, "+++ ") ||
			strings.HasPrefix(ln, "@@") || strings.HasPrefix(ln, "diff "):
			out = append(out, "⋯ "+ln)
		case strings.HasPrefix(ln, "+"):
			out = append(out, "+ "+ln[1:])
		case strings.HasPrefix(ln, "-"):
			out = append(out, "- "+ln[1:])
		case strings.HasPrefix(ln, " "):
			out = append(out, " "+ln)
		default:
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

// render returns the block's display text given whether it is the selected
// block. Collapsible blocks show a header with a ▸/▾ marker; when collapsed
// they also show a single preview line.
func (b *block) render(selected bool) string {
	var s strings.Builder
	switch b.kind {
	case blockText:
		if b.role == "user" {
			// User turns: a clear cyan caret + the message, so "you said"
			// stands apart from assistant prose and tool activity.
			s.WriteString(styleUser.Render("❯ ") + styleUser.Render(b.body))
		} else {
			s.WriteString(renderProse(b.body, b.wrapW))
		}

	case blockNote:
		// Notes carry severity: errors read red with a ✗; ordinary status
		// notes are muted (green is reserved for success/done, so a neutral
		// note shouldn't masquerade as it).
		if b.isErr {
			s.WriteString(styleErr.Render("✗ " + b.body))
		} else {
			s.WriteString(styleReason.Render(b.body))
		}

	case blockThinking, blockTool:
		marker := theme.Expanded
		if b.collapsed {
			marker = theme.Collapsed
		}
		style := styleTool
		rule := styleTool.Render("▏")
		if b.kind == blockThinking {
			style = styleReason
			rule = styleReason.Render("▏")
		}
		if b.isErr {
			style = styleErr
			rule = styleErr.Render("▏")
		}
		// Header sits on the gutter rule: "▏ ▾ ✓ tool summary". The rule turns
		// tool/thinking activity into a distinct left lane, clearly set off
		// from conversational prose.
		header := rule + " " + style.Render(marker+" "+b.header())
		if selected {
			// Selected: a leading Focus bar (the ONE selection treatment,
			// shared with the rail) — structural so it reads even without
			// color — plus Focus-tinted, bold header.
			header = styleFocus.Render("▎") + styleFocus.Bold(true).Render(marker+" ") + style.Bold(true).Render(b.header())
		}
		s.WriteString(header)

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
				s.WriteString(dim(" · " + line))
			}
		} else if full != "" {
			if isDiff {
				s.WriteString("\n" + gutterRule(renderDiffLang(full, langForPath(b.editPath())), rule))
			} else if b.kind == blockTool && looksLikeJSON(full) {
				// JSON tool results read terribly raw — pretty-print + tint.
				s.WriteString("\n" + gutterRule(renderJSON(full, nil), rule))
			} else if code := b.codeResult(full); code != "" {
				// A tool that returned source code (e.g. read a .go file) gets
				// the document treatment: framed on the Surface tint, syntax-
				// tinted — same as a fenced code block, not a flat blob.
				s.WriteString("\n" + gutterRule(code, rule))
			} else {
				s.WriteString("\n" + gutterRule(style.Render(full), rule))
			}
		}
	}
	return s.String()
}

// gutterRule prefixes every line of s with the gutter rule + a space, so a
// tool/thinking block's body reads as a single contiguous lane under its
// header. The rule is a pre-styled "▏".
func gutterRule(s, rule string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = rule + " " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// renderProse renders assistant markdown for the terminal: fenced code blocks
// get a left bar + code color; headings, list items, blockquotes, and
// horizontal rules are styled per-line; inline **bold**, *italic*, and `code`
// are styled within normal text. It is deliberately lightweight (no full
// CommonMark) so it stays fast and dependency-free.
func renderProse(s string, width int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	// Code-block width: fill the FULL transcript content width so the framed
	// surface spans edge-to-edge and meets the panel/edge with no gap (a
	// narrower block leaves an exposed-base sliver — the "hole" in the design).
	codeW := width
	if codeW < 8 {
		codeW = 8
	}
	for li := 0; li < len(lines); li++ {
		ln := lines[li]
		trimmed := strings.TrimSpace(ln)

		// Markdown table: a header row "| a | b |" followed by a separator row
		// (|---|---|). Render as an aligned, bordered table on the Surface tint
		// — models emit tables constantly; raw pipes look broken.
		if strings.Contains(ln, "|") && isTableSep(lineAt(lines, li+1)) {
			tbl, consumed := renderMarkdownTable(lines[li:], codeW)
			if consumed > 0 {
				out = append(out, tbl...)
				li += consumed - 1
				continue
			}
		}

		// Fenced code block: collect the whole body to the closing ``` and
		// render it as one framed, syntax-highlighted block (chroma needs the
		// full block for accurate per-language highlighting).
		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			body := make([]string, 0, 8)
			closed := false
			j := li + 1
			for ; j < len(lines); j++ {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
					closed = true
					break
				}
				body = append(body, lines[j])
			}
			out = append(out, renderCodeBlock(strings.Join(body, "\n"), lang, codeW))
			if closed {
				li = j // consume through the closing fence
			} else {
				li = len(lines) // unterminated fence: consume the rest
			}
			continue
		}

		// Horizontal rule.
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			out = append(out, dim(strings.Repeat("─", 24)))
			continue
		}

		// ATX heading: #, ##, … up to ######. Render WITHOUT the raw '#'
		// markers — a level glyph + the heading style reads as a real heading.
		if h := headingLevel(trimmed); h > 0 {
			text := strings.TrimSpace(trimmed[h:])
			// Give headings breathing room: a blank line before, unless we're at
			// the very top or the previous line is already blank.
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) != "" {
				out = append(out, "")
			}
			rendered := styleHeading.Render(renderInline(text))
			out = append(out, rendered)
			// h1/h2 get an underline rule for weight, like a real document.
			if h <= 2 {
				rule := "═"
				if h == 2 {
					rule = "─"
				}
				out = append(out, styleHeading.Render(strings.Repeat(rule, min(lipgloss.Width(text), 48))))
			}
			continue
		}

		// Blockquote.
		if strings.HasPrefix(trimmed, "> ") {
			out = append(out, styleQuote.Render("▏ "+renderInline(strings.TrimPrefix(trimmed, "> "))))
			continue
		}

		// Bullet list item: -, *, or + followed by a space (keep indentation).
		if item, indent, ok := bulletItem(ln); ok {
			out = append(out, indent+styleBullet.Render("• ")+styleText.Render(renderInline(item)))
			continue
		}

		// Ordered list item: "N. text" (keep the number).
		if num, item, indent, ok := orderedItem(ln); ok {
			out = append(out, indent+styleBullet.Render(num+". ")+styleText.Render(renderInline(item)))
			continue
		}

		// Plain paragraph line: apply inline styling, on the crisp Text color
		// (not the terminal default, which reads muddy on the deep base).
		if strings.TrimSpace(ln) == "" {
			out = append(out, "")
		} else {
			out = append(out, styleText.Render(renderInline(ln)))
		}
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
		case s[i] == '[':
			// Markdown link [text](url): show the text underlined in the accent
			// color, drop the raw URL syntax (it's noise in a terminal).
			if close := strings.IndexByte(s[i+1:], ']'); close >= 0 {
				rest := s[i+1+close+1:]
				if strings.HasPrefix(rest, "(") {
					if paren := strings.IndexByte(rest, ')'); paren >= 0 {
						text := s[i+1 : i+1+close]
						b.WriteString(styleLink.Render(text))
						i = i + 1 + close + 1 + paren + 1
						continue
					}
				}
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
