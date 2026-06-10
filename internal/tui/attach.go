package tui

// Image attachment: when a user submits a prompt that references image files
// (typed paths, @mentions, or drag-dropped paths), eigen reads those files and
// attaches them to the turn as vision inputs — but only when the active model
// supports vision. The referenced path tokens stay in the prompt text so the
// model still sees what was attached by name.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// maxImageBytes caps a single attached image (generous for screenshots; guards
// against accidentally inlining a huge file).
const maxImageBytes = 8 << 20 // 8MB

// imageMediaType returns the IANA media type for a path by extension, or "" if
// it is not a supported image.
func imageMediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	return ""
}

// extractImages scans the prompt for tokens that name readable image files and
// loads them. It returns the loaded images and any per-file notes (skipped:
// too big / unreadable). Tokens are whitespace-separated; @-prefixed and
// single-quoted forms are unwrapped. The prompt text is NOT modified.
func extractImages(prompt string) (imgs []llm.Image, notes []string) {
	seen := map[string]bool{}
	for _, tok := range strings.Fields(prompt) {
		p := unwrapToken(tok)
		mt := imageMediaType(p)
		if mt == "" {
			continue
		}
		abs := expandHome(p)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > maxImageBytes {
			notes = append(notes, "image too large, skipped: "+p)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			notes = append(notes, "could not read image: "+p)
			continue
		}
		imgs = append(imgs, llm.Image{MediaType: mt, Data: data})
	}
	return imgs, notes
}

// referencesImage reports whether the prompt names at least one readable image
// file — used to bias routing toward a vision model before the images are
// actually loaded.
func referencesImage(prompt string) bool {
	for _, tok := range strings.Fields(prompt) {
		p := expandHome(unwrapToken(tok))
		if imageMediaType(p) == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// unwrapToken strips a leading @ and surrounding single/double quotes from a
// path token.
func unwrapToken(tok string) string {
	tok = strings.TrimPrefix(tok, "@")
	if len(tok) >= 2 {
		if (tok[0] == '\'' && tok[len(tok)-1] == '\'') || (tok[0] == '"' && tok[len(tok)-1] == '"') {
			tok = tok[1 : len(tok)-1]
		}
	}
	return tok
}

// expandHome resolves a leading ~/ to the user's home directory.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
