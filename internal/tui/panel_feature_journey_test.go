package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/charmbracelet/x/ansi"
)

func TestTUITasksPanelFeatureJourney(t *testing.T) {
	m := tasksModel(t)
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-1", Task: "compile assets", Status: "done", Result: "compiled ok",
		Started: time.Now().Add(-time.Minute), Finished: time.Now(), InTokens: 12, OutTokens: 34,
	})
	seedTask(t, m, agent.BgTask{
		ID: "bg-12345678-2", Task: "long integration run", Status: "running",
		Started: time.Now(), Pid: os.Getpid(), LastTool: "bash", ToolStarted: time.Now().Add(-2 * time.Second),
	})
	m.command("/tasks")
	if m.rightTab != rightTabTasks {
		t.Fatalf("/tasks should open tasks tab, got %v", m.rightTab)
	}
	m.toggleTaskExpand(taskIndex(t, m, "bg-12345678-1"))
	plain := ansi.Strip(m.View())
	for _, want := range []string{"compiled ok", "↑12 ↓34", "tasks"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("tasks feature journey missing %q:\n%s", want, plain)
		}
	}
	m.toggleTaskExpand(taskIndex(t, m, "bg-12345678-2"))
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "[cancel]") {
		t.Fatalf("running task journey should expose cancel row:\n%s", plain)
	}
	m.tasks.sel = taskIndex(t, m, "bg-12345678-2")
	m.cancelSelectedTask()
	if !m.ov.active {
		t.Fatal("cancel should require confirmation")
	}
	m.overlayKey("y")
	if _, err := os.Stat(filepath.Join(m.storeDir(), "bg-12345678-2.cancel")); err != nil {
		t.Fatalf("confirmed task cancel should write marker: %v", err)
	}
}

func TestTUIShellsPanelFeatureJourney(t *testing.T) {
	m, be := shellModel(t, []chat.ShellInfo{{ID: "shell-1", Command: "npm run dev", Status: "running", LastLine: "listening on :3000"}})
	m.setRightTab(rightTabShells)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"shell-1", "npm run dev", "shells"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("shells feature journey missing %q:\n%s", want, plain)
		}
	}
	m.shellsClick(1) // first body row: select + expand shell-1
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "listening on :3000") || !strings.Contains(plain, "[kill]") {
		t.Fatalf("expanded shell should show tail and kill action:\n%s", plain)
	}
	rows := m.shellsRows()
	killLY := -1
	for i, r := range rows {
		if r.kind == shellRowKill {
			killLY = i + 1
		}
	}
	if killLY < 0 {
		t.Fatal("expanded running shell should have kill row")
	}
	m.shellsClick(killLY)
	if len(be.killed) != 1 || be.killed[0] != "shell-1" {
		t.Fatalf("kill row should invoke backend kill, killed=%v", be.killed)
	}
}
