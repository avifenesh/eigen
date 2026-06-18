package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedComponentsContainCargoManifests(t *testing.T) {
	for _, name := range ComponentNames() {
		c := Components[name]
		if _, err := SourceFS.Open(c.SourceDir + "/Cargo.toml"); err != nil {
			t.Fatalf("%s missing embedded Cargo.toml: %v", name, err)
		}
		if len(c.Binaries) == 0 {
			t.Fatalf("%s has no install binaries", name)
		}
	}
}

func TestCopyExecutableReplacesExistingFileAtomically(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	dst := filepath.Join(dir, "dst-bin")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(src, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Fatalf("destination not replaced: %q", b)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".dst-bin-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func TestMaterializeEmbeddedSource(t *testing.T) {
	cases := map[string][]string{
		"computer-use": {"Cargo.toml", "Cargo.lock", filepath.Join("src", "main.rs"), filepath.Join("gnome-shell-extension", "computer-use-linux@avifenesh.dev", "extension.js")},
		"workspace":    {"Cargo.toml", "Cargo.lock", filepath.Join("src", "main.rs"), filepath.Join("src", "workspace.rs")},
	}
	for name, wants := range cases {
		root, cleanup, err := Materialize(Components[name])
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range wants {
			if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
				cleanup()
				t.Fatalf("%s materialized source missing %s: %v", name, rel, err)
			}
		}
		cleanup()
	}
}
