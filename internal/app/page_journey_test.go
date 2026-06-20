package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAppEveryPageKeyboardJourney(t *testing.T) {
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			m := New(testData())
			m.width, m.height = 120, 30
			m.Update(key(":"))
			for _, r := range p.name {
				m.Update(key(string(r)))
			}
			m.Update(key("enter"))
			if m.active != p.page {
				t.Fatalf("palette journey to %s landed on %s", p.name, m.activeName())
			}
			plain := ansi.Strip(m.View())
			if !strings.Contains(plain, "eigen") || !strings.Contains(plain, p.name) {
				t.Fatalf("page %s journey missing shell/page identity:\n%s", p.name, plain)
			}
			if strings.Contains(plain, "[home]") {
				t.Fatalf("page %s regressed to classic header buttons:\n%s", p.name, plain)
			}
		})
	}
}

func TestAppEveryPageQuickJumpJourney(t *testing.T) {
	m := New(testData())
	m.width, m.height = 120, 30
	for _, p := range pages {
		m.active = PageHome
		m.Update(key("g" + p.key))
		if m.active != p.page {
			t.Fatalf("quick jump g%s should land on %s, got %s", p.key, p.name, m.activeName())
		}
	}
}
