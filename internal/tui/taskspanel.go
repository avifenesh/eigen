package tui

// The [tasks] right-panel tab (Tier 12): live visibility into background
// delegations. The surface is the durable task store on disk
// (agent.LoadBgTasks) — the TUI is a separate process from the daemon that
// hosts the goroutines, so the jsonl records ARE the protocol. Rows follow the
// rail's row-model convention: tasksRows() is walked by both the renderer
// (tasksLines) and the click hit-test (tasksRowAt), so geometry cannot drift.
// Refresh: own 2s tick while the tab is visible (tasksTickMsg, generation-
// guarded like the terminal tab). Controls go through the action registry:
// enter/click expands a task (result / error / live progress), c cancels a
// running one (confirm overlay → agent.RequestCancel marker).

import (
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// tasksRefresh paces the tasks tab's disk re-read while visible.
const tasksRefresh = 2 * time.Second

// tasksState is the tab's UI state. tasks is the last disk read; expanded is
// the expanded task id ("" = none); sel is the selected row index into the
// task rows (kept in range as the list changes under the tick).
type tasksState struct {
	tasks     []agent.BgTask
	expanded  string
	sel       int
	loaded    bool
	ticking   bool
	gen       int
	refreshed time.Time // last store read (throttles piggybacked refreshes)
}

// tasksTickMsg drives the periodic refresh (gen-guarded so stale ticks from a
// previous visibility stretch are ignored, and only one chain runs).
type tasksTickMsg struct{ gen int }

// tasksTick schedules the next refresh while the tab is visible.
func (m *model) tasksTick() tea.Cmd {
	if m.rightTab != rightTabTasks || !m.changesVisible() {
		m.tasks.ticking = false
		return nil
	}
	m.tasks.ticking = true
	gen := m.tasks.gen
	return tea.Tick(tasksRefresh, func(time.Time) tea.Msg { return tasksTickMsg{gen: gen} })
}

// storeDir is the background-task store directory (defaulting to the real
// ~/.eigen/tasks; tests point it at a temp dir).
func (m *model) storeDir() string {
	if m.tasksDir != "" {
		return m.tasksDir
	}
	return agent.TasksDir()
}

// refreshTasks re-reads the durable store and keeps selection/expansion valid.
func (m *model) refreshTasks() {
	m.tasks.tasks = agent.LoadBgTasks(m.storeDir())
	m.tasks.loaded = true
	m.tasks.refreshed = time.Now()
	if m.tasks.sel >= len(m.tasks.tasks) {
		m.tasks.sel = len(m.tasks.tasks) - 1
	}
	if m.tasks.sel < 0 {
		m.tasks.sel = 0
	}
	if m.tasks.expanded != "" {
		found := false
		for _, t := range m.tasks.tasks {
			if t.ID == m.tasks.expanded {
				found = true
				break
			}
		}
		if !found {
			m.tasks.expanded = ""
		}
	}
}

// taskRowKind distinguishes the row model's line types.
type taskRowKind int

const (
	trTask   taskRowKind = iota // a task's summary row (selectable)
	trDetail                    // an expanded detail line under its task
	trCancel                    // the expanded running task's [cancel] action row
	trEmpty                     // the empty-state line
)

// taskRow is one rendered panel line below the header.
type taskRow struct {
	kind taskRowKind
	task int    // index into m.tasks.tasks (trTask/trDetail)
	text string // rendered detail line (trDetail) or empty-state text
}

// tasksRows builds the row model: each task one summary row, the expanded
// task's detail lines directly under it.
func (m *model) tasksRows(contentW int) []taskRow {
	if len(m.tasks.tasks) == 0 {
		return []taskRow{{kind: trEmpty, task: -1, text: "no background tasks"}, {kind: trEmpty, task: -1, text: dim("task(background=true) starts one")}}
	}
	var rows []taskRow
	for i, t := range m.tasks.tasks {
		rows = append(rows, taskRow{kind: trTask, task: i})
		if t.ID == m.tasks.expanded {
			for _, ln := range taskDetailLines(t, contentW) {
				rows = append(rows, taskRow{kind: trDetail, task: i, text: ln})
			}
			if t.Status == "running" && !t.Canceling {
				rows = append(rows, taskRow{kind: trCancel, task: i, text: "  " + styleAsk.Render("[cancel]")})
			}
		}
	}
	return rows
}

// taskGlyph maps a task status to the shared glyph language.
func taskGlyph(status string, canceling bool) string {
	switch {
	case canceling:
		return styleAsk.Render("◌")
	case status == "running":
		return styleAccent.Render("●")
	case status == "done":
		return styleStatus.Render("✓")
	case status == "error":
		return styleErrTask.Render("✗")
	case status == "canceled":
		return dim("⊘")
	case status == "lost":
		return dim("?")
	}
	return dim("·")
}

var styleErrTask = styleAsk // amber/bold reads as failure without a new color

// taskSummaryLine renders one task's row: glyph, short id, elapsed, and the
// most useful live detail (running tool / result preview / error).
func taskSummaryLine(t agent.BgTask, now time.Time, w int) string {
	glyph := taskGlyph(t.Status, t.Canceling && t.Status == "running")
	id := shortTaskID(t.ID)
	var dur time.Duration
	if !t.Finished.IsZero() {
		dur = t.Finished.Sub(t.Started)
	} else {
		dur = now.Sub(t.Started)
	}
	elapsed := compactDuration(dur)
	detail := ""
	switch t.Status {
	case "running":
		switch {
		case t.Canceling:
			detail = "canceling…"
		case t.LastTool != "":
			detail = t.LastTool
			if !t.ToolStarted.IsZero() {
				detail += " " + compactDuration(now.Sub(t.ToolStarted))
			}
		case t.Steps > 0:
			detail = fmt.Sprintf("step %d", t.Steps)
		default:
			detail = "starting"
		}
	case "done":
		detail = "→ view result"
	case "error":
		detail = oneLineTrunc(t.Error, 40)
	case "lost":
		detail = "host gone"
	}
	line := glyph + " " + id + "  " + elapsed
	if detail != "" {
		line += "  " + detail
	}
	return ansiTrunc(line, w)
}

// taskDetailLines renders the expanded view of one task: what it is, where it
// ran, usage, and the result/error/progress body wrapped to the panel.
func taskDetailLines(t agent.BgTask, w int) []string {
	iw := w - 2 // "  " indent
	if iw < 8 {
		iw = 8
	}
	var out []string
	add := func(s string) {
		for _, ln := range strings.Split(ansi.Hardwrap(expandTabs(s), iw, true), "\n") {
			out = append(out, "  "+ln)
		}
	}
	add(dim("task: ") + oneLineTrunc(t.Task, 200))
	if t.Where != "" {
		add(dim("on:   ") + t.Where)
	}
	if t.InTokens > 0 || t.OutTokens > 0 {
		add(dim(fmt.Sprintf("toks: ↑%d ↓%d", t.InTokens, t.OutTokens)))
	}
	switch t.Status {
	case "done":
		add(dim("result:"))
		body := strings.TrimSpace(t.Result)
		if body == "" {
			body = "(empty)"
		}
		for _, ln := range strings.Split(body, "\n") {
			add(ln)
		}
	case "error":
		add(styleAsk.Render("error: ") + t.Error)
	case "running":
		if t.LastNote != "" {
			add(dim("note: ") + t.LastNote)
		}
	case "lost":
		add(dim("transcript: ~/.eigen/tasks/" + t.ID + ".transcript.jsonl"))
	}
	return out
}

// shortTaskID compresses bg-<nanos>-<seq> to a readable handle: the last 4
// digits of the timestamp plus the sequence keep it unique enough on screen
// while the full id stays in the store/expanded view.
func shortTaskID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 3 && len(parts[1]) > 4 {
		return "bg-…" + parts[1][len(parts[1])-4:] + "-" + parts[2]
	}
	return id
}

// compactDuration renders durations tersely: 42s, 3m10s, 1h02m.
func compactDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func oneLineTrunc(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// tasksLines renders the tab as exactly h panel lines (header + rows).
func (m *model) tasksLines(h int) []string {
	if !m.tasks.loaded {
		m.refreshTasks()
	}
	pw := m.rightCols()
	contentW := pw - 4
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(pw-2), pw))
	rows := m.tasksRows(contentW)
	now := time.Now()
	for i := 0; i < len(rows) && len(lines) < h; i++ {
		r := rows[i]
		var s string
		switch r.kind {
		case trTask:
			t := m.tasks.tasks[r.task]
			s = taskSummaryLine(t, now, contentW)
			if r.task == m.tasks.sel {
				s = "▸ " + s
			} else {
				s = "  " + s
			}
		case trDetail, trCancel:
			s = "  " + r.text
		case trEmpty:
			s = r.text
		}
		lines = append(lines, changesPad(ansiTrunc(s, contentW+2), pw))
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", pw))
	}
	return lines
}

// tasksRowAt maps a panel-local row (0 = header) to its taskRow.
func (m *model) tasksRowAt(localY int) (taskRow, bool) {
	if localY <= 0 {
		return taskRow{}, false
	}
	rows := m.tasksRows(m.rightCols() - 4)
	i := localY - 1
	if i < 0 || i >= len(rows) || rows[i].task < 0 {
		return taskRow{}, false
	}
	return rows[i], true
}

// tasksClick handles a left press inside the tasks tab content: a summary row
// selects + toggles expansion; the [cancel] row confirms a cancel.
func (m *model) tasksClick(localY int) tea.Cmd {
	r, ok := m.tasksRowAt(localY)
	if !ok {
		return nil
	}
	switch r.kind {
	case trCancel:
		m.tasks.sel = r.task
		return m.cancelSelectedTask()
	default:
		m.toggleTaskExpand(r.task)
	}
	return nil
}

// toggleTaskExpand expands/collapses the task at index i.
func (m *model) toggleTaskExpand(i int) {
	if i < 0 || i >= len(m.tasks.tasks) {
		return
	}
	id := m.tasks.tasks[i].ID
	if m.tasks.expanded == id {
		m.tasks.expanded = ""
	} else {
		m.tasks.expanded = id
	}
	m.tasks.sel = i
}

// cancelSelectedTask confirms then drops the cancel marker for the selected
// running task. The marker protocol works cross-process: the daemon hosting
// the goroutine observes it within ~2s.
func (m *model) cancelSelectedTask() tea.Cmd {
	if m.tasks.sel < 0 || m.tasks.sel >= len(m.tasks.tasks) {
		return nil
	}
	t := m.tasks.tasks[m.tasks.sel]
	if t.Status != "running" {
		m.note("task " + shortTaskID(t.ID) + " is " + t.Status + " — nothing to cancel")
		return nil
	}
	id := t.ID
	m.openConfirm("cancel background task "+shortTaskID(id)+"?", func(m *model) tea.Cmd {
		if err := agent.RequestCancel(m.storeDir(), id); err != nil {
			m.note("cancel: " + err.Error())
		} else {
			m.note("cancel requested for " + shortTaskID(id))
			m.refreshTasks()
		}
		return nil
	})
	return nil
}
