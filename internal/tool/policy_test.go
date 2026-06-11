package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyResolveAllowsWithinRoot(t *testing.T) {
	dir := t.TempDir()
	p := NewPolicy(dir)

	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := p.Resolve(f)
	if err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	if got == "" {
		t.Fatal("expected resolved path")
	}
}

func TestPolicyResolveDeniesOutsideRoot(t *testing.T) {
	p := NewPolicy(t.TempDir())
	if _, err := p.Resolve("/etc/hosts"); err == nil {
		t.Fatal("expected denial for path outside root")
	}
}

func TestPolicyResolveDeniesSensitivePatterns(t *testing.T) {
	dir := t.TempDir()
	p := NewPolicy(dir)

	cases := []string{".env", ".env.local", "server.pem", "deploy.key", "id_rsa", ".npmrc"}
	for _, name := range cases {
		if _, err := p.Resolve(filepath.Join(dir, name)); err == nil {
			t.Errorf("expected denial for %q", name)
		}
	}
}

func TestPolicyResolveDeniesSensitiveDir(t *testing.T) {
	dir := t.TempDir()
	p := NewPolicy(dir)
	if _, err := p.Resolve(filepath.Join(dir, ".ssh", "config")); err == nil {
		t.Fatal("expected denial for .ssh directory")
	}
}

func TestRelativePathsResolveAgainstRoot(t *testing.T) {
	// A daemon hosts sessions rooted at different projects in ONE process, so
	// relative tool paths must resolve against the session's root, never the
	// process cwd. Regression: a daemon-session write of "hello.txt" landed in
	// the daemon's cwd, not the project.
	root := t.TempDir()
	p := NewPolicy(root)
	got, err := p.Resolve("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realPath(t, root), "hello.txt")
	if got != want {
		t.Fatalf("relative path resolved to %q, want %q", got, want)
	}
	// Escapes via relative .. still denied.
	if _, err := p.Resolve("../escape.txt"); err == nil {
		t.Fatal("relative escape should be denied")
	}
}

// realPath resolves symlinks the same way NewPolicy does (macOS /tmp etc).
func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

func TestBashRunsInPolicyDir(t *testing.T) {
	root := t.TempDir()
	b := Bash(NewPolicy(root))
	out, err := b.Run(context.Background(), []byte(`{"command":"pwd"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != realPath(t, root) {
		t.Fatalf("bash ran in %q, want %q", strings.TrimSpace(out), root)
	}
}
