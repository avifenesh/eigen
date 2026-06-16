package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

// TestRegistryCompactsSchema: a pretty-printed schema literal is normalized to
// canonical compact form ONCE at registration, so the prompt/data plane never
// carries indentation/newlines to the model. Pretty-printing is render-time
// only (internal/tui/jsonview).
func TestRegistryCompactsSchema(t *testing.T) {
	pretty := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "a path" }
  },
  "required": ["path"]
}`)
	noop := func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil }
	r, err := NewRegistry(Definition{Name: "t", Description: "d", Parameters: pretty, Run: noop})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Specs()[0].Parameters

	// No newlines/tabs and no indentation => compacted.
	if bytes.ContainsAny(got, "\n\t") || bytes.Contains(got, []byte("  ")) {
		t.Fatalf("schema not compacted at registration: %s", got)
	}
	// Still valid + semantically identical to the pretty source.
	var a, b any
	if err := json.Unmarshal(pretty, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatalf("compacted schema invalid: %v", err)
	}
	pa, _ := json.Marshal(a)
	pb, _ := json.Marshal(b)
	if !bytes.Equal(pa, pb) {
		t.Fatalf("normalization changed schema meaning:\n%s\n%s", pa, pb)
	}
	if len(got) >= len(pretty) {
		t.Fatalf("normalization did not shrink: %d >= %d", len(got), len(pretty))
	}
}

// Empty / invalid params must pass through unchanged (never corrupt a schema).
func TestCompactJSONSafety(t *testing.T) {
	if got := compactJSON(nil); got != nil {
		t.Fatalf("nil → %v", got)
	}
	bad := json.RawMessage(`{not json`)
	if got := compactJSON(bad); !bytes.Equal(got, bad) {
		t.Fatalf("invalid JSON should pass through unchanged, got %s", got)
	}
}
