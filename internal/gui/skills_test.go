package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallSkillFromPathEmpty rejects a blank path before touching the
// filesystem or building a scanner.
func TestInstallSkillFromPathEmpty(t *testing.T) {
	b := &Bridge{}
	if _, err := b.InstallSkillFromPath("   "); err == nil {
		t.Fatal("expected error for empty skill path")
	}
}

// TestInstallSkillFromGitHubBadRef rejects a malformed reference at parse time
// (before any fetch or scanner construction), surfacing ParseGitHubRef's error.
func TestInstallSkillFromGitHubBadRef(t *testing.T) {
	b := &Bridge{}
	if _, err := b.InstallSkillFromGitHub("nope"); err == nil {
		t.Fatal("a bare token without owner/repo should error")
	}
}

// TestUserSkillsDir resolves to the per-user store under the home directory —
// the same target the CLI installs into.
func TestUserSkillsDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	want := filepath.Join(home, ".eigen", "skills")
	if got := userSkillsDir(); got != want {
		t.Fatalf("userSkillsDir() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(want, filepath.Join(".eigen", "skills")) {
		t.Fatalf("unexpected skills dir suffix: %q", want)
	}
}
