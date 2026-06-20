package docs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGUINativeVisualArtifactBundle(t *testing.T) {
	b, err := os.ReadFile("artifacts/gui/native/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Surfaces []struct {
			Surface      string   `json:"surface"`
			HTML         string   `json:"html"`
			Screenshot   string   `json:"screenshot"`
			RequiredText []string `json:"required_text"`
		} `json:"surfaces"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"chat": true, "changes": true, "tools": true, "shells": true, "approvals": true, "memory": true, "plugins": true, "config": true}
	if len(manifest.Surfaces) != len(want) {
		t.Fatalf("artifact surfaces=%d want %d", len(manifest.Surfaces), len(want))
	}
	for _, s := range manifest.Surfaces {
		if !want[s.Surface] {
			t.Fatalf("unexpected visual surface %q", s.Surface)
		}
		delete(want, s.Surface)
		htmlPath := filepath.Clean(filepath.Join("..", s.HTML))
		pngPath := filepath.Clean(filepath.Join("..", s.Screenshot))
		htmlBytes, err := os.ReadFile(htmlPath)
		if err != nil {
			t.Fatalf("%s html missing: %v", s.Surface, err)
		}
		info, err := os.Stat(pngPath)
		if err != nil {
			t.Fatalf("%s screenshot missing: %v", s.Surface, err)
		}
		if info.Size() < 20000 {
			t.Fatalf("%s screenshot too small to be a real desktop artifact: %d", s.Surface, info.Size())
		}
		htmlText := string(htmlBytes)
		for _, token := range s.RequiredText {
			if token == "" || !containsString(htmlText, token) {
				t.Fatalf("%s artifact html missing required token %q", s.Surface, token)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing visual surfaces: %+v", want)
	}
}

func containsString(s, sub string) bool {
	return len(sub) == 0 || (len(sub) <= len(s) && (s == sub || containsStringAt(s, sub)))
}

func containsStringAt(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
