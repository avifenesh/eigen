package docs_test

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
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
	wantSurfaces := map[string]bool{"chat": true, "changes": true, "tools": true, "shells": true, "approvals": true, "memory": true, "plugins": true, "config": true}
	if len(manifest.Captures) != len(wantSurfaces) {
		t.Fatalf("captures=%d want %d", len(manifest.Captures), len(wantSurfaces))
	}
	seenHashes := map[string]string{}
	metric := map[string]visualMetric{}
	for _, c := range manifest.Captures {
		if !wantSurfaces[c.Surface] {
			t.Fatalf("unexpected real Wails capture %q", c.Surface)
		}
		delete(wantSurfaces, c.Surface)
		p := filepath.Clean(filepath.Join("..", c.Screenshot))
		blob, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s screenshot missing: %v", c.Surface, err)
		}
		if len(blob) < 120000 {
			t.Fatalf("%s screenshot too small for a real Wails workspace capture: %d", c.Surface, len(blob))
		}
		hash := fmt.Sprintf("%x", md5.Sum(blob))
		if other, ok := seenHashes[hash]; ok {
			t.Fatalf("%s screenshot duplicates %s byte-for-byte", c.Surface, other)
		}
		seenHashes[hash] = c.Surface
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
		m := measureVisualMetric(img, 6000)
		metric[c.Surface] = m
		if m.Colors < 1000 {
			t.Fatalf("%s screenshot has too little visual detail/color variance: %d", c.Surface, m.Colors)
		}
		if m.DominantShare > 0.40 {
			t.Fatalf("%s screenshot is too visually flat: dominant sampled color %.1f%%", c.Surface, m.DominantShare*100)
		}
		if m.NonBackgroundRows < 620 {
			t.Fatalf("%s screenshot has too little vertical UI structure: %d rows", c.Surface, m.NonBackgroundRows)
		}
		if m.NonBackgroundCols < 760 {
			t.Fatalf("%s screenshot has too little horizontal UI structure: %d cols", c.Surface, m.NonBackgroundCols)
		}
	}
	if len(wantSurfaces) != 0 {
		t.Fatalf("missing real Wails captures: %+v", wantSurfaces)
	}
	if metric["plugins"].Hash == metric["config"].Hash {
		t.Fatalf("plugins and config captures must be distinct real surfaces")
	}
}

type visualMetric struct {
	Colors            int
	DominantShare     float64
	NonBackgroundRows int
	NonBackgroundCols int
	Hash              string
}

func measureVisualMetric(img interface {
	Bounds() image.Rectangle
	At(x, y int) color.Color
}, limit int) visualMetric {
	seen := map[uint32]int{}
	b := img.Bounds()
	step := 4
	total := 0
	rows := map[int]bool{}
	cols := map[int]bool{}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			key := uint32(r>>8)<<24 | uint32(g>>8)<<16 | uint32(bl>>8)<<8 | uint32(a>>8)
			seen[key]++
			total++
			if !nearBackground(r>>8, g>>8, bl>>8) {
				rows[y] = true
				cols[x] = true
			}
		}
	}
	maxCount := 0
	for _, n := range seen {
		if n > maxCount {
			maxCount = n
		}
	}
	colors := len(seen)
	if colors > limit {
		colors = limit
	}
	return visualMetric{
		Colors:            colors,
		DominantShare:     float64(maxCount) / float64(total),
		NonBackgroundRows: len(rows) * step,
		NonBackgroundCols: len(cols) * step,
		Hash:              fmt.Sprintf("%v", seen),
	}
}

func nearBackground(r, g, b uint32) bool {
	return r < 18 && g < 24 && b < 24
}
