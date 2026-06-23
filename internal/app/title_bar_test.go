package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/avifenesh/eigen/internal/daemon"
)

// TestRenderTitleBarNeverOverflows guards the overflow handling in
// renderTitleBar: the rendered bar must never be wider than the title rect at
// any width. The previously-broken case is a too-wide RIGHT segment (the stats
// suffix alone exceeds the bar) — shedding the optional suffixes wasn't enough,
// so the bar still overflowed. The width must hold for that case too.
func TestRenderTitleBarNeverOverflows(t *testing.T) {
	// Many sessions + live entries make titleStats long, so on a narrow bar the
	// right segment alone is wider than the whole title width.
	d := testData()
	for i := 0; i < 40; i++ {
		d.Sessions = append(d.Sessions, SessionRow{ID: "x", Title: "t"})
		d.Live = append(d.Live, daemon.SessionInfo{ID: "l", Title: "t"})
	}
	m := NewAt(d, PageSessions)

	for w := 1; w <= 200; w++ {
		m.width, m.height = w, 30
		l := m.computeLayout()
		if l.title.empty() {
			continue
		}
		out := m.renderTitleBar(l)
		if got := lipgloss.Width(out); got > l.title.w {
			t.Fatalf("width %d: title bar width = %d, want <= %d (%q)", w, got, l.title.w, out)
		}
		if strings.Contains(out, "\n") {
			t.Fatalf("width %d: title bar must be a single line, got %q", w, out)
		}
	}
}
