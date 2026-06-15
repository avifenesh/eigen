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

// --- AddRoot (the user-invoked /add-dir grant) ---

func TestAddRootAllowsPathsInTheAddedDir(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "lib.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewPolicy(primary)
	// Before adding: a file in extra is outside all roots.
	if _, err := p.Resolve(filepath.Join(extra, "lib.go")); err == nil {
		t.Fatal("file in a non-root dir must be denied before AddRoot")
	}
	got, err := p.AddRoot(extra)
	if err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	// AddRoot returns the symlink-resolved root; resolve through it for compare.
	if want := mustEval(t, extra); got != want {
		t.Fatalf("AddRoot returned %q, want %q", got, want)
	}
	// After adding: the file resolves.
	if _, err := p.Resolve(filepath.Join(extra, "lib.go")); err != nil {
		t.Fatalf("file in the added dir should resolve: %v", err)
	}
	// The primary root is unchanged (bash cwd / relative base).
	if p.Dir() != mustEval(t, primary) {
		t.Fatalf("primary root changed after AddRoot: %s", p.Dir())
	}
}

func TestAddRootStillDeniesSecretsAndDotGitWithinIt(t *testing.T) {
	extra := t.TempDir()
	if _, err := NewPolicy(t.TempDir()).AddRoot(extra); err != nil {
		// just exercising AddRoot validity
	}
	p := NewPolicy(t.TempDir())
	if _, err := p.AddRoot(extra); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	// Per-path denials still apply inside an added root.
	for _, bad := range []string{
		filepath.Join(extra, ".env"),
		filepath.Join(extra, "id_rsa"),
		filepath.Join(extra, "sub", ".git", "config"),
		filepath.Join(extra, ".ssh", "key"),
	} {
		if _, err := p.Resolve(bad); err == nil {
			t.Errorf("%s should still be denied inside an added root", bad)
		}
	}
}

func TestAddRootRejectsNonexistentAndDeniedDirs(t *testing.T) {
	p := NewPolicy(t.TempDir())
	if _, err := p.AddRoot(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("AddRoot must reject a non-existent dir")
	}
	// A file (not a dir) is rejected.
	f := filepath.Join(t.TempDir(), "afile")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if _, err := p.AddRoot(f); err == nil {
		t.Error("AddRoot must reject a non-directory")
	}
	// A dir sitting under a denied segment (e.g. ~/.ssh) is rejected.
	ssh := filepath.Join(t.TempDir(), ".ssh")
	_ = os.MkdirAll(ssh, 0o755)
	if _, err := p.AddRoot(ssh); err == nil {
		t.Error("AddRoot must reject a denied dir (.ssh)")
	}
}

func TestAddRootIsIdempotentAndPrefixSafe(t *testing.T) {
	base := t.TempDir()
	foo := filepath.Join(base, "foo")
	foobar := filepath.Join(base, "foobar")
	_ = os.MkdirAll(foo, 0o755)
	_ = os.MkdirAll(foobar, 0o755)
	_ = os.WriteFile(filepath.Join(foobar, "f.txt"), []byte("x"), 0o644)
	p := NewPolicy(t.TempDir())
	if _, err := p.AddRoot(foo); err != nil {
		t.Fatal(err)
	}
	// Idempotent: adding the same root twice keeps a single entry.
	if _, err := p.AddRoot(foo); err != nil {
		t.Fatal(err)
	}
	if n := len(p.Roots()); n != 2 { // primary + foo
		t.Fatalf("idempotent AddRoot should keep 2 roots, got %d", n)
	}
	// Prefix safety: adding /foo must NOT admit /foobar.
	if _, err := p.Resolve(filepath.Join(foobar, "f.txt")); err == nil {
		t.Fatal("/foo as a root must not admit /foobar (prefix bug)")
	}
}

func TestAddRootConcurrentWithResolve(t *testing.T) {
	// -race guard: a tool goroutine calling Resolve while the user grants a
	// root must not race on Policy.roots.
	primary := t.TempDir()
	p := NewPolicy(primary)
	dirs := make([]string, 8)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			_, _ = p.Resolve("x.txt")
			_ = p.Roots()
			_ = p.Dir()
		}
		close(done)
	}()
	for _, d := range dirs {
		_, _ = p.AddRoot(d)
	}
	<-done
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(p)
}
