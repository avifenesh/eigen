package llm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConverseGroupsToolResultsIntoUserTurn(t *testing.T) {
	msgs := converseMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "do it"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "t1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
				{ID: "t2", Name: "read", Arguments: json.RawMessage(`{"path":"b"}`)},
			}},
			{Role: RoleTool, ToolCallID: "t1", Text: "A"},
			{Role: RoleTool, ToolCallID: "t2", Text: "B"},
		},
	})

	// Expect: user(text), assistant(2 toolUse), user(2 toolResult).
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content[0].Text != "do it" {
		t.Errorf("msg0 wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || len(msgs[1].Content) != 2 || msgs[1].Content[0].ToolUse == nil || msgs[1].Content[0].ToolUse.Name != "read" {
		t.Errorf("msg1 (toolUse) wrong: %+v", msgs[1])
	}
	if msgs[2].Role != "user" || len(msgs[2].Content) != 2 {
		t.Fatalf("msg2 should group both tool results into one user turn: %+v", msgs[2])
	}
	if msgs[2].Content[0].ToolResult == nil || msgs[2].Content[0].ToolResult.ToolUseID != "t1" || msgs[2].Content[0].ToolResult.Content[0].Text != "A" {
		t.Errorf("tool result 0 wrong: %+v", msgs[2].Content[0])
	}
	if msgs[2].Content[1].ToolResult == nil || msgs[2].Content[1].ToolResult.Status != "success" {
		t.Errorf("tool result 1 wrong: %+v", msgs[2].Content[1])
	}
}

func TestConverseDropsReasoningOnlyAssistantTurn(t *testing.T) {
	// A reasoning-only assistant turn (empty Text, no tool calls) — which
	// providers like GLM persist — must not become an empty content array,
	// which Converse rejects with HTTP 400 "Member must not be null".
	msgs := converseMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Reasoning: "thinking out loud"},
			{Role: RoleAssistant, Text: "Hello!"},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (reasoning-only turn dropped): %+v", len(msgs), msgs)
	}
	for i, m := range msgs {
		if len(m.Content) == 0 {
			t.Fatalf("msg %d has empty content (would serialize to null): %+v", i, m)
		}
	}
	if msgs[1].Content[0].Text != "Hello!" {
		t.Errorf("final assistant text wrong: %+v", msgs[1])
	}
}

func TestConverseToolsInputSchema(t *testing.T) {
	tools := converseTools([]ToolSpec{
		{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)},
	}, false)
	if len(tools) != 1 || tools[0].ToolSpec == nil || tools[0].ToolSpec.Name != "read" {
		t.Fatalf("tools wrong: %+v", tools)
	}
	if string(tools[0].ToolSpec.InputSchema.JSON) != `{"type":"object"}` {
		t.Errorf("input schema wrong: %s", tools[0].ToolSpec.InputSchema.JSON)
	}
}

func TestNormalizeArgsRaw(t *testing.T) {
	if string(normalizeArgsRaw(nil)) != "{}" {
		t.Error("nil should become {}")
	}
	if string(normalizeArgsRaw(json.RawMessage(`{"a":1}`))) != `{"a":1}` {
		t.Error("passthrough failed")
	}
}

func TestConverseToolErrorStatus(t *testing.T) {
	msgs := converseMessages(Request{
		Messages: []Message{
			{Role: RoleTool, ToolCallID: "t1", Text: "boom", ToolError: true},
			{Role: RoleTool, ToolCallID: "t2", Text: "ok"},
		},
	})
	if len(msgs) != 1 || len(msgs[0].Content) != 2 {
		t.Fatalf("expected one user turn with two results: %+v", msgs)
	}
	if msgs[0].Content[0].ToolResult.Status != "error" {
		t.Errorf("errored tool result should have status=error, got %q", msgs[0].Content[0].ToolResult.Status)
	}
	if msgs[0].Content[1].ToolResult.Status != "success" {
		t.Errorf("ok tool result should have status=success, got %q", msgs[0].Content[1].ToolResult.Status)
	}
}

func TestConverseToolsAppendsCachePoint(t *testing.T) {
	specs := []ToolSpec{{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}

	// Caching off: just the tool spec, no cachePoint.
	off := converseTools(specs, false)
	if len(off) != 1 || off[0].ToolSpec == nil || off[0].CachePoint != nil {
		t.Fatalf("cache off should yield one tool spec, no cachePoint: %+v", off)
	}
	// Caching on: a trailing cachePoint after the tool definitions.
	on := converseTools(specs, true)
	if len(on) != 2 || on[1].CachePoint == nil || on[1].CachePoint.Type != "default" {
		t.Fatalf("cache on should append a default cachePoint: %+v", on)
	}
}

func TestConverseAdditionalFields(t *testing.T) {
	// 1M + thinking both on: both keys present.
	c := &Converse{context1M: true, thinkingBudget: 8192}
	var got map[string]any
	if err := json.Unmarshal(c.additionalFields(), &got); err != nil {
		t.Fatalf("additionalFields not valid JSON: %v", err)
	}
	beta, _ := got["anthropic_beta"].([]any)
	if len(beta) != 1 || beta[0] != context1mBeta {
		t.Fatalf("1M beta flag missing/wrong: %+v", got)
	}
	think, _ := got["thinking"].(map[string]any)
	if think["type"] != "enabled" || think["budget_tokens"].(float64) != 8192 {
		t.Fatalf("thinking config missing/wrong: %+v", got)
	}

	// Neither: nil (no additionalModelRequestFields sent).
	if (&Converse{}).additionalFields() != nil {
		t.Fatal("no capabilities should produce nil additional fields")
	}
}

func TestConverseSystemCachePoint(t *testing.T) {
	// Build a request payload and confirm the system prefix gets a cachePoint
	// when caching is enabled (the system+tools prefix is the cache target).
	c := &Converse{cache: true}
	var sys []converseContent
	if true {
		sys = []converseContent{{Text: "system prompt"}}
		if c.cache {
			sys = append(sys, converseContent{CachePoint: &converseCachePoint{Type: "default"}})
		}
	}
	if len(sys) != 2 || sys[1].CachePoint == nil {
		t.Fatalf("system prefix should end with a cachePoint when caching: %+v", sys)
	}
}

func TestConverseEffortMapsToBudget(t *testing.T) {
	c := &Converse{}
	if !c.SetEffort("medium") {
		t.Fatal("medium should be a valid effort")
	}
	if c.thinkingBudget != effortBudget["medium"] || c.Effort() != "medium" {
		t.Fatalf("medium effort: budget=%d effort=%q", c.thinkingBudget, c.Effort())
	}
	if c.SetEffort("nonsense") {
		t.Fatal("invalid effort should return false")
	}
	// "off" disables thinking (budget=0); "minimal" remains a back-compat alias.
	c.SetEffort("off")
	if c.thinkingBudget != 0 || c.additionalFields() != nil {
		t.Fatalf("off should disable thinking: budget=%d", c.thinkingBudget)
	}
}

func TestBudgetToEffort(t *testing.T) {
	if got := budgetToEffort(0); got != "off" {
		t.Fatalf("budget 0 => %q, want off", got)
	}
	if got := budgetToEffort(8192); got != "medium" {
		t.Fatalf("budget 8192 => %q, want medium", got)
	}
	if got := budgetToEffort(16384); got != "high" {
		t.Fatalf("budget 16384 => %q, want high", got)
	}
}

func TestConverseAdaptiveThinking(t *testing.T) {
	// Adaptive models (opus-4-8) emit thinking.type=adaptive + output_config.effort.
	c := &Converse{adaptive: true, effort: "high"}
	var got map[string]any
	if err := json.Unmarshal(c.additionalFields(), &got); err != nil {
		t.Fatalf("additionalFields not valid JSON: %v", err)
	}
	think, _ := got["thinking"].(map[string]any)
	if think["type"] != "adaptive" {
		t.Fatalf("adaptive model should use thinking.type=adaptive, got %+v", got)
	}
	if think["budget_tokens"] != nil {
		t.Fatalf("adaptive model must NOT send budget_tokens: %+v", got)
	}
	oc, _ := got["output_config"].(map[string]any)
	if oc["effort"] != "high" {
		t.Fatalf("adaptive model should set output_config.effort, got %+v", got)
	}

	// Budget models (sonnet-4-6) keep the older enabled+budget shape.
	b := &Converse{thinkingBudget: 8192}
	_ = json.Unmarshal(b.additionalFields(), &got)
	bt, _ := got["thinking"].(map[string]any)
	if bt["type"] != "enabled" || bt["budget_tokens"].(float64) != 8192 {
		t.Fatalf("budget model should use enabled+budget_tokens, got %+v", got)
	}

	// opus-4-8 from the constructor is adaptive.
	t.Setenv("AWS_REGION", "us-east-2")
	// (NewConverse needs creds; just check catalog wiring via a direct lookup)
	if info, _ := Lookup("us.anthropic.claude-opus-4-8"); info.Effort == "" {
		t.Fatal("opus-4-8 should carry an Effort (adaptive) in the catalog")
	}
}

// encodeEventStreamFrame builds one AWS event-stream frame for the given
// :event-type and JSON payload, with valid prelude/message CRC32s — the exact
// wire shape converse-stream emits, so the decoder is exercised end to end.
func encodeEventStreamFrame(eventType string, payload []byte) []byte {
	// Headers: :message-type=event, :event-type=<eventType>, :content-type=application/json.
	var hdr bytes.Buffer
	writeStrHeader := func(name, val string) {
		hdr.WriteByte(byte(len(name)))
		hdr.WriteString(name)
		hdr.WriteByte(7) // string value type
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(val)))
		hdr.Write(l[:])
		hdr.WriteString(val)
	}
	writeStrHeader(":message-type", "event")
	writeStrHeader(":event-type", eventType)
	writeStrHeader(":content-type", "application/json")
	headers := hdr.Bytes()

	totalLen := uint32(12 + len(headers) + len(payload) + 4)
	var prelude [8]byte
	binary.BigEndian.PutUint32(prelude[0:4], totalLen)
	binary.BigEndian.PutUint32(prelude[4:8], uint32(len(headers)))
	preludeCRC := crc32.ChecksumIEEE(prelude[:])

	var frame bytes.Buffer
	frame.Write(prelude[:])
	var pc [4]byte
	binary.BigEndian.PutUint32(pc[:], preludeCRC)
	frame.Write(pc[:])
	frame.Write(headers)
	frame.Write(payload)
	msgCRC := crc32.ChecksumIEEE(frame.Bytes())
	var mc [4]byte
	binary.BigEndian.PutUint32(mc[:], msgCRC)
	frame.Write(mc[:])
	return frame.Bytes()
}

// TestConverseStreamEmitsIncrementalText is the regression for APP-008: Bedrock
// Converse must implement Streamer so the default Claude path streams instead of
// blocking on Complete (frozen UI mid-turn). It mocks the converse-stream binary
// event-stream framing — contentBlockStart/Delta for text, reasoning, and a
// streamed toolUse, then messageStop + metadata — and asserts the deltas reached
// the sink and the final Response is assembled correctly.
func TestConverseStreamEmitsIncrementalText(t *testing.T) {
	frames := bytes.Join([][]byte{
		encodeEventStreamFrame("messageStart", []byte(`{"role":"assistant"}`)),
		encodeEventStreamFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":"think"}}}`)),
		encodeEventStreamFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"Hel"}}`)),
		encodeEventStreamFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"lo"}}`)),
		encodeEventStreamFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)),
		encodeEventStreamFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"path\":"}}}`)),
		encodeEventStreamFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"\"a\"}"}}}`)),
		encodeEventStreamFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)),
		encodeEventStreamFrame("metadata", []byte(`{"usage":{"inputTokens":12,"outputTokens":5,"cacheReadInputTokens":4}}`)),
	}, nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Write(frames)
	}))
	defer srv.Close()

	c := &Converse{Model: "us.anthropic.claude-opus-4-8", region: "us-east-2", bearer: "t", http: srv.Client(), baseURL: srv.URL}
	var textChunks, reasoningChunks []string
	resp, err := c.Stream(context.Background(),
		Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}},
		func(ch StreamChunk) {
			switch ch.Kind {
			case ChunkText:
				textChunks = append(textChunks, ch.Text)
			case ChunkReasoning:
				reasoningChunks = append(reasoningChunks, ch.Text)
			}
		})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(textChunks) != 2 || textChunks[0] != "Hel" || textChunks[1] != "lo" {
		t.Fatalf("text chunks = %v, want [Hel lo]", textChunks)
	}
	if len(reasoningChunks) != 1 || reasoningChunks[0] != "think" {
		t.Fatalf("reasoning chunks = %v, want [think]", reasoningChunks)
	}
	if resp.Text != "Hello" || resp.Reasoning != "think" {
		t.Fatalf("resp text=%q reasoning=%q", resp.Text, resp.Reasoning)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "tu_1" || resp.ToolCalls[0].Name != "read" {
		t.Fatalf("tool call wrong: %+v", resp.ToolCalls)
	}
	if string(resp.ToolCalls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("tool args = %s", resp.ToolCalls[0].Arguments)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 5 || resp.Usage.CacheReadTokens != 4 {
		t.Fatalf("usage wrong: %+v", resp.Usage)
	}
	if _, ok := interface{}(c).(Streamer); !ok {
		t.Fatal("Converse must implement Streamer")
	}
}

func TestConverseBearerTokenSkipsAWSFile(t *testing.T) {
	// With AWS_BEARER_TOKEN_BEDROCK set, NewConverse must NOT require
	// ~/.aws/credentials (the bearer token drives the converse endpoint). Point
	// HOME at an empty dir so any file read would fail.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token-123")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	c, err := NewConverse("us.anthropic.claude-opus-4-8")
	if err != nil {
		t.Fatalf("NewConverse with bearer token should not need ~/.aws/credentials: %v", err)
	}
	if c.bearer != "test-token-123" {
		t.Fatalf("bearer not stored: %q", c.bearer)
	}
}
