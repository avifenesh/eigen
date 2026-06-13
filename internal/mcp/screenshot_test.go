package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// tinyPNG is a valid 1x1 PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestAttachScreenshotNestedShape(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "screenshot-1.png")
	os.WriteFile(p, tinyPNG, 0o644)
	res := ToolResult{Text: `{"ok":true,"screenshot":{"path":"` + p + `","width":1,"height":1,"format":"png"}}`}
	out := attachScreenshotPath(res)
	if len(out.Images) != 1 {
		t.Fatalf("nested screenshot path should attach 1 image, got %d", len(out.Images))
	}
	if out.Images[0].MediaType != "image/png" || len(out.Images[0].Data) != len(tinyPNG) {
		t.Fatalf("image not read correctly: %+v", out.Images[0])
	}
}

func TestAttachScreenshotTopLevelPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shot.jpeg")
	os.WriteFile(p, tinyPNG, 0o644) // bytes don't matter, ext drives media type
	res := ToolResult{Text: `{"path":"` + p + `"}`}
	out := attachScreenshotPath(res)
	if len(out.Images) != 1 || out.Images[0].MediaType != "image/jpeg" {
		t.Fatalf("top-level path should attach jpeg: %+v", out.Images)
	}
}

func TestAttachScreenshotNoopCases(t *testing.T) {
	// Already has an image → untouched.
	withImg := ToolResult{Text: `{"path":"x.png"}`, Images: []llm.Image{{MediaType: "image/png", Data: []byte("x")}}}
	if got := attachScreenshotPath(withImg); len(got.Images) != 1 {
		t.Fatal("should not double-attach")
	}
	// Non-JSON text.
	if got := attachScreenshotPath(ToolResult{Text: "just text"}); len(got.Images) != 0 {
		t.Fatal("plain text must not attach")
	}
	// Path that isn't an image extension.
	if got := attachScreenshotPath(ToolResult{Text: `{"path":"/tmp/data.json"}`}); len(got.Images) != 0 {
		t.Fatal("non-image path must not attach")
	}
	// Missing file.
	if got := attachScreenshotPath(ToolResult{Text: `{"path":"/nonexistent/x.png"}`}); len(got.Images) != 0 {
		t.Fatal("missing file must not attach")
	}
}
