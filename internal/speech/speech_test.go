package speech

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpeakPipesTextToCommand(t *testing.T) {
	out := filepath.Join(t.TempDir(), "spoken.txt")
	t.Setenv("EIGEN_TTS_CMD", "cat > "+out)
	s := Detect()
	if !s.Available() {
		t.Fatal("EIGEN_TTS_CMD should make the speaker available")
	}
	if err := s.speak("hello world"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "hello world" {
		t.Fatalf("command should receive text on stdin, got %q", b)
	}
}

func TestEmptyTextIsNoop(t *testing.T) {
	t.Setenv("EIGEN_TTS_CMD", "cat")
	s := Detect()
	s.Speak("")    // must not panic
	s.Speak("   ") // whitespace-only: no-op
}

func TestZeroSpeakerUnavailable(t *testing.T) {
	s := &Speaker{}
	if s.Available() {
		t.Fatal("zero speaker should be unavailable")
	}
	s.Speak("hi") // no-op
	if err := s.speak("hi"); err != nil {
		t.Fatalf("unavailable speak should be a no-op, got %v", err)
	}
}
