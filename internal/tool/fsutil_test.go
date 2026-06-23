package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWritePreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Reassert the mode in case umask masked bits at creation time.
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := atomicWrite(path, []byte("#!/bin/sh\necho new\n")); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o755 {
		t.Fatalf("mode not preserved: got %o, want 0755", got)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "#!/bin/sh\necho new\n" {
		t.Fatalf("content not updated: %q", got)
	}
}

func TestAtomicWriteNewFileDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")
	if err := atomicWrite(path, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// New files default to 0o644 (subject to the process umask), never 0o600.
	if got := fi.Mode().Perm(); got&0o044 == 0 {
		t.Fatalf("new file should be readable beyond owner: got %o", got)
	}
}

func TestRunRipgrepInvalidRegexIsError(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "(" is an unclosed group; rg exits 2 and prints a parse error.
	out, code, err := runRipgrep(context.Background(), "--", "(", dir)
	if err == nil {
		t.Fatalf("invalid regex should be an error, got out=%q code=%d", out, code)
	}
	if code < 2 {
		t.Fatalf("invalid regex should report exit code >= 2, got %d", code)
	}
	if out != "" {
		t.Fatalf("error case should not return rg output as result, got %q", out)
	}
	if !strings.Contains(err.Error(), "ripgrep failed") {
		t.Fatalf("error should mention ripgrep failure, got %v", err)
	}
}

func TestRunRipgrepNoMatchIsSuccess(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid pattern with no matches: rg exits 1, which is not an error.
	out, code, err := runRipgrep(context.Background(), "--", "nonexistent_pattern_xyz", dir)
	if err != nil {
		t.Fatalf("no-match should not be an error: %v", err)
	}
	if code != 1 {
		t.Fatalf("no-match should report exit code 1, got %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("no-match should return empty output, got %q", out)
	}
}
