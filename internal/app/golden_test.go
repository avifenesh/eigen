package app

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func assertGoldenContains(t *testing.T, goldenPath, rendered string) {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(plain, line) {
			t.Fatalf("rendered view missing golden token %q from %s:\n%s", line, goldenPath, plain)
		}
	}
}

func TestAppHomeGoldenSnapshotTokens(t *testing.T) {
	m := layoutModel(t, 80, 20)
	m.active = PageHome
	assertGoldenContains(t, "testdata/golden/home_80x20.txt", m.View())
}

func TestAppEveryPageGoldenSnapshotTokens(t *testing.T) {
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			m := layoutModel(t, 120, 30)
			m.active = p.page
			plain := ansi.Strip(m.View())
			for _, want := range []string{"eigen", p.name} {
				if !strings.Contains(plain, want) {
					t.Fatalf("page %s missing golden token %q:\n%s", p.name, want, plain)
				}
			}
			if strings.Contains(plain, "[home]") {
				t.Fatalf("page %s regressed to classic header buttons:\n%s", p.name, plain)
			}
		})
	}
}

func TestAppPaletteGoldenSnapshotTokens(t *testing.T) {
	m := layoutModel(t, 100, 24)
	m.Update(key(":"))
	for _, r := range "plug" {
		m.Update(key(string(r)))
	}
	assertGoldenContains(t, "testdata/golden/palette_plugins_100x24.txt", m.View())
}
