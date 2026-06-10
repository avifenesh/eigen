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

func TestPromptTokensQuoteAware(t *testing.T) {
	// A quoted path with spaces stays one token (the drag-drop case).
	got := promptTokens("'/home/u/my pics/a.png'")
	if len(got) != 1 || got[0] != "'/home/u/my pics/a.png'" {
		t.Fatalf("quoted spaced path should be one token, got %#v", got)
	}
	// Ordinary words split normally.
	got = promptTokens("look at /tmp/a.png please")
	if len(got) != 4 {
		t.Fatalf("plain words should split, got %#v", got)
	}
	// A mid-word apostrophe does not start a quote span.
	got = promptTokens("don't break this")
	if len(got) != 3 || got[0] != "don't" {
		t.Fatalf("mid-word apostrophe should not quote, got %#v", got)
	}
	// Double quotes too.
	got = promptTokens(`read "/tmp/My File.png" now`)
	if len(got) != 3 || got[1] != `"/tmp/My File.png"` {
		t.Fatalf("double-quoted span should stay together, got %#v", got)
	}
}

func TestDroppedSpacedImageAttaches(t *testing.T) {
	dir := t.TempDir() + "/my pics"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := dir + "/shot.png"
	if err := os.WriteFile(p, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}
	// A dropped path with spaces is quoted by normalizeDropped; it must still
	// be detected and attached (regression: strings.Fields split it apart).
	dropped := normalizeDropped(p)
	if !referencesImage(dropped) {
		t.Fatalf("spaced dropped image not detected from %q", dropped)
	}
	imgs, notes := extractImages(dropped)
	if len(imgs) != 1 {
		t.Fatalf("want 1 attached image, got %d (notes=%v)", len(imgs), notes)
	}
}
