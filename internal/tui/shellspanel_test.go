package tui

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/chat"
	tea "github.com/charmbracelet/bubbletea"
)

// shellBackend is a fake chat.Backend exposing canned shells + recording kills.
type shellBackend struct {
	chat.Backend
	shells []chat.ShellInfo
	killed []string
}

func (s *shellBackend) Shells() []chat.ShellInfo { return s.shells }
func (s *shellBackend) KillShell(id string) bool {
	s.killed = append(s.killed, id)
	for i := range s.shells {
		if s.shells[i].ID == id && s.shells[i].Status == "running" {
			s.shells[i].Status = "killed"
			return true
		}
	}
	return false
}

func shellModel(t *testing.T, shells []chat.ShellInfo) (*model, *shellBackend) {
	t.Helper()
	m := testModel(t)
	be := &shellBackend{Backend: m.backend, shells: shells}
	m.backend = be
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m, be
}

func TestShellsTabAppearsWhenShellsExist(t *testing.T) {
	m, _ := shellModel(t, nil)
	for _, tab := range m.rightTabs() {
		if tab == rightTabShells {
			t.Fatal("shells tab should be hidden when there are no shells")
		}
	}
	m, _ = shellModel(t, []chat.ShellInfo{{ID: "shell-1", Command: "npm run dev", Status: "running"}})
	var found bool
	for _, tab := range m.rightTabs() {
		if tab == rightTabShells {
			found = true
		}
	}
	if !found {
		t.Fatal("shells tab should appear when a shell exists")
	}
}

func TestShellsPanelRendersAndExpands(t *testing.T) {
	m, _ := shellModel(t, []chat.ShellInfo{
		{ID: "shell-1", Command: "npm run dev", Status: "running", LastLine: "listening on :3000"},
		{ID: "shell-2", Command: "make build", Status: "exited", ExitCode: 0},
	})
	m.setRightTab(rightTabShells)
	out := strings.Join(m.shellsLines(12), "\n")
	if !strings.Contains(out, "shell-1") || !strings.Contains(out, "npm run dev") {
		t.Fatalf("panel should list shells, got:\n%s", out)
	}
	if !strings.Contains(out, "exit 0") {
		t.Fatalf("finished shell should show its exit code, got:\n%s", out)
	}
	// Expand shell-1 → its last output line shows.
	m.shells.expanded = "shell-1"
	out = strings.Join(m.shellsLines(12), "\n")
	if !strings.Contains(out, "listening on :3000") {
		t.Fatalf("expanded shell should show its last line, got:\n%s", out)
	}
}

func TestShellsPanelKillFromClick(t *testing.T) {
	m, be := shellModel(t, []chat.ShellInfo{{ID: "shell-1", Command: "sleep 300", Status: "running"}})
	m.setRightTab(rightTabShells)
	// Expand it so the [kill] row exists, then click that row.
	m.shells.expanded = "shell-1"
	rows := m.shellsRows()
	killLY := -1
	for i, r := range rows {
		if r.kind == shellRowKill {
			killLY = i + 1 // +1 for the title line
		}
	}
	if killLY < 0 {
		t.Fatal("a running expanded shell should have a [kill] row")
	}
	m.shellsClick(killLY)
	if len(be.killed) != 1 || be.killed[0] != "shell-1" {
		t.Fatalf("clicking [kill] should kill the shell, killed=%v", be.killed)
	}
}

// detachBackend records DetachBash calls.
type detachBackend struct {
	chat.Backend
	detachCalls int
	detachOK    bool
}

func (d *detachBackend) DetachBash() bool { d.detachCalls++; return d.detachOK }

func TestAltDDetachesRunningBash(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	be := &detachBackend{Backend: m.backend, detachOK: true}
	m.backend = be
	m.state = stRunning
	// alt+d while running → DetachBash on the backend.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true})
	if be.detachCalls != 1 {
		t.Fatalf("alt+d while running should call DetachBash once, got %d", be.detachCalls)
	}
}
