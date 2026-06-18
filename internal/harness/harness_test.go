package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestInstallOrientationWritesEngineAndWrapper(t *testing.T) {
	home := t.TempDir()
	dst := t.TempDir()
	t.Setenv("HOME", home)
	if err := InstallOrientation("/opt/eigen/bin/eigen", dst); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"consume.js", "hook.js", "state.js", "projects.txt"} {
		if _, err := os.Stat(filepath.Join(home, ".eigen", "orientation", rel)); err != nil {
			t.Fatalf("orientation install missing %s: %v", rel, err)
		}
	}
	wrapper := filepath.Join(dst, "orientation")
	b, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, "eigen orientation") || !strings.Contains(got, "/opt/eigen/bin/eigen") {
		t.Fatalf("wrapper should delegate to eigen orientation, got:\n%s", got)
	}
	info, err := os.Stat(filepath.Join(home, ".eigen", "orientation"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("orientation home mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestOrientationHooksInstallUsesHarnessWrapper(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	home := t.TempDir()
	dst := filepath.Join(home, ".local", "bin")
	t.Setenv("HOME", home)
	if err := InstallOrientation("/opt/eigen/bin/eigen", dst); err != nil {
		t.Fatal(err)
	}
	if err := InstallOrientationHooks(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".eigen", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, filepath.Join(home, ".local", "bin", "orientation")) || !strings.Contains(got, "hook --runtime") {
		t.Fatalf("hooks should call orientation wrapper, got:\n%s", got)
	}
	if strings.Contains(got, "hook.js") || strings.Contains(got, "ORIENTATION_ENGINE_DIR") {
		t.Fatalf("hooks should not hard-code node hook.js engine commands, got:\n%s", got)
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
