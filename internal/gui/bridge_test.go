package gui

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
)

// TestToMessageDTO verifies the read-only history seam (Go->TS) preserves the
// fields the frontend renders: role, text, reasoning, tool calls (raw JSON
// args as a string), tool-result linkage, and base64 image bytes. The seam is
// one-directional — there is no TS->Go message path — so this is the only
// conversion that must hold.
func TestToMessageDTO(t *testing.T) {
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

	got := make([]MessageDTO, 0, len(orig))
	for _, m := range orig {
		got = append(got, toMessageDTO(m))
	}
	if len(got) != len(orig) {
		t.Fatalf("len: got %d want %d", len(got), len(orig))
	}

	// tool-call args survive as raw JSON string
	if len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].Args != `{"path":"x.go"}` {
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
	wantData := base64.StdEncoding.EncodeToString(orig[2].Images[0].Data)
	if len(got[2].Images) != 1 || got[2].Images[0].Data != wantData {
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
	b := NewBridge(func() (*daemon.Client, error) { return nil, nil }, nil, nil)
	if err := b.Subscribe("  "); err == nil {
		t.Fatal("expected error for empty session id")
	}
	if len(b.pumps) != 0 {
		t.Fatalf("empty subscribe left %d pumps", len(b.pumps))
	}
}

// TestLastAssistantText returns the most recent non-empty assistant message —
// the step "answer" RunWorkflow records — skipping trailing tool/user messages.
func TestLastAssistantText(t *testing.T) {
	if got := lastAssistantText(nil); got != "" {
		t.Fatalf("empty history: got %q want \"\"", got)
	}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: "go"},
		{Role: llm.RoleAssistant, Text: "first answer"},
		{Role: llm.RoleAssistant, Text: "second answer"},
		{Role: llm.RoleTool, Text: "tool output"}, // skipped: not assistant
		{Role: llm.RoleAssistant, Text: "   "},    // skipped: blank
	}
	if got := lastAssistantText(msgs); got != "second answer" {
		t.Fatalf("got %q want %q", got, "second answer")
	}
}

// TestWorkflowsListing surfaces authored workflows (name/description/step count)
// and skips an unparseable file, mirroring the bridge's Workflows() guarantee.
func TestWorkflowsListing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wfDir := filepath.Join(home, ".eigen", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := "---\nname: ship\ndescription: ship it\n---\n## build\nbuild the thing\n## test\ntest the thing\n"
	if err := os.WriteFile(filepath.Join(wfDir, "ship.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file with no "## step" sections fails to parse → must be omitted.
	if err := os.WriteFile(filepath.Join(wfDir, "broken.md"), []byte("just prose, no steps\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := NewBridge(func() (*daemon.Client, error) { return nil, nil }, nil, nil)
	got, err := b.Workflows()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d workflows want 1 (broken one must be skipped): %+v", len(got), got)
	}
	if got[0].Name != "ship" || got[0].Description != "ship it" || got[0].Steps != 2 {
		t.Fatalf("workflow info wrong: %+v", got[0])
	}
}

// TestCommandsListing surfaces user-scope custom commands with their frontmatter.
func TestCommandsListing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cmdDir := filepath.Join(home, ".eigen", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ndescription: review the diff\nargument-hint: <target>\n---\nReview $ARGUMENTS\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "review.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	b := NewBridge(func() (*daemon.Client, error) { return nil, nil }, nil, nil)
	got, err := b.Commands()
	if err != nil {
		t.Fatal(err)
	}
	var found *CommandInfoDTO
	for i := range got {
		if got[i].Name == "review" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("review command not listed: %+v", got)
	}
	if found.Description != "review the diff" || found.ArgHint != "<target>" || found.Scope != "user" {
		t.Fatalf("command info wrong: %+v", *found)
	}
}

// TestRunWorkflowAndCommandRequireSession guards the daemon-session ops against a
// blank session id before any RPC.
func TestRunWorkflowAndCommandRequireSession(t *testing.T) {
	b := NewBridge(func() (*daemon.Client, error) { return nil, nil }, nil, nil)
	if _, err := b.RunWorkflow("  ", "ship", nil); err == nil {
		t.Fatal("RunWorkflow: expected error for empty session id")
	}
	if _, err := b.RunCommand("", "review", "x"); err == nil {
		t.Fatal("RunCommand: expected error for empty session id")
	}
}
