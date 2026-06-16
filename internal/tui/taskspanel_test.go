package tui

// Tier 12 Wave 2: the [tasks] right-panel tab — rendering, row model + click
// parity, badge, cancel flow, tick refresh.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// seedTask writes a fabricated task state file into m's isolated store dir.
func seedTask(t *testing.T, m *model, task agent.BgTask) {
	t.Helper()
	line, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.storeDir(), task.ID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(line, '\n'))
	f.Close()
}

func tasksModel(t *testing.T) *model {
	t.Helper()
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	return m
}

func TestTasksTabRendersTasks(t *testing.T) {
	m := tasksModel(t)
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-1", Task: "scan the docs", Status: "running",
		Started: time.Now().Add(-90 * time.Second), Pid: os.Getpid(), LastTool: "grep",
		ToolStarted: time.Now().Add(-5 * time.Second), Steps: 3,
	})
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-2", Task: "old work", Status: "done", Result: "ALLDONE",
		Started: time.Now().Add(-10 * time.Minute), Finished: time.Now().Add(-9 * time.Minute),
	})
	m.setRightTab(rightTabTasks)
	band := ansi.Strip(m.transcriptBand())
	if !strings.Contains(band, "[tasks]") && !strings.Contains(band, "[tsk]") {
		t.Fatalf("tasks tab missing [tasks]/[tsk]:\n%s", band)
	}
	for _, want := range []string{"…5678-1", "grep", "…5678-2", "→ view result"} {
		if !strings.Contains(band, want) {
			t.Fatalf("tasks tab missing %q:\n%s", want, band)
		}
	}
}

func TestTasksTabEmptyState(t *testing.T) {
	m := tasksModel(t)
	m.setRightTab(rightTabTasks)
	band := ansi.Strip(m.transcriptBand())
	if !strings.Contains(band, "nothing running in the background") {
		t.Fatalf("empty state missing:\n%s", band)
	}
}

func TestTasksExpandShowsResultAndCancelRow(t *testing.T) {
	m := tasksModel(t)
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-1", Task: "do a thing", Status: "done",
		Result: "FIRST LINE\nsecond line", Started: time.Now().Add(-time.Minute),
		Finished: time.Now(), InTokens: 100, OutTokens: 20,
	})
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-2", Task: "long runner", Status: "running",
		Started: time.Now(), Pid: os.Getpid(),
	})
	m.setRightTab(rightTabTasks)

	// Expand the done task: result body + usage appear.
	m.toggleTaskExpand(taskIndex(t, m, "bg-12345678-1"))
	band := ansi.Strip(m.transcriptBand())
	for _, want := range []string{"FIRST LINE", "second line", "↑100 ↓20"} {
		if !strings.Contains(band, want) {
			t.Fatalf("expanded done task missing %q:\n%s", want, band)
		}
	}
	// Expand the running task instead: [cancel] action row appears.
	m.toggleTaskExpand(taskIndex(t, m, "bg-12345678-2"))
	band = ansi.Strip(m.transcriptBand())
	if !strings.Contains(band, "[cancel]") {
		t.Fatalf("expanded running task missing [cancel]:\n%s", band)
	}
	if strings.Contains(band, "FIRST LINE") {
		t.Fatal("expanding another task should collapse the first")
	}
}

func TestTasksExpandedDetailScrolls(t *testing.T) {
	m := tasksModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	var result strings.Builder
	for i := 0; i < 40; i++ {
		result.WriteString("result line " + itoa(i) + "\n")
	}
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-1", Task: "long result", Status: "done",
		Result: result.String(), Started: time.Now().Add(-time.Minute), Finished: time.Now(),
	})
	m.setRightTab(rightTabTasks)
	m.toggleTaskExpand(taskIndex(t, m, "bg-12345678-1"))

	before := ansi.Strip(strings.Join(m.tasksLines(m.vp.Height), "\n"))
	if !strings.Contains(before, "result line 0") {
		t.Fatalf("expanded task should start at the result top:\n%s", before)
	}
	if strings.Contains(before, "result line 30") {
		t.Fatalf("unscrolled panel should not already show late result lines:\n%s", before)
	}

	l := m.computeLayout()
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: l.rightPanel.x + 3, Y: l.rightPanel.y + 3})
	if m.tasks.scroll == 0 {
		t.Fatal("mouse wheel over the tasks panel should scroll task detail")
	}

	m.scrollTasks(30)
	after := ansi.Strip(strings.Join(m.tasksLines(m.vp.Height), "\n"))
	if !strings.Contains(after, "result line 30") {
		t.Fatalf("scrolling tasks should reveal later result lines:\n%s", after)
	}
}

func taskIndex(t *testing.T, m *model, id string) int {
	t.Helper()
	m.refreshTasks()
	for i, task := range m.tasks.tasks {
		if task.ID == id {
			return i
		}
	}
	t.Fatalf("task %s not loaded", id)
	return -1
}

func TestTasksClickExpandsAndCancels(t *testing.T) {
	m := tasksModel(t)
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-1", Task: "long runner", Status: "running",
		Started: time.Now(), Pid: os.Getpid(),
	})
	m.setRightTab(rightTabTasks)
	m.refreshTasks()

	// Click row 1 (first task under the header): expands.
	if cmd := m.tasksClick(1); cmd != nil {
		cmd()
	}
	if m.tasks.expanded != "bg-12345678-1" {
		t.Fatalf("click should expand, expanded=%q", m.tasks.expanded)
	}
	// Find the [cancel] row in the row model and click it → confirm overlay.
	rows := m.tasksRows(m.rightCols() - 4)
	cancelRow := -1
	for i, r := range rows {
		if r.kind == trCancel {
			cancelRow = i + 1 // +1: header row offset
		}
	}
	if cancelRow < 0 {
		t.Fatal("no [cancel] row for expanded running task")
	}
	m.tasksClick(cancelRow)
	if !m.ov.active {
		t.Fatal("cancel must confirm via overlay, not fire silently")
	}
	// Accept the confirm: the marker file lands.
	m.overlayKey("y")
	if _, err := os.Stat(filepath.Join(m.storeDir(), "bg-12345678-1.cancel")); err != nil {
		t.Fatal("cancel marker not written after confirm")
	}
}

func TestTasksCancelOnFinishedTaskRefuses(t *testing.T) {
	m := tasksModel(t)
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-1", Task: "done already", Status: "done", Result: "x",
		Started: time.Now().Add(-time.Minute), Finished: time.Now(),
	})
	m.setRightTab(rightTabTasks)
	m.refreshTasks()
	m.tasks.sel = 0
	m.cancelSelectedTask()
	if m.ov.active {
		t.Fatal("finished task must not open a cancel confirm")
	}
}

func TestTasksBadgeStates(t *testing.T) {
	m := tasksModel(t)
	if m.tasksBadge() != "" {
		t.Fatalf("no tasks → no badge, got %q", m.tasksBadge())
	}
	seedTask(t, m, agent.BgTask{ID: "bg-12345678-1", Status: "done", Result: "x",
		Started: time.Now().Add(-time.Minute), Finished: time.Now()})
	m.refreshTasks()
	if got := m.tasksBadge(); !strings.Contains(got, "1✓") {
		t.Fatalf("done badge: %q", got)
	}
	seedTask(t, m, agent.BgTask{ID: "bg-12345678-2", Status: "running",
		Started: time.Now(), Pid: os.Getpid()})
	m.refreshTasks()
	if got := m.tasksBadge(); !strings.Contains(got, "1●") {
		t.Fatalf("running beats done in the badge: %q", got)
	}
	// The badge row appears in the sidebar and clicking it opens the tab.
	found := false
	for _, r := range m.sidebarRows() {
		if r.action == actTasksTab {
			found = true
		}
	}
	if !found {
		t.Fatal("sidebar should show the tasks badge row")
	}
}

func TestTasksSlashCommandOpensTab(t *testing.T) {
	m := tasksModel(t)
	m.command("/tasks")
	if m.rightTab != rightTabTasks {
		t.Fatalf("/tasks should select the tasks tab, got %v", m.rightTab)
	}
}

func TestTasksTickRefreshesAndStaleGenIgnored(t *testing.T) {
	m := tasksModel(t)
	m.setRightTab(rightTabTasks)
	gen := m.tasks.gen
	seedTask(t, m, agent.BgTask{ID: "bg-12345678-1", Status: "running",
		Started: time.Now(), Pid: os.Getpid()})
	// Live tick re-reads the store.
	m.Update(tasksTickMsg{gen: gen})
	if len(m.tasks.tasks) != 1 {
		t.Fatalf("tick should refresh tasks, got %d", len(m.tasks.tasks))
	}
	// A stale generation must not re-arm (no infinite old chains).
	m.tasks.tasks = nil
	m.Update(tasksTickMsg{gen: gen - 1})
	if m.tasks.tasks != nil {
		t.Fatal("stale tick must be ignored")
	}
}

func TestTasksExpansionFollowsRemoval(t *testing.T) {
	m := tasksModel(t)
	seedTask(t, m, agent.BgTask{ID: "bg-12345678-1", Status: "done", Result: "x",
		Started: time.Now().Add(-time.Minute), Finished: time.Now()})
	m.setRightTab(rightTabTasks)
	m.refreshTasks()
	m.toggleTaskExpand(0)
	if m.tasks.expanded == "" {
		t.Fatal("expand failed")
	}
	// Task file vanishes (pruned by another process): expansion clears.
	os.Remove(filepath.Join(m.storeDir(), "bg-12345678-1.jsonl"))
	m.refreshTasks()
	if m.tasks.expanded != "" {
		t.Fatal("expansion must clear when the task disappears")
	}
}

func TestShortTaskIDAndCompactDuration(t *testing.T) {
	if got := shortTaskID("bg-1781281767132806736-1"); got != "bg-…6736-1" {
		t.Fatalf("shortTaskID: %q", got)
	}
	if got := shortTaskID("weird"); got != "weird" {
		t.Fatalf("shortTaskID passthrough: %q", got)
	}
	cases := map[time.Duration]string{
		42 * time.Second:               "42s",
		3*time.Minute + 10*time.Second: "3m10s",
		62 * time.Minute:               "1h02m",
	}
	for d, want := range cases {
		if got := compactDuration(d); got != want {
			t.Fatalf("compactDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
