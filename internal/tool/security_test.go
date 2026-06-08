package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGrepDoesNotLeakSecrets is the regression for the Opus review C1: grep/glob
// must never return the contents or names of denied files even though ripgrep
// recurses freely.
func TestGrepDoesNotLeakSecrets(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	secret := "SUPERSECRETTOKEN12345"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("// references "+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := Grep(NewPolicy(dir))
	out, err := g.Run(context.Background(), mustArgs(t, map[string]any{"pattern": secret, "path": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, ".env") {
		t.Fatalf("grep leaked the .env file:\n%s", out)
	}
	if !strings.Contains(out, "app.go") {
		t.Fatalf("grep should still find non-secret matches, got:\n%s", out)
	}
}

func TestGlobDoesNotListSecrets(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "key.pem"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("x"), 0o644)

	gl := Glob(NewPolicy(dir))
	out, err := gl.Run(context.Background(), mustArgs(t, map[string]any{"pattern": "**/*", "path": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, ".env") || strings.Contains(out, "key.pem") {
		t.Fatalf("glob listed sensitive files:\n%s", out)
	}
}

func TestTruncateUTF8NoSplit(t *testing.T) {
	s := strings.Repeat("é", 100) // 2 bytes each
	got := TruncateUTF8(s, 51)    // 51 is mid-rune
	if len(got) > 51 {
		t.Fatalf("exceeded max: %d", len(got))
	}
	if !strings.HasPrefix(s, got) || len(got)%2 != 0 {
		t.Fatalf("split a rune: len=%d", len(got))
	}
}
