package tool

import (
	"os"
	"path/filepath"
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
