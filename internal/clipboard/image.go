package clipboard

import (
	"fmt"
	"os/exec"
)

// ImageData is raw clipboard image bytes plus their media type.
type ImageData struct {
	MediaType string
	Bytes     []byte
}

// imageMIMEs are the clipboard image types we try, in preference order.
var imageMIMEs = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

// PasteImage reads an image from the clipboard, or (nil, nil) when the
// clipboard holds no image / no tool supports image paste. Uses the same
// backend family as text paste: wl-paste -t <mime>, xclip -t <mime> -o, or
// pngpaste (macOS). Best-effort and side-effect-free.
func PasteImage() (*ImageData, error) {
	// Wayland: wl-paste can list and emit a specific MIME type.
	if p, err := exec.LookPath("wl-paste"); err == nil {
		types, _ := exec.Command(p, "--list-types").Output()
		for _, mime := range imageMIMEs {
			if !containsLine(types, mime) {
				continue
			}
			out, err := exec.Command(p, "--no-newline", "-t", mime).Output()
			if err == nil && len(out) > 0 {
				return &ImageData{MediaType: mime, Bytes: out}, nil
			}
		}
		return nil, nil
	}
	// X11: xclip can request a target MIME type.
	if p, err := exec.LookPath("xclip"); err == nil {
		for _, mime := range imageMIMEs {
			out, err := exec.Command(p, "-selection", "clipboard", "-t", mime, "-o").Output()
			if err == nil && len(out) > 0 {
				return &ImageData{MediaType: mime, Bytes: out}, nil
			}
		}
		return nil, nil
	}
	// macOS: pngpaste emits PNG from the clipboard if present.
	if p, err := exec.LookPath("pngpaste"); err == nil {
		out, err := exec.Command(p, "-").Output()
		if err == nil && len(out) > 0 {
			return &ImageData{MediaType: "image/png", Bytes: out}, nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("no clipboard image tool (need wl-paste, xclip, or pngpaste)")
}

// containsLine reports whether data has a line exactly equal to s.
func containsLine(data []byte, s string) bool {
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[start:i]
			if string(line) == s {
				return true
			}
			start = i + 1
		}
	}
	return false
}
