package gui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
)

// TestMessageDTORoundTrip verifies the lossy-looking conversions preserve the
// fields the frontend renders: role, text, reasoning, tool calls (raw JSON
// args), tool-result linkage, and base64 image bytes.
func TestMessageDTORoundTrip(t *testing.T) {
	orig := []llm.Message{
		{
			Role:      llm.Role("assistant"),
			Text:      "calling a tool",
			Reasoning: "because reasons",
			ToolCalls: []llm.ToolCall{
				{ID: "tc1", Name: "read", Arguments: json.RawMessage(`{"path":"x.go"}`)},
			},
		},
		{
			Role:       llm.Role("tool"),
			Text:       "file contents",
			ToolCallID: "tc1",
			ToolName:   "read",
			ToolError:  false,
		},
		{
			Role: llm.Role("user"),
			Text: "look at this",
			Images: []llm.Image{
				{MediaType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01}},
			},
		},
	}

	dtos := make([]MessageDTO, 0, len(orig))
	for _, m := range orig {
		dtos = append(dtos, toMessageDTO(m))
	}
	got, err := fromMessageDTOs(dtos)
	if err != nil {
		t.Fatalf("fromMessageDTOs: %v", err)
	}
	if len(got) != len(orig) {
		t.Fatalf("len: got %d want %d", len(got), len(orig))
	}

	// tool-call args survive as raw JSON
	if len(got[0].ToolCalls) != 1 || string(got[0].ToolCalls[0].Arguments) != `{"path":"x.go"}` {
		t.Errorf("tool call args lost: %+v", got[0].ToolCalls)
	}
	if got[0].Reasoning != "because reasons" {
		t.Errorf("reasoning lost: %q", got[0].Reasoning)
	}
	// tool-result linkage
	if got[1].ToolCallID != "tc1" || got[1].ToolName != "read" {
		t.Errorf("tool result linkage lost: %+v", got[1])
	}
	// image bytes survive base64
	if len(got[2].Images) != 1 || !bytes.Equal(got[2].Images[0].Data, orig[2].Images[0].Data) {
		t.Errorf("image bytes lost: %+v", got[2].Images)
	}
	if got[2].Images[0].MediaType != "image/png" {
		t.Errorf("image media type lost: %q", got[2].Images[0].MediaType)
	}
}

// TestImageTooLarge rejects an oversize decoded image rather than handing the
// daemon an unbounded allocation.
func TestImageTooLarge(t *testing.T) {
	big := make([]byte, maxImageBytes+1)
	// base64 of an over-cap payload
	dto := []ImageDTO{{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(big)}}
	if _, err := fromImageDTOs(dto); err == nil {
		t.Fatal("expected error for oversize image, got nil")
	}
}

// TestWireEventDTO maps every event kind's fields without loss.
func TestWireEventDTO(t *testing.T) {
	e := daemon.WireEvent{
		Kind: "tool_result", Step: 3, ToolName: "bash", ToolID: "t9",
		ToolArgs: json.RawMessage(`{"cmd":"ls"}`), Result: "ok", IsError: true,
		InTokens: 10, OutTokens: 20,
	}
	d := toWireEventDTO(e)
	if d.Kind != "tool_result" || d.ToolID != "t9" || d.ToolArgs != `{"cmd":"ls"}` || !d.IsError {
		t.Errorf("wire event mapping lost fields: %+v", d)
	}
}

// TestStopPumpCloseOnce proves the teardown guards are panic-free under the
// concurrent paths that exist in production: Shutdown's loop, Unsubscribe, and
// the watchdog can all race to close the same pump's stop channel + client.
func TestStopPumpCloseOnce(t *testing.T) {
	closes := 0
	var mu sync.Mutex
	p := &sessionPump{id: "s1", stop: make(chan struct{})}
	closeClient := func() {
		mu.Lock()
		closes++
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.stopOnce.Do(func() { close(p.stop) })
			p.closeOnce.Do(closeClient)
		}()
	}
	wg.Wait()

	if closes != 1 {
		t.Fatalf("closeOnce ran %d times, want 1", closes)
	}
	select {
	case <-p.stop:
		// closed exactly once (a second close would have panicked above)
	default:
		t.Fatal("stop channel not closed")
	}
}

// TestSubscribeEmptyID rejects a blank session id before dialing.
func TestSubscribeEmptyID(t *testing.T) {
	b := NewBridge(func() (*daemon.Client, error) { return nil, nil })
	if err := b.Subscribe("  "); err == nil {
		t.Fatal("expected error for empty session id")
	}
	if len(b.pumps) != 0 {
		t.Fatalf("empty subscribe left %d pumps", len(b.pumps))
	}
}
