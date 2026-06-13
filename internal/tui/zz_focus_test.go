package tui

import (
	"testing"

	"github.com/avifenesh/eigen/internal/theme"
)

// TestActiveSessionUsesFocusNotBrand pins the brand rule at the ROLE level
// (lipgloss strips color in non-TTY tests, so assert the data, not the render):
// the active session uses Focus (rose), NOT the brand Accent/Title blue.
func TestActiveSessionUsesFocusNotBrand(t *testing.T) {
	if theme.Focus.Dark != "#D1A0B0" {
		t.Errorf("Focus dark = %q, want the non-blue rose #D1A0B0", theme.Focus.Dark)
	}
	if theme.Focus.Dark == theme.Accent.Dark {
		t.Error("Focus must not equal brand Accent (blue is reserved for brand)")
	}
	if theme.Focus.Dark == theme.Title.Dark {
		t.Error("Focus must not equal brand Title (blue is reserved for brand)")
	}
}
