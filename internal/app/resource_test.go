package app

import (
	"github.com/avifenesh/eigen/internal/theme"
	"strings"
	"testing"
)

func TestAppRenderSoakPaintsAndDoesNotLeakGoroutines(t *testing.T) {
	m := layoutModel(t, 120, 30)
	m.active = PageLive
	before := settledGoroutines(t)
	bg := bgSeq(theme.Active.Base.Dark)
	for i := 0; i < 250; i++ {
		v := m.View()
		lines := strings.Split(v, "\n")
		if len(lines) != 30 {
			t.Fatalf("iteration %d rows=%d want 30", i, len(lines))
		}
		for row, ln := range lines {
			if !strings.HasPrefix(ln, bg) {
				t.Fatalf("iteration %d row %d is not base painted", i, row)
			}
		}
	}
	assertGoroutineBound(t, before, 2, "app render soak")
}
