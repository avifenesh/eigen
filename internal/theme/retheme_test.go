package theme

import "testing"

// TestReThemeSwapsAllRoles proves the roles-not-hues discipline: selecting a
// different named palette changes the role colors (a whole re-theme), and the
// brand rule holds in every palette (Focus/Sel are NOT the brand Accent/Title).
func TestReThemeSwapsAllRoles(t *testing.T) {
	nord := selectPalette("nord")
	gruv := selectPalette("gruvbox")
	if nord.Accent == gruv.Accent {
		t.Error("re-theme should change Accent across palettes")
	}
	if selectPalette("nonexistent").Name != "deepteal" {
		t.Error("unknown theme should fall back to the default (deepteal)")
	}
	// Brand rule invariant in EVERY palette: Focus/Sel must differ from the
	// brand blues (Accent/Title) — selection/active must never be brand.
	for _, p := range []Palette{deepTealPalette, nord, gruv} {
		if p.Focus == p.Accent || p.Focus == p.Title {
			t.Errorf("%s: Focus must not equal brand Accent/Title", p.Name)
		}
		if p.Sel == p.Accent || p.Sel == p.Title {
			t.Errorf("%s: Sel must not equal brand Accent/Title", p.Name)
		}
	}
	for _, n := range PaletteNames() {
		if _, ok := palettes[n]; !ok {
			t.Errorf("PaletteNames lists %q but it's not registered", n)
		}
	}
}
