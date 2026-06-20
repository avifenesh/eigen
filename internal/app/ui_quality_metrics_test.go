package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestAppObjectiveUIQualityMetrics(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 34})
	for _, spec := range pages {
		m.setActive(spec.page)
		view := m.View()
		plain := ansi.Strip(view)
		lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
		if len(lines) > m.height {
			t.Fatalf("%s renders %d rows, terminal height %d", spec.name, len(lines), m.height)
		}
		for i, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("%s line %d overflows: width=%d terminal=%d line=%q", spec.name, i+1, ansi.StringWidth(line), m.width, line)
			}
		}
		for _, token := range []string{"eigen", spec.name, spec.purpose, spec.action} {
			if !strings.Contains(plain, token) {
				t.Fatalf("%s missing premium/navigation token %q in view:\n%s", spec.name, token, plain)
			}
		}
		if strings.Contains(plain, "TODO") || strings.Contains(plain, "lorem") || strings.Contains(plain, "undefined") {
			t.Fatalf("%s contains placeholder/debug copy:\n%s", spec.name, plain)
		}
	}
}
