package tui

import (
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

// block is one selectable, optionally collapsible unit of the transcript.
type block struct {
	kind      blockKind
	role      string // "user" | "assistant" for text blocks
	title     string // tool name / "thinking" — shown on the header line
	body      strings.Builder
	collapsed bool
	isErr     bool   // tool/result errored
	result    string // tool result (shown when expanded)
}

func (b *block) collapsible() bool { return b.kind == blockThinking || b.kind == blockTool }

// render returns the block's display text given whether it is the selected
// block. Collapsible blocks show a header with a ▸/▾ marker; when collapsed
// they also show a single preview line.
func (b *block) render(selected bool) string {
	var s strings.Builder
	switch b.kind {
	case blockText:
		txt := b.body.String()
		if b.role == "user" {
			s.WriteString(styleUser.Render("» " + txt))
		} else {
			s.WriteString(txt)
		}

	case blockNote:
		s.WriteString(styleStatus.Render(b.body.String()))

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
		header := marker + " " + b.title
		if selected {
			s.WriteString(lipgloss.NewStyle().Bold(true).Render(style.Render("❭ " + header)))
		} else {
			s.WriteString("  " + style.Render(header))
		}

		full := b.body.String()
		if b.kind == blockTool && b.result != "" {
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
			s.WriteString("\n" + indent(style.Render(full)))
		}
	}
	return s.String()
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
