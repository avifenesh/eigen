package tui

import (
	"testing"

	"github.com/avifenesh/eigen/internal/theme"
)

// TestActiveSessionUsesFocusNotBrand pins the brand rule at the ROLE level
// (lipgloss strips color in non-TTY tests, so assert the data, not the render):
// the active session uses Focus, which must NOT be the brand Accent/Title
// (palette-agnostic — the specific hue varies per palette).
func TestActiveSessionUsesFocusNotBrand(t *testing.T) {
	if theme.Focus.Dark == theme.Accent.Dark {
		t.Error("Focus must not equal brand Accent (brand color is reserved for brand)")
	}
	if theme.Focus.Dark == theme.Title.Dark {
		t.Error("Focus must not equal brand Title (brand color is reserved for brand)")
	}
}
