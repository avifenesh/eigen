package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestGoalAchievedTool(t *testing.T) {
	// Confirmed verdict.
	confirmed := GoalAchieved(func(_ context.Context, ev string) (bool, string, error) {
		if ev != "tests green" {
			t.Fatalf("evidence not forwarded: %q", ev)
		}
		return true, "verified", nil
	})
	out, err := confirmed.Run(context.Background(), json.RawMessage(`{"evidence":"tests green"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "CONFIRMED") {
		t.Fatalf("confirmed output wrong: %q", out)
	}

	// Rejected verdict tells the model to continue.
	rejected := GoalAchieved(func(context.Context, string) (bool, string, error) {
		return false, "no proof", nil
	})
	out, err = rejected.Run(context.Background(), json.RawMessage(`{"evidence":"trust me"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NOT confirmed") || !strings.Contains(out, "no proof") {
		t.Fatalf("rejected output wrong: %q", out)
	}

	// Errors propagate; empty evidence rejected; nil judge errors.
	failing := GoalAchieved(func(context.Context, string) (bool, string, error) {
		return false, "", errors.New("judge down")
	})
	if _, err := failing.Run(context.Background(), json.RawMessage(`{"evidence":"x"}`)); err == nil {
		t.Fatal("judge error should propagate")
	}
	if _, err := confirmed.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("empty evidence should error")
	}
	nilJudge := GoalAchieved(nil)
	if _, err := nilJudge.Run(context.Background(), json.RawMessage(`{"evidence":"x"}`)); err == nil {
		t.Fatal("nil judge should error")
	}
}
