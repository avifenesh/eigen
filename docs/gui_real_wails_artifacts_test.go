package docs_test

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGUIRealWailsArtifactBundle(t *testing.T) {
	b, err := os.ReadFile("artifacts/gui/real-wails/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Source                  string `json:"source"`
		LaunchCommand           string `json:"launch_command"`
		WindowTitle             string `json:"window_title"`
		WindowClass             string `json:"window_class"`
		WindowGeometry          string `json:"window_geometry"`
		WorkspaceScreenshotSize string `json:"workspace_screenshot_size"`
		Captures                []struct {
			Surface    string `json:"surface"`
			Screenshot string `json:"screenshot"`
		} `json:"captures"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	joined := manifest.Source + manifest.LaunchCommand + manifest.WindowTitle + manifest.WindowClass + manifest.WindowGeometry + manifest.WorkspaceScreenshotSize
	for _, want := range []string{
		"actual running Wails app",
		"go run -tags 'wails production webkit2_41' . --instance dev gui",
		"Eigen",
		"1320x900",
		"1320x900",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("real Wails manifest missing provenance token %q", want)
		}
	}
	wantSurfaces := map[string]bool{"chat": true, "changes": true, "tools": true, "shells": true, "approvals": true, "memory": true}
	if len(manifest.Captures) != len(wantSurfaces) {
		t.Fatalf("captures=%d want %d", len(manifest.Captures), len(wantSurfaces))
	}
	for _, c := range manifest.Captures {
		if !wantSurfaces[c.Surface] {
			t.Fatalf("unexpected real Wails capture %q", c.Surface)
		}
		delete(wantSurfaces, c.Surface)
		p := filepath.Clean(filepath.Join("..", c.Screenshot))
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s screenshot missing: %v", c.Surface, err)
		}
		if info.Size() < 120000 {
			t.Fatalf("%s screenshot too small for a real Wails workspace capture: %d", c.Surface, info.Size())
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s screenshot is not a PNG: %v", c.Surface, err)
		}
		if got := img.Bounds().Dx(); got != 1320 {
			t.Fatalf("%s width=%d want 1320", c.Surface, got)
		}
		if got := img.Bounds().Dy(); got != 900 {
			t.Fatalf("%s height=%d want 900", c.Surface, got)
		}
		if countPNGColors(img, 6000) < 1000 {
			t.Fatalf("%s screenshot has too little visual detail/color variance", c.Surface)
		}
	}
	if len(wantSurfaces) != 0 {
		t.Fatalf("missing real Wails captures: %+v", wantSurfaces)
	}
}

func countPNGColors(img interface {
	Bounds() image.Rectangle
	At(x, y int) color.Color
}, limit int) int {
	seen := map[uint32]struct{}{}
	b := img.Bounds()
	step := 4
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			key := uint32(r>>8)<<24 | uint32(g>>8)<<16 | uint32(bl>>8)<<8 | uint32(a>>8)
			seen[key] = struct{}{}
			if len(seen) >= limit {
				return len(seen)
			}
		}
	}
	return len(seen)
}
