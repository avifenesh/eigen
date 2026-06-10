package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConverseSerializesImage(t *testing.T) {
	req := Request{Messages: []Message{{
		Role:   RoleUser,
		Text:   "what is this?",
		Images: []Image{{MediaType: "image/png", Data: []byte("PNGDATA")}},
	}}}
	msgs := converseMessages(req)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("want 1 user msg, got %+v", msgs)
	}
	var hasText, hasImage bool
	for _, c := range msgs[0].Content {
		if c.Text == "what is this?" {
			hasText = true
		}
		if c.Image != nil {
			hasImage = true
			if c.Image.Format != "png" {
				t.Errorf("format = %q, want png", c.Image.Format)
			}
			if string(c.Image.Source.Bytes) != "PNGDATA" {
				t.Errorf("image bytes wrong: %q", c.Image.Source.Bytes)
			}
		}
	}
	if !hasText || !hasImage {
		t.Fatalf("user msg should carry text+image: %+v", msgs[0].Content)
	}
	// []byte marshals to base64 in JSON (the Bedrock wire shape).
	b, _ := json.Marshal(msgs[0])
	if !strings.Contains(string(b), `"bytes":"`) {
		t.Fatalf("image bytes should marshal as base64 string: %s", b)
	}
}

func TestConverseImageFormatMapping(t *testing.T) {
	cases := map[string]string{
		"image/png": "png", "image/jpeg": "jpeg", "image/jpg": "jpeg",
		"image/gif": "gif", "image/webp": "webp", "image/bmp": "",
	}
	for mt, want := range cases {
		if got := converseImageFormat(mt); got != want {
			t.Errorf("converseImageFormat(%q) = %q, want %q", mt, got, want)
		}
	}
}

func TestAnthropicSerializesImage(t *testing.T) {
	req := Request{Messages: []Message{{
		Role:   RoleUser,
		Text:   "describe",
		Images: []Image{{MediaType: "image/jpeg", Data: []byte("JPG")}},
	}}}
	msgs := anthropicMessages(req)
	if len(msgs) != 1 {
		t.Fatalf("want 1 msg, got %d", len(msgs))
	}
	var img *anthropicImageSrc
	for _, c := range msgs[0].Content {
		if c.Type == "image" {
			img = c.Source
		}
	}
	if img == nil {
		t.Fatalf("no image block: %+v", msgs[0].Content)
	}
	if img.Type != "base64" || img.MediaType != "image/jpeg" {
		t.Errorf("image src wrong: %+v", img)
	}
	if img.Data != "SlBH" { // base64("JPG")
		t.Errorf("base64 data = %q, want SlBH", img.Data)
	}
}

func TestUnsupportedImageTypeDropped(t *testing.T) {
	req := Request{Messages: []Message{{
		Role:   RoleUser,
		Text:   "hi",
		Images: []Image{{MediaType: "image/tiff", Data: []byte("x")}},
	}}}
	for _, c := range converseMessages(req)[0].Content {
		if c.Image != nil {
			t.Fatal("unsupported converse image type should be dropped")
		}
	}
	for _, c := range anthropicMessages(req)[0].Content {
		if c.Type == "image" {
			t.Fatal("unsupported anthropic image type should be dropped")
		}
	}
}

func TestHasVision(t *testing.T) {
	if !HasVision("us.anthropic.claude-opus-4-8") {
		t.Error("opus should be vision-capable")
	}
	if HasVision("local") {
		t.Error("local llama is not vision-capable in the catalog")
	}
	if HasVision("nonexistent") {
		t.Error("unknown model should not claim vision")
	}
}

func TestEstimateTokensCountsImages(t *testing.T) {
	base := EstimateTokens([]Message{{Role: RoleUser, Text: "hi"}})
	withImg := EstimateTokens([]Message{{Role: RoleUser, Text: "hi", Images: []Image{{MediaType: "image/png", Data: []byte("x")}}}})
	if withImg <= base+1000 {
		t.Fatalf("image should add ~1200 tokens: base=%d withImg=%d", base, withImg)
	}
}
