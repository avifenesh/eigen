package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
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

// statusBarParts assembles the status segments.
func (m *model) statusBarParts() []string {
	parts := []string{"eigen"}
	if m.a != nil && m.a.Provider != nil {
		parts = append(parts, modelShort(m.a.Provider.Name()))
	}
	if m.a != nil {
		parts = append(parts, "perm="+string(m.a.Perm))
		if es, ok := m.a.Provider.(llm.EffortSetter); ok {
			parts = append(parts, "effort="+es.Effort())
		}
		if sr, ok := m.a.Provider.(llm.Searcher); ok && sr.SearchMode() != "off" {
			parts = append(parts, "search="+sr.SearchMode())
		}
	}
	if ind := m.ctxIndicator(); ind != "" {
		parts = append(parts, ind)
	}
	if m.readAloud {
		parts = append(parts, "read-aloud")
	}
	return parts
}

// statusBarLines packs the status parts into 1–2 width-respecting rows, the
// last padded with a faint accent rule. Width is measured in display columns
// (ansi.StringWidth), not bytes, so multi-byte glyphs never overflow.
func (m *model) statusBarLines() []string {
	parts := m.statusBarParts()
	w := m.width
	if w <= 0 {
		return []string{dim(strings.Join(parts, " · "))}
	}
	const sep = " · "
	var rows []string
	cur := ""
	for _, p := range parts {
		cand := p
		if cur != "" {
			cand = cur + sep + p
		}
		if ansi.StringWidth(cand) > w && cur != "" && len(rows) < 1 {
			// Overflow on the first row: wrap to a second row.
			rows = append(rows, cur)
			cur = p
			continue
		}
		cur = cand
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	// Style each row; pad the final row with an accent rule to the width.
	for i, r := range rows {
		rw := ansi.StringWidth(r)
		if rw > w {
			r = ansi.Truncate(r, w, "")
			rw = w
		}
		if i == len(rows)-1 {
			if pad := w - rw - 1; pad > 0 {
				rows[i] = dim(r) + " " + styleAccent.Render(strings.Repeat("─", pad))
				continue
			}
		}
		rows[i] = dim(r)
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
