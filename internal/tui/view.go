package tui

// Top-level View rendering: status line, pickers, and the agent-event →
// block translation.

import (
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *model) renderEvent(e agent.Event) {
	switch e.Kind {
	case agent.EventTextDelta:
		m.streamedText = true
		m.turnOutChars += len(e.Text)
		// Streamed speech: complete sentences start speaking NOW, not at
		// turn end (voice mode / read-aloud).
		m.speechFeed(e.Text)
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
		// Provider-reported usage (summed over the turn): real numbers beat
		// the chars/4 estimate when available.
		if e.InTokens > 0 || e.OutTokens > 0 {
			m.turnInToks, m.turnOutToks = e.InTokens, e.OutTokens
		}
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
	if m.switching {
		return m.switcherView()
	}
	if m.tray {
		return m.trayView()
	}
	if m.modelPicking {
		return m.modelPickerView()
	}
	if m.conf.active {
		return m.configPanelView()
	}
	if m.pal.active {
		return m.paletteView()
	}
	var bottom string
	switch {
	case m.pending != nil:
		bottom = styleAsk.Render("[y]es approve · [n]o deny · [a]lways allow this tool")
	case m.state == stRunning:
		// Status/spinner on its own line, with the input below so the user can
		// type a message to queue (enter) or interrupt (esc) while it runs.
		// Truncated to the width — a too-long line wraps and breaks the layout.
		hint := dim("   enter steer · esc interrupt · alt+↑/↓ select · tab expand")
		// On-brand loader: a breathing λ + caret + synced dot, then the status
		// in the working color — unmistakably "eigen is working", no jitter.
		run := loaderView(m.brandTick) + " " + styleWorking.Render(m.status) + dim(m.liveTokRate()) + m.queuedHint() + hint
		if m.width > 0 {
			run = ansi.Truncate(run, m.width, "")
		}
		bottom = run + "\n" + m.ti.View()
	default:
		bottom = m.compMenuView() + m.ti.View()
	}
	if m.pending == nil && m.composerBarVisible() {
		bottom += "\n" + m.composerBarView()
	}
	if m.ov.active {
		bottom = m.overlayView() + "\n" + bottom
	}
	if m.flash != "" {
		bottom += "\n" + m.flashBanner()
	}
	if m.sidebarVisible() {
		// Headerless sidebar mode — THE design: no header, no top plan panel,
		// no bottom status bar. The sidebar owns all three; the band starts
		// at the top and the input sits clean at the bottom. paintBase ensures
		// every cell (incl. the input row) sits on Base, so a terminal with a
		// non-black background can't show through as an "exposed" grey hole.
		return paintBase(m.transcriptBand()+"\n"+bottom, m.width, m.height)
	}
	return paintBase(m.headerView()+"\n"+m.planView()+m.transcriptBand()+"\n"+bottom+"\n"+m.statusBarView(), m.width, m.height)
}

// flashBanner renders the transient confirmation pill, right-aligned: a calm
// filled accent badge — "✓ copied 250 chars" — that auto-clears after a beat.
func (m *model) flashBanner() string {
	bg, glyph := theme.Ok, "✓ "
	switch m.flashTone {
	case flashWarn:
		bg, glyph = theme.Warn, "• "
	case flashBad:
		bg, glyph = theme.Err, "✗ "
	}
	pill := lipgloss.NewStyle().
		Foreground(theme.OnBright).
		Background(bg).
		Bold(true).
		Padding(0, 1).
		Render(glyph + m.flash)
	if m.width <= 0 {
		return pill
	}
	pad := m.width - lipgloss.Width(pill)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + pill
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
		line := selectLine(i == m.pickIdx, fmt.Sprintf("%s  %-7s  %s", when, p.Source, title))
		b.WriteString(line + "\n")
	}
	return b.String()
}

// switcherView renders the in-window session switcher (alt+s): the daemon's
// sessions with live status glyphs; enter hops the window, h goes home to
// the app, the running session keeps running either way.
func (m *model) switcherView() string {
	var b strings.Builder
	hint := "   type to search · ↑↓ move · enter switch · ctrl+h home · esc cancel"
	b.WriteString(styleUser.Render("switch session") + dim(hint) + "\n")
	entries := m.switchFiltered()
	q := "search: " + m.switchQuery + "▌"
	if m.switchQuery == "" {
		q = dim("(type to filter)")
	}
	b.WriteString(styleAccent.Render(q) + dim(fmt.Sprintf("   %d/%d", len(entries), len(m.switchEntries))) + "\n\n")
	rows := m.height - 5
	if rows < 1 {
		rows = 1
	}
	if m.switchIdx >= len(entries) {
		m.switchIdx = max(0, len(entries)-1)
	}
	start := 0
	if m.switchIdx >= rows {
		start = m.switchIdx - rows + 1
	}
	end := start + rows
	if end > len(entries) {
		end = len(entries)
	}
	cur := ""
	if sl, ok := m.backend.(chat.SessionLister); ok {
		cur = sl.SessionID()
	}
	if len(entries) == 0 {
		b.WriteString(dim("  no matches\n"))
	}
	for i := start; i < end; i++ {
		e := entries[i]
		title := e.Title
		if title == "" {
			title = dim("(untitled)")
		}
		mark := " "
		if e.ID == cur {
			mark = styleFocus.Render("·") // you are here (active session — non-brand)
		}
		line := fmt.Sprintf("%s %s %-4s %s  %s", statusGlyph(e.Status), mark, e.ID, title, dim(e.Dir))
		line = selectLine(i == m.switchIdx, line)
		b.WriteString(line + "\n")
	}
	return b.String()
}

// statusGlyph maps a daemon session status to the rail glyph language:
// ● working · ○ idle · ◆ waiting on approval · ✗ error.
func statusGlyph(s string) string {
	switch s {
	case "working":
		return styleWorking.Render(theme.StatusWorking) // loud orange, matches the loader
	case "approval":
		return styleAsk.Render(theme.StatusApproval)
	case "error":
		return styleErr.Render(theme.StatusError)
	}
	return dim(theme.StatusIdle)
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
		// Selection uses the unified ▎ bar (Sel); the active model is marked
		// with a working-colored ● inside the row, so both signals coexist
		// without a bespoke prefix.
		active := mi.ID == m.modelID
		dot := " "
		if active {
			dot = theme.StatusWorking
		}
		raw := fmt.Sprintf("%s %-34s %-9s %-5s%s", dot, mi.ID, mi.Provider, winStr, tagStr)
		var line string
		switch {
		case i == m.modelPickIdx:
			line = selectLine(true, raw)
		case active:
			line = "  " + styleStatus.Render(raw)
		default:
			line = "  " + raw
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func dim(s string) string { return styleReason.Render(s) }
