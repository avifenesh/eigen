package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractImagesLoadsReferenced(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nfakebody"), 0o644); err != nil {
		t.Fatal(err)
	}
	jpg := filepath.Join(dir, "photo.jpg")
	os.WriteFile(jpg, []byte("jpegbody"), 0o644)

	prompt := "compare " + png + " and '" + jpg + "' please"
	imgs, notes := extractImages(prompt)
	if len(imgs) != 2 {
		t.Fatalf("want 2 images, got %d (notes: %v)", len(imgs), notes)
	}
	if imgs[0].MediaType != "image/png" || imgs[1].MediaType != "image/jpeg" {
		t.Fatalf("media types wrong: %s %s", imgs[0].MediaType, imgs[1].MediaType)
	}
	if string(imgs[1].Data) != "jpegbody" {
		t.Fatalf("jpg data wrong: %q", imgs[1].Data)
	}
}

func TestExtractImagesIgnoresNonImages(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.md")
	os.WriteFile(txt, []byte("hi"), 0o644)
	imgs, _ := extractImages("read " + txt + " and relative/x.png and /no/such/file.png")
	if len(imgs) != 0 {
		t.Fatalf("non-image + missing files should yield nothing, got %d", len(imgs))
	}
}

func TestExtractImagesTooLarge(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "huge.png")
	os.WriteFile(big, make([]byte, maxImageBytes+1), 0o644)
	imgs, notes := extractImages(big)
	if len(imgs) != 0 {
		t.Fatal("oversized image should be skipped")
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "too large") {
		t.Fatalf("should note the skip: %v", notes)
	}
}

func TestExtractImagesDedupes(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")
	os.WriteFile(png, []byte("x"), 0o644)
	imgs, _ := extractImages(png + " " + png)
	if len(imgs) != 1 {
		t.Fatalf("same file twice should load once, got %d", len(imgs))
	}
}

func TestImageMediaType(t *testing.T) {
	cases := map[string]string{
		"a.png": "image/png", "a.PNG": "image/png", "a.jpg": "image/jpeg",
		"a.jpeg": "image/jpeg", "a.webp": "image/webp", "a.gif": "image/gif",
		"a.txt": "", "a": "",
	}
	for in, want := range cases {
		if got := imageMediaType(in); got != want {
			t.Errorf("imageMediaType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPasteImageNonVisionModel(t *testing.T) {
	m := testModel(t)
	m.modelID = "local" // no vision
	m.pasteImage()
	if len(m.pendingImages) != 0 {
		t.Fatal("non-vision model must not stage images")
	}
	found := false
	for _, b := range m.blocks {
		if strings.Contains(b.body, "no vision support") {
			found = true
		}
	}
	if !found {
		t.Fatal("should note the model lacks vision")
	}
}
