package app

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestFillBGReassertsAfterReset(t *testing.T) {
	got := fillBG("a\x1b[0mb\x1b[mc", theme.Active.Base.Dark, 8)
	bg := bgSeq(theme.Active.Base.Dark)
	if strings.Count(got, bg) < 3 {
		t.Fatalf("background should be reasserted after both reset forms; got %q", got)
	}
	if w := ansi.StringWidth(got); w != 8 {
		t.Fatalf("painted line width = %d, want 8: %q", w, got)
	}
}

func TestAppViewPaintsFullCanvas(t *testing.T) {
	m := layoutModel(t, 80, 14)
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) != 14 {
		t.Fatalf("rendered rows = %d, want 14", len(lines))
	}
	bg := bgSeq(theme.Active.Base.Dark)
	for i, ln := range lines {
		if !strings.HasPrefix(ln, bg) {
			t.Fatalf("line %d is not painted on base: %q", i, ln)
		}
		if w := ansi.StringWidth(ln); w != 80 {
			t.Fatalf("line %d width = %d, want 80: %q", i, w, ln)
		}
	}
}
