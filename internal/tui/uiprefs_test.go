package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUIPrefsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveUIPrefs(uiPrefs{RailW: 30, RightW: 44})
	got := loadUIPrefs()
	if got.RailW != 30 || got.RightW != 44 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// File actually written under ~/.eigen/ui.json.
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".eigen", "ui.json")); err != nil {
		t.Fatalf("ui.json not written: %v", err)
	}
}

func TestUIPrefsMissingYieldsZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := loadUIPrefs()
	if got.RailW != 0 || got.RightW != 0 {
		t.Fatalf("missing file should yield zero prefs, got %+v", got)
	}
}

func TestUIPrefsMalformedYieldsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".eigen"), 0o755)
	os.WriteFile(filepath.Join(home, ".eigen", "ui.json"), []byte("{not json"), 0o644)
	if got := loadUIPrefs(); got.RailW != 0 || got.RightW != 0 {
		t.Fatalf("malformed file should yield zero prefs, got %+v", got)
	}
}

func TestPersistPanelWidthsFromModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := &model{railW: 26, rightW: 50}
	m.persistPanelWidths()
	if got := loadUIPrefs(); got.RailW != 26 || got.RightW != 50 {
		t.Fatalf("model widths not persisted: %+v", got)
	}
}
