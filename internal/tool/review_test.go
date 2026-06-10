package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReviewTool(t *testing.T) {
	def := Review(func(_ context.Context, artifact, focus string) (string, error) {
		if artifact == "" {
			t.Fatal("artifact should be forwarded")
		}
		return "critique: " + focus, nil
	})
	out, err := def.Run(context.Background(), json.RawMessage(`{"artifact":"some code","focus":"security"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "critique: security") {
		t.Fatalf("review output wrong: %q", out)
	}
	// Empty artifact rejected.
	if _, err := def.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("empty artifact should error")
	}
	// nil reviewer.
	nilDef := Review(nil)
	if _, err := nilDef.Run(context.Background(), json.RawMessage(`{"artifact":"x"}`)); err == nil {
		t.Fatal("nil reviewer should error")
	}
	// error propagates.
	errDef := Review(func(context.Context, string, string) (string, error) { return "", errors.New("reviewer down") })
	if _, err := errDef.Run(context.Background(), json.RawMessage(`{"artifact":"x"}`)); err == nil {
		t.Fatal("reviewer error should propagate")
	}
}
