package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// todoItem mirrors one entry of the agent's todo tool call.
type todoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// maxTodoRows caps how many plan rows the panel shows.
const maxTodoRows = 8

// updateTodos parses a todo tool call's arguments into the live plan panel.
func (m *model) updateTodos(args json.RawMessage) {
	var in struct {
		Todos []todoItem `json:"todos"`
	}
	if json.Unmarshal(args, &in) != nil {
		return
	}
	m.todos = in.Todos
	m.relayout()
}

// topHeight is the number of rows the top panels occupy: the plan panel (0 when
// empty). The status bar now lives at the bottom, so it is not counted here.
func (m *model) topHeight() int {
	h := 0
	if len(m.todos) > 0 {
		rows := len(m.todos)
		if rows > maxTodoRows {
			rows = maxTodoRows
		}
		h += 1 + rows // plan header + tasks
	}
	return h
}

// todoGlyphStyled returns a colored marker for a task status.
func todoGlyphStyled(status string) string {
	switch status {
	case "completed":
		return styleStatus.Render("✓")
	case "in_progress":
		return styleAsk.Render("▸")
	case "cancelled":
		return styleReason.Render("✗")
	default:
		return dim("○")
	}
}

// statusBarView renders the persistent status line: model · perm · context
// usage · read-aloud. Shown at the bottom, below the input. It wraps onto a
// second line when the parts don't fit the terminal width (so nothing runs off
// the edge); statusBarHeight() reports how many rows it uses.
func (m *model) statusBarView() string {
	return strings.Join(m.statusBarLines(), "\n")
}

// statusBarHeight is the number of rows the status bar occupies (1 or 2).
func (m *model) statusBarHeight() int { return len(m.statusBarLines()) }

// statusSeg is one status-bar segment: its plain text (for width math) and the
// style used to render it.
type statusSeg struct {
	text  string
	style lipgloss.Style
}

// statusBarParts assembles the colored status segments.
func (m *model) statusBarParts() []statusSeg {
	segs := []statusSeg{{"eigen", styleAccent.Bold(true)}}
	if m.a != nil && m.a.Provider != nil {
		segs = append(segs, statusSeg{modelShort(m.a.Provider.Name()), styleUser})
	}
	if m.a != nil {
		// perm: green when gated (safe), amber when auto (runs tools freely).
		permStyle := styleStatus
		if m.a.Perm == agent.PermAuto {
			permStyle = styleAsk
		}
		segs = append(segs, statusSeg{"perm=" + string(m.a.Perm), permStyle})
		if es, ok := m.a.Provider.(llm.EffortSetter); ok {
			segs = append(segs, statusSeg{"effort=" + es.Effort(), styleTool})
		}
		if sr, ok := m.a.Provider.(llm.Searcher); ok && sr.SearchMode() != "off" {
			segs = append(segs, statusSeg{"search=" + sr.SearchMode(), styleCode})
		}
	}
	if ind := m.ctxIndicator(); ind != "" {
		segs = append(segs, statusSeg{ind, m.ctxStyle()})
	}
	if m.readAloud {
		segs = append(segs, statusSeg{"read-aloud", styleStatus})
	}
	return segs
}

// ctxStyle colors the context indicator by how full the budget is: calm green
// under ~75%, amber approaching the limit, red when nearly full (a nudge to
// /compact before a 429).
func (m *model) ctxStyle() lipgloss.Style {
	if m.a == nil || m.a.MaxContextTokens <= 0 {
		return styleReason
	}
	frac := float64(m.ctxTokens) / float64(m.a.MaxContextTokens)
	switch {
	case frac >= 0.9:
		return styleErr
	case frac >= 0.75:
		return styleAsk
	default:
		return styleStatus
	}
}

// statusBarLines packs the colored status segments into 1–2 width-respecting
// rows, the last padded with a faint accent rule. Width is measured in display
// columns (plain text), not bytes; each segment keeps its own color and the
// separators are dim.
func (m *model) statusBarLines() []string {
	segs := m.statusBarParts()
	w := m.width
	sep := dim(" · ")
	plainSep := " · "
	if w <= 0 {
		var parts []string
		for _, s := range segs {
			parts = append(parts, s.style.Render(s.text))
		}
		return []string{strings.Join(parts, sep)}
	}
	// Pack into rows by plain display width, rendering styled segments.
	var rows []string      // styled
	var rowsPlainW []int   // plain width of each row
	cur := ""              // styled accumulator
	curW := 0              // plain width accumulator
	for _, s := range segs {
		addW := ansi.StringWidth(s.text)
		if cur != "" {
			addW += ansi.StringWidth(plainSep)
		}
		if curW+addW > w && cur != "" && len(rows) < 1 {
			rows = append(rows, cur)
			rowsPlainW = append(rowsPlainW, curW)
			cur, curW = "", 0
			addW = ansi.StringWidth(s.text)
		}
		if cur != "" {
			cur += sep
		}
		cur += s.style.Render(s.text)
		curW += addW
	}
	if cur != "" {
		rows = append(rows, cur)
		rowsPlainW = append(rowsPlainW, curW)
	}
	// Pad the final row with an accent rule to the width.
	last := len(rows) - 1
	if last >= 0 {
		if pad := w - rowsPlainW[last] - 1; pad > 0 {
			rows[last] += " " + styleAccent.Render(strings.Repeat("─", pad))
		}
	}
	return rows
}

// ctxIndicator is the approximate context usage vs the budget, e.g. "~12k/200k".
// It reads the cached token count (refreshCtx updates it at safe points) so the
// render never races the agent goroutine appending to the session.
func (m *model) ctxIndicator() string {
	used := m.ctxTokens
	if m.a != nil && m.a.MaxContextTokens > 0 {
		return fmt.Sprintf("~%s/%s", kfmt(used), kfmt(m.a.MaxContextTokens))
	}
	return "~" + kfmt(used)
}

// refreshCtx recomputes the cached context-token estimate. Call only on the main
// goroutine when no turn is running (the session slice is otherwise mutated by
// the agent goroutine).
func (m *model) refreshCtx() {
	if m.session == nil {
		m.ctxTokens = 0
		return
	}
	m.ctxTokens = llm.EstimateTokens(m.session.Messages())
}

// kfmt formats a token count compactly (e.g. 12345 -> "12k").
func kfmt(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// modelShort trims a provider Name() like "openai.gpt-5.5 (bedrock mantle)" to
// the model id before the parenthetical.
func modelShort(name string) string {
	if i := strings.Index(name, " ("); i >= 0 {
		return name[:i]
	}
	return name
}

// planView renders the pinned plan panel shown above the transcript. Returns
// exactly topHeight() newline-terminated lines (or "" when empty).
func (m *model) planView() string {
	if len(m.todos) == 0 {
		return ""
	}
	done := 0
	for _, t := range m.todos {
		if t.Status == "completed" {
			done++
		}
	}
	var b strings.Builder
	b.WriteString(styleAccent.Render("▸ ") + styleUser.Render(fmt.Sprintf("plan (%d/%d)", done, len(m.todos))) + "\n")
	rows := len(m.todos)
	if rows > maxTodoRows {
		rows = maxTodoRows
	}
	for i := 0; i < rows; i++ {
		t := m.todos[i]
		line := todoGlyphStyled(t.Status) + " " + t.Content
		if t.Status == "completed" {
			line = todoGlyphStyled(t.Status) + " " + dim(t.Content)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
