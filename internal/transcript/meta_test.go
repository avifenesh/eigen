package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "s.eigen.jsonl")

	want := SessionMeta{Provider: "glm", Model: "glm-4.6", Perm: "auto", Effort: "high", Search: "on", Goal: "ship v2", LoopPrompt: "next item", LoopEvery: "10m0s"}
	if err := SaveMeta(sess, want); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	// The sidecar lives next to the session file.
	if _, err := os.Stat(sess + ".meta.json"); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	got, ok := LoadMeta(sess)
	if !ok {
		t.Fatal("LoadMeta reported missing meta")
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestLoadMetaMissing(t *testing.T) {
	if _, ok := LoadMeta(filepath.Join(t.TempDir(), "none.eigen.jsonl")); ok {
		t.Fatal("missing meta should report ok=false")
	}
}

// TestSaveMetaAtomic verifies the atomic write leaves no temp files behind and
// overwrites an existing sidecar cleanly — a crash between turns must never see a
// truncated .meta.json (the failure that silently reset config on resume).
func TestSaveMetaAtomic(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "s.eigen.jsonl")

	// First write, then overwrite (the every-turn case) with new config.
	if err := SaveMeta(sess, SessionMeta{Provider: "glm", Model: "glm-4.6"}); err != nil {
		t.Fatalf("SaveMeta (first): %v", err)
	}
	want := SessionMeta{Provider: "anthropic", Model: "claude-opus-4-8", Perm: "auto"}
	if err := SaveMeta(sess, want); err != nil {
		t.Fatalf("SaveMeta (overwrite): %v", err)
	}

	got, ok := LoadMeta(sess)
	if !ok {
		t.Fatal("LoadMeta reported missing meta after overwrite")
	}
	if got != want {
		t.Fatalf("overwrite mismatch: got %+v want %+v", got, want)
	}

	// No leftover *.tmp files: the rename must have consumed every temp file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
