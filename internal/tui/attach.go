package tui

// Image attachment: when a user submits a prompt that references image files
// (typed paths, @mentions, or drag-dropped paths), eigen reads those files and
// attaches them to the turn as vision inputs — but only when the active model
// supports vision. The referenced path tokens stay in the prompt text so the
// model still sees what was attached by name.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/clipboard"
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
	for _, tok := range promptTokens(prompt) {
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
	for _, tok := range promptTokens(prompt) {
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

// promptTokens splits a prompt into tokens, keeping single- or double-quoted
// spans together (so a quoted path with spaces — what normalizeDropped emits
// for a dropped "My Screenshot.png" — stays one token instead of being split
// on the space by strings.Fields).
func promptTokens(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case (r == '\'' || r == '"') && cur.Len() == 0:
			quote = r
			cur.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
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

// pasteImage grabs an image from the system clipboard and stages it for the
// next message. Noted to the user; no-op with a hint when the clipboard has no
// image or the model can't see images.
func (m *model) pasteImage() {
	// Fail OPEN on unknown models: only a POSITIVE "blind" verdict refuses —
	// an uncataloged model gets the paste and the backend's real answer.
	if has, known := llm.Vision(m.modelID); known && !has {
		m.note("image paste: the active model has no vision support (switch with /model)")
		return
	}
	img, err := clipboard.PasteImage()
	if err != nil {
		m.note("image paste: " + err.Error())
		return
	}
	if img == nil {
		m.note("image paste: no image in the clipboard")
		return
	}
	if len(img.Bytes) > maxImageBytes {
		m.note("image paste: too large")
		return
	}
	m.pendingImages = append(m.pendingImages, llm.Image{MediaType: img.MediaType, Data: img.Bytes})
	m.note(fmt.Sprintf("staged %s image (%d KB) for your next message", img.MediaType, len(img.Bytes)/1024))
}
