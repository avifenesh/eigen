package voice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHaveResolvesPathAndPATH(t *testing.T) {
	if !have("sh") {
		t.Error("sh should resolve on PATH")
	}
	if have("definitely-not-a-real-command-xyz") {
		t.Error("nonexistent command should not resolve")
	}
	// Absolute executable path.
	dir := t.TempDir()
	exe := filepath.Join(dir, "x")
	os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)
	if !have(exe) {
		t.Error("absolute executable should resolve")
	}
	noexe := filepath.Join(dir, "y")
	os.WriteFile(noexe, []byte("x"), 0o644)
	if have(noexe) {
		t.Error("non-executable absolute path should not resolve")
	}
	// Command with args uses the first field.
	if !have("sh -c true") {
		t.Error("first field of a command string should resolve")
	}
}

func TestExpandCmd(t *testing.T) {
	got := expandCmd("rec -d {secs} {out}", map[string]string{"secs": "30", "out": "/tmp/a.wav"})
	want := []string{"rec", "-d", "30", "/tmp/a.wav"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestTTSUnavailableSpeakNoop(t *testing.T) {
	tts := &cmdTTS{command: ""}
	if tts.Available() {
		t.Fatal("empty command should be unavailable")
	}
	// Speak on unavailable is a no-op (nil error).
	if err := tts.Speak(context.Background(), "hi"); err != nil {
		t.Fatalf("unavailable Speak should be nil, got %v", err)
	}
}

func TestSTTAvailabilityGating(t *testing.T) {
	// Missing whisper/model → unavailable, and Listen errors rather than hangs.
	s := &whisperSTT{recordCmd: "arecord {out}", whisperBin: "", model: ""}
	if s.Available() {
		t.Fatal("no whisper/model should be unavailable")
	}
	if _, err := s.Listen(context.Background()); err == nil {
		t.Fatal("unavailable Listen should error")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "", "x", "y") != "x" {
		t.Fatal("should return the first non-empty")
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("all empty → empty")
	}
}
