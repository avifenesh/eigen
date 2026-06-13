package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// screenshotImageBytes caps a screenshot read from disk (8 MiB) so a stray
// large file can't blow up context/memory.
const screenshotImageBytes = 8 << 20

// attachScreenshotPath upgrades a tool result that REFERENCES a screenshot file
// on disk (rather than inlining the image) into one that carries the image, so
// the model can actually see it. The agent-workspace sandbox's
// workspace_screenshot / workspace_observe return a PNG path
// ({"screenshot":{"path":"…png",…}} or {"path":"…png"}), not an inline image
// block — this reads that file and attaches it. No-op when the result already
// has images, the text isn't JSON with a screenshot path, or the file is
// missing/oversized/not an image. Returns the (possibly augmented) result.
func attachScreenshotPath(res ToolResult) ToolResult {
	if len(res.Images) > 0 || res.Text == "" {
		return res
	}
	path := screenshotPathFromJSON(res.Text)
	if path == "" {
		return res
	}
	data, mt, ok := readImageFile(path)
	if !ok {
		return res
	}
	res.Images = append(res.Images, llm.Image{MediaType: mt, Data: data})
	return res
}

// screenshotPathFromJSON pulls a screenshot file path out of a tool's JSON text
// result. It looks for a top-level "path" or a nested {"screenshot":{"path"}},
// only returning paths that end in a known image extension.
func screenshotPathFromJSON(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "{") {
		return ""
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(t), &doc) != nil {
		return ""
	}
	// Nested screenshot object (workspace_screenshot / observe shape).
	if raw, ok := doc["screenshot"]; ok {
		var inner struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &inner) == nil && isImagePath(inner.Path) {
			return inner.Path
		}
	}
	// Top-level "path".
	var top struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(t), &top) == nil && isImagePath(top.Path) {
		return top.Path
	}
	return ""
}

func isImagePath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	}
	return false
}

// readImageFile reads an on-disk image, returning its bytes and media type.
// Returns ok=false when missing, oversized, or unreadable.
func readImageFile(path string) (data []byte, mediaType string, ok bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > screenshotImageBytes {
		return nil, "", false
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil, "", false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		mediaType = "image/png"
	case ".jpg", ".jpeg":
		mediaType = "image/jpeg"
	case ".webp":
		mediaType = "image/webp"
	case ".gif":
		mediaType = "image/gif"
	default:
		return nil, "", false
	}
	return b, mediaType, true
}
