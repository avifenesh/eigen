package tui

// Top-level View rendering: status line, pickers, and the agent-event →
// block translation.

import (
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

func (m *model) renderEvent(e agent.Event) {
	switch e.Kind {
	case agent.EventTextDelta:
		m.streamedText = true
		m.turnOutChars += len(e.Text)
		// Real output started: collapse the live thinking block(s) for this turn.
		m.collapseThinking()
		if b := m.lastOpen(blockText); b != nil && b.role == "assistant" {
			b.body += e.Text
			m.sync()
		} else {
			m.text("assistant", e.Text)
		}
	case agent.EventReasoningDelta:
		m.turnOutChars += len(e.Text)
		// Stream reasoning into a live "thinking" block, shown expanded so the
		// user sees the thoughts as they arrive; it is collapsed once the turn
		// produces text or a tool call (collapseThinking).
		if b := m.lastOpen(blockThinking); b != nil {
			b.body += e.Text
			m.sync()
		} else {
			m.push(&block{kind: blockThinking, title: "thinking", collapsed: false, body: sb(e.Text)})
		}
	case agent.EventToolStart:
		// Real action started: collapse the live thinking block(s) for this turn.
		m.collapseThinking()
		// A new step follows; whatever text streamed so far was in-between
		// commentary, not the final answer. Reset so EventDone renders the
		// final text unless deltas arrive AFTER the last tool call.
		m.streamedText = false
		// The todo tool drives the pinned plan panel instead of a tool block.
		if e.ToolName == "todo" {
			m.updateTodos(e.ToolArgs)
			m.status = "updated plan"
			return
		}
		m.status = "running " + e.ToolName
		m.push(&block{
			kind:      blockTool,
			toolName:  e.ToolName,
			toolArgs:  e.ToolArgs,
			title:     e.ToolName + " " + compact(string(e.ToolArgs)),
			collapsed: true,
			state:     toolRunning,
		})
	case agent.EventToolResult:
		if e.ToolName == "todo" {
			return // already reflected in the plan panel
		}
		// attach result to the matching open tool block (most recent)
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool && m.blocks[i].result == "" && m.blocks[i].state == toolRunning {
				m.blocks[i].result = e.Result
				m.blocks[i].isErr = e.IsError
				if e.IsError {
					m.blocks[i].state = toolFailed
				} else {
					m.blocks[i].state = toolDone
				}
				break
			}
		}
		m.sync()
	case agent.EventDone:
		m.status = "done"
		m.collapseThinking()
		// Show the final answer when the provider didn't stream any text this
		// turn (non-streaming, or a reasoning-only stream) — otherwise the
		// streamed assistant block already holds it.
		if !m.streamedText && strings.TrimSpace(e.Text) != "" {
			// Count the final answer into the turn's output stats: a
			// non-streaming turn with no tool steps emits no deltas, so this
			// is the only place its tokens are seen (tok/s would vanish).
			m.turnOutChars += len(e.Text)
			m.text("assistant", e.Text)
		}
		// Conversation mode: speak the answer, then listen for the next turn.
		if m.voiceOn {
			m.speakAnswer(strings.TrimSpace(e.Text))
		}
	case agent.EventNote:
		// Out-of-band notice from the loop (e.g. compaction circuit breaker).
		m.note(e.Text)
	}
}

// collapseThinking collapses the most recent still-expanded "thinking" block —
// called when real output (text/tool/done) follows streamed reasoning, so the
// thoughts are shown live then tucked away into a one-line, expandable header.
func (m *model) collapseThinking() {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := m.blocks[i]
		if b.kind == blockThinking {
			if !b.collapsed {
				b.collapsed = true
				m.sync()
			}
			return
		}
		// Stop at the previous turn's assistant text (don't collapse older turns).
		if b.kind == blockText && b.role == "assistant" {
			return
		}
	}
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	if m.picking {
		return m.pickerView()
	}
	if m.modelPicking {
		return m.modelPickerView()
	}
	var bottom string
	switch {
	case m.pending != nil:
		bottom = styleAsk.Render("[y]es approve · [n]o deny · [a]lways allow this tool")
	case m.state == stRunning:
		// Status/spinner on its own line, with the input below so the user can
		// type a message to queue (enter) or interrupt (esc) while it runs.
		hint := dim("   enter queue · esc interrupt · alt+↑/↓ select · tab expand")
		bottom = m.sp.View() + " " + m.status + dim(m.liveTokRate()) + m.queuedHint() + hint + "\n" + m.ti.View()
	default:
		bottom = m.compMenuView() + m.ti.View()
	}
	return m.planView() + m.vp.View() + "\n" + bottom + "\n" + m.statusBarView()
}

// queuedHint summarizes how many messages are waiting to be sent.
func (m *model) queuedHint() string {
	if len(m.queued) == 0 {
		return ""
	}
	return styleAsk.Render(fmt.Sprintf("  [%d queued]", len(m.queued)))
}

// pickerView renders the session chooser.
func (m *model) pickerView() string {
	var b strings.Builder
	b.WriteString(styleUser.Render("resume a session") + dim("   ↑↓ move · enter open · esc cancel") + "\n\n")
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	// window around the selection
	start := 0
	if m.pickIdx >= rows {
		start = m.pickIdx - rows + 1
	}
	end := start + rows
	if end > len(m.picks) {
		end = len(m.picks)
	}
	for i := start; i < end; i++ {
		p := m.picks[i]
		title := p.Title
		if title == "" {
			title = dim("(untitled)")
		}
		when := time.Unix(0, p.Updated).Format("01-02 15:04")
		line := fmt.Sprintf("%s  %-7s  %s", when, p.Source, title)
		if i == m.pickIdx {
			line = styleAsk.Render("› " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// modelPickerView renders the interactive model chooser (bare /model).
func (m *model) modelPickerView() string {
	var b strings.Builder
	b.WriteString(styleUser.Render("choose a model") + dim("   ↑↓ move · enter switch · esc cancel") + "\n\n")
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	start := 0
	if m.modelPickIdx >= rows {
		start = m.modelPickIdx - rows + 1
	}
	end := start + rows
	if end > len(m.modelPicks) {
		end = len(m.modelPicks)
	}
	for i := start; i < end; i++ {
		mi := m.modelPicks[i]
		// Window: prefer 1M when available.
		win := mi.ContextWindow
		if mi.Context1M && mi.ContextWindow1M > 0 {
			win = mi.ContextWindow1M
		}
		winStr := ""
		if win > 0 {
			winStr = fmt.Sprintf("%dk", win/1000)
		}
		// Capability tags.
		var tags []string
		if mi.Cache {
			tags = append(tags, "cache")
		}
		if mi.Context1M {
			tags = append(tags, "1M")
		}
		if mi.Reasoning {
			if mi.Effort != "" {
				tags = append(tags, "effort:"+mi.Effort)
			} else if mi.ThinkingBudget > 0 {
				tags = append(tags, "thinking")
			}
		}
		if mi.Search {
			tags = append(tags, "search")
		}
		tagStr := ""
		if len(tags) > 0 {
			tagStr = "  [" + strings.Join(tags, " ") + "]"
		}
		// One render path: selection marker first, then active marker, then the
		// row — styled once at the end.
		active := mi.ID == m.modelID
		raw := fmt.Sprintf("%-34s %-9s %-5s%s", mi.ID, mi.Provider, winStr, tagStr)
		var line string
		switch {
		case i == m.modelPickIdx && active:
			line = styleAsk.Render("›● " + raw)
		case i == m.modelPickIdx:
			line = styleAsk.Render("›  " + raw)
		case active:
			line = styleStatus.Render(" ● " + raw)
		default:
			line = "   " + raw
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func dim(s string) string { return styleReason.Render(s) }
