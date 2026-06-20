package docs_test

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestGUIScreenshotArtifactsDocumentVisualAssertions(t *testing.T) {
	b, err := os.ReadFile("gui-screenshot-artifacts.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"docs/artifacts/gui/release-app-shell.png",
		"docs/artifacts/gui/chat-tui-shell.png",
		"agent-workspace:screenshot-release",
		"[screensho1:eigen-release*]",
		"agent-workspace:screenshot-chat",
		"[screensho1:eigen-smoke.test*]",
		"Premium app shell is visible",
		"Sidebar includes home, live, projects, machines, sessions, config, skills, models, providers, memory, crons, plugins",
		"Premium chat shell is visible",
		"agent-workspace:screenshot-chat",
		"[screensho1:eigen-smoke.test*]",
		"Right panel includes tabs `[chg] [git] [trm] [tsk] [nt] [X]`",
		"scripts/verify-gui-phase.sh",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("screenshot artifact manifest missing %q", want)
		}
	}
}

func TestGUIScreenshotArtifactsExistAndArePNGs(t *testing.T) {
	pngHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	for _, path := range []string{
		"artifacts/gui/release-app-shell.png",
		"artifacts/gui/chat-tui-shell.png",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing screenshot artifact %s: %v", path, err)
		}
		if len(b) < 1024 {
			t.Fatalf("screenshot artifact %s is unexpectedly small: %d bytes", path, len(b))
		}
		if !bytes.HasPrefix(b, pngHeader) {
			t.Fatalf("screenshot artifact %s is not a PNG", path)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("screenshot artifact %s failed PNG decode: %v", path, err)
		}
		bounds := img.Bounds()
		if bounds.Dx() < 800 || bounds.Dy() < 500 {
			t.Fatalf("screenshot artifact %s dimensions too small: %dx%d", path, bounds.Dx(), bounds.Dy())
		}
	}
}
