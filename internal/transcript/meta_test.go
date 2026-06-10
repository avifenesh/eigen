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
