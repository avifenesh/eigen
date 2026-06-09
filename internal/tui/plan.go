package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
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

// statusBarView renders the persistent status line: model · perm · context usage
// · read-aloud. Shown at the bottom, below the input. Single line, no trailing
// newline (it is the last row).
func (m *model) statusBarView() string {
	parts := []string{"eigen"}
	if m.a != nil && m.a.Provider != nil {
		parts = append(parts, modelShort(m.a.Provider.Name()))
	}
	if m.a != nil {
		parts = append(parts, "perm="+string(m.a.Perm))
		// Surface the reasoning effort when the model exposes it.
		if es, ok := m.a.Provider.(llm.EffortSetter); ok {
			parts = append(parts, "effort="+es.Effort())
		}
		// Surface live-search mode when the model supports it (grok).
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
	line := strings.Join(parts, " · ")
	// Pad into a footer bar with a faint rule extending to the width.
	if m.width > 0 {
		if len(line) > m.width {
			line = line[:m.width]
		} else if pad := m.width - len(line) - 1; pad > 0 {
			line = line + " " + strings.Repeat("─", pad)
		}
	}
	return dim(line)
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
	b.WriteString(styleUser.Render(fmt.Sprintf("plan (%d/%d)", done, len(m.todos))) + "\n")
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
