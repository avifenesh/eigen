package tool

import (
	"os"
	"path/filepath"
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
