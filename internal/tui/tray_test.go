package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTrayListsApprovalAndErrorSessions(t *testing.T) {
	m := switcherModel(t) // s3 = approval; s1 idle, s2 working
	sb := m.backend.(*switchBackend)
	sb.entries[0].Status = "error" // s1 now errored
	m.openTray()
	if !m.tray {
		t.Fatal("tray should be open")
	}
	if len(m.trayItems) != 2 {
		t.Fatalf("want 2 needs-you items (approval s3 + error s1), got %d: %+v", len(m.trayItems), m.trayItems)
	}
	// Idle/working sessions must not appear.
	for _, it := range m.trayItems {
		if it.status != "approval" && it.status != "error" {
			t.Fatalf("unexpected status in tray: %q", it.status)
		}
	}
}

func TestTrayPendingApprovalLeads(t *testing.T) {
	m := switcherModel(t)
	m.pending = &pendingApproval{id: "a1", name: "bash", args: `{"cmd":"ls"}`}
	m.openTray()
	if len(m.trayItems) == 0 || !m.trayItems[0].current {
		t.Fatalf("this window's pending approval should lead the tray: %+v", m.trayItems)
	}
	if !strings.Contains(m.trayItems[0].title, "bash") {
		t.Fatalf("pending row should name the tool: %q", m.trayItems[0].title)
	}
}

func TestTrayActivateHopsToSession(t *testing.T) {
	m := switcherModel(t)
	m.openTray() // only s3 (approval); s2 is current/working
	if len(m.trayItems) != 1 {
		t.Fatalf("want 1 item, got %d", len(m.trayItems))
	}
	m.trayIdx = 0
	handled, quit := m.trayActivate()
	if !handled || !quit {
		t.Fatalf("activating a sibling approval should hop (handled=%v quit=%v)", handled, quit)
	}
	if m.switchTo != "s3" {
		t.Fatalf("should hop to s3, got %q", m.switchTo)
	}
	if m.tray {
		t.Fatal("tray should close on activate")
	}
}

func TestTrayNotificationsRing(t *testing.T) {
	m := switcherModel(t)
	m.note("first thing")
	m.note("second thing")
	m.openTray()
	out := m.trayView()
	if !strings.Contains(out, "recent") || !strings.Contains(out, "second thing") {
		t.Fatalf("tray should show recent notifications:\n%s", out)
	}
}

func TestNoteRingCapped(t *testing.T) {
	m := switcherModel(t)
	for i := 0; i < maxNotif+20; i++ {
		m.note("n")
	}
	if len(m.notif) != maxNotif {
		t.Fatalf("notif ring should cap at %d, got %d", maxNotif, len(m.notif))
	}
}

func TestTrayKeyCloses(t *testing.T) {
	m := switcherModel(t)
	m.openTray()
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.tray {
		t.Fatal("esc should close the tray")
	}
}

func TestTrayKeyAltW(t *testing.T) {
	// alt+w opens the tray (zellij-safe; zellij binds alt+n to NewPane).
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}, Alt: true})
	if !m.tray {
		t.Fatal("alt+w should open the tray")
	}
	// alt+n still works for terminals that don't capture it.
	m2 := switcherModel(t)
	m2.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}, Alt: true})
	if !m2.tray {
		t.Fatal("alt+n should still open the tray on non-zellij terminals")
	}
}
