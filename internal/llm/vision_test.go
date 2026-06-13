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

// --- tool-result images (browser / computer-use screenshots) ---------------

func toolImgReq() Request {
	// assistant calls a tool, the tool returns text + a screenshot.
	return Request{Messages: []Message{
		{Role: RoleUser, Text: "screenshot the page"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "screenshot"}}},
		{Role: RoleTool, ToolCallID: "c1", Text: "captured", Images: []Image{{MediaType: "image/png", Data: []byte("SHOT")}}},
	}}
}

func TestConverseToolResultCarriesImage(t *testing.T) {
	msgs := converseMessages(toolImgReq())
	// Find the toolResult block; it must contain a text AND an image block.
	var tr *converseToolResult
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.ToolResult != nil {
				tr = c.ToolResult
			}
		}
	}
	if tr == nil {
		t.Fatal("no toolResult block")
	}
	var hasText, hasImage bool
	for _, c := range tr.Content {
		if c.Text == "captured" {
			hasText = true
		}
		if c.Image != nil && string(c.Image.Source.Bytes) == "SHOT" {
			hasImage = true
		}
	}
	if !hasText || !hasImage {
		t.Fatalf("toolResult should carry text+image: %+v", tr.Content)
	}
}

func TestAnthropicToolResultCarriesImage(t *testing.T) {
	msgs := anthropicMessages(toolImgReq())
	var tr *anthropicContent
	for i := range msgs {
		for j := range msgs[i].Content {
			if msgs[i].Content[j].Type == "tool_result" {
				tr = &msgs[i].Content[j]
			}
		}
	}
	if tr == nil {
		t.Fatal("no tool_result block")
	}
	blocks, ok := tr.Content.([]anthropicContent)
	if !ok {
		t.Fatalf("tool_result content should be a block array when images present, got %T", tr.Content)
	}
	var hasText, hasImage bool
	for _, b := range blocks {
		if b.Type == "text" && b.Text == "captured" {
			hasText = true
		}
		if b.Type == "image" && b.Source != nil {
			hasImage = true
		}
	}
	if !hasText || !hasImage {
		t.Fatalf("tool_result should carry text+image: %+v", blocks)
	}
}

func TestMantleToolResultImageAsSyntheticUserMessage(t *testing.T) {
	items := buildInput(toolImgReq())
	// Ordering: function_call_output (text) must come BEFORE the synthetic
	// user image message.
	var outIdx, userImgIdx = -1, -1
	for i, it := range items {
		if it.Type == "function_call_output" {
			outIdx = i
		}
		if it.Role == "user" && userImgIdx == -1 && i > 0 && strings.Contains(string(it.Content), "input_image") {
			userImgIdx = i
		}
	}
	if outIdx < 0 {
		t.Fatal("no function_call_output")
	}
	if userImgIdx < 0 {
		t.Fatal("tool-result image should appear as a synthetic user input_image message")
	}
	if !(outIdx < userImgIdx) {
		t.Fatalf("function_call_output (%d) must precede the synthetic image user msg (%d)", outIdx, userImgIdx)
	}
}

func TestChatToolResultImageAsSyntheticUserMessage(t *testing.T) {
	msgs := chatMessagesFrom(toolImgReq())
	var toolIdx, userImgIdx = -1, -1
	for i, m := range msgs {
		if m.Role == "tool" {
			toolIdx = i
		}
		if m.Role == "user" {
			if parts, ok := m.Content.([]chatPart); ok {
				for _, p := range parts {
					if p.Type == "image_url" {
						userImgIdx = i
					}
				}
			}
		}
	}
	if toolIdx < 0 || userImgIdx < 0 {
		t.Fatalf("want a tool msg and a synthetic user-image msg, got tool=%d img=%d", toolIdx, userImgIdx)
	}
	if !(toolIdx < userImgIdx) {
		t.Fatalf("tool result (%d) must precede the synthetic image user msg (%d)", toolIdx, userImgIdx)
	}
}
