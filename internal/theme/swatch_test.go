package theme

import (
	"strings"
	"testing"
)

func TestSwatchRendersAllRolesAndRule(t *testing.T) {
	out := Swatch()
	for _, role := range []string{"Text", "Accent", "Title", "Focus", "Sel", "Ok", "Warn", "Err", "Tool", "Code", "Link", "Working"} {
		if !strings.Contains(out, role) {
			t.Errorf("swatch missing role %q", role)
		}
	}
	for _, section := range []string{"roles", "elevation", "icons", "ramps", "weight", "glyphs"} {
		if !strings.Contains(out, section) {
			t.Errorf("swatch missing section %q", section)
		}
	}
	if !strings.Contains(out, "brand rule") {
		t.Error("swatch should restate the brand rule")
	}
	// The brand glyph + the focus pointer must appear.
	if !strings.Contains(out, "λ") || !strings.Contains(out, "❯") {
		t.Error("swatch should show the λ mark and ❯ pointer")
	}
}
