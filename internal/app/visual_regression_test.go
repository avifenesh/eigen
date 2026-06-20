package app

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestAppPremiumShellVisualContract(t *testing.T) {
	m := layoutModel(t, 120, 30)
	m.active = PageHome
	v := m.View()
	plain := ansi.Strip(v)
	for _, want := range []string{
		"λ eigen",
		"home",
		"live",
		"sessions",
		"projects",
		"providers",
		"models",
		"plugins",
		"scanning for things to act on",
		"recent",
		"sessions",
		"q quit",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("premium shell missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "[home]") || strings.Contains(plain, "[sessions]") {
		t.Fatalf("wide app shell should use premium sidebar, not classic header buttons:\n%s", plain)
	}
	if !strings.Contains(v, bgSeq(theme.Active.Base.Dark)) {
		t.Fatal("premium shell should be painted on the Base canvas")
	}
}

func TestAppPaletteVisualContract(t *testing.T) {
	m := layoutModel(t, 120, 30)
	m.Update(key(":"))
	for _, r := range "plug" {
		m.Update(key(string(r)))
	}
	v := m.View()
	plain := ansi.Strip(v)
	for _, want := range []string{"command", "› plug", "go: plugins", "page"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("palette visual missing %q:\n%s", want, plain)
		}
	}
	for _, line := range strings.Split(v, "\n") {
		if !strings.HasPrefix(line, bgSeq(theme.Active.Base.Dark)) {
			t.Fatalf("palette overlay row not base-painted: %q", line)
		}
	}
}

func TestAppSelectionUsesSelectionRoleNotAccent(t *testing.T) {
	d := testData()
	d.Live = []daemon.SessionInfo{{ID: "s1", Title: "selected live session", Dir: "/p", Status: daemon.StatusIdle}}
	m := New(d)
	m.width, m.height = 100, 24
	m.active = PageLive
	v := m.View()
	if strings.Contains(ansi.Strip(v), "▸ ") {
		t.Fatal("live selection must not use the old brand-accent arrow")
	}
	if !strings.Contains(ansi.Strip(v), "▎") {
		t.Fatal("live selection should use the unified selection bar")
	}
}
