package clipboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyPipesToCommand(t *testing.T) {
	out := filepath.Join(t.TempDir(), "clip.txt")
	t.Setenv("EIGEN_CLIPBOARD_CMD", "cat > "+out)
	c := Detect()
	if !c.Available() {
		t.Fatal("EIGEN_CLIPBOARD_CMD should make the copier available")
	}
	if err := c.Copy("copied text"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "copied text" {
		t.Fatalf("clipboard command should receive text on stdin, got %q", b)
	}
}

func TestUnavailableCopierIsNoop(t *testing.T) {
	c := &Copier{}
	if c.Available() {
		t.Fatal("zero copier should be unavailable")
	}
	if err := c.Copy("x"); err != nil {
		t.Fatalf("unavailable copy should be a no-op, got %v", err)
	}
}
