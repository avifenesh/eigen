package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTaskDelegatesToRunner(t *testing.T) {
	var got string
	var gotKind, gotDiff string
	def := Task(func(_ context.Context, task, kind, difficulty string) (string, error) {
		got = task
		gotKind, gotDiff = kind, difficulty
		return "subtask result", nil
	})
	args, _ := json.Marshal(map[string]string{"task": "do the thing", "kind": "search", "difficulty": "hard"})
	out, err := def.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if got != "do the thing" {
		t.Fatalf("runner got %q", got)
	}
	if out != "subtask result" {
		t.Fatalf("result wrong: %q", out)
	}
	if gotKind != "search" || gotDiff != "hard" {
		t.Fatalf("kind/difficulty not forwarded: %q %q", gotKind, gotDiff)
	}
}

func TestTaskRequiresTask(t *testing.T) {
	def := Task(func(context.Context, string, string, string) (string, error) { return "", nil })
	if _, err := def.Run(context.Background(), json.RawMessage(`{"task":""}`)); err == nil {
		t.Fatal("empty task should error")
	}
}

func TestTaskNilRunner(t *testing.T) {
	def := Task(nil)
	if _, err := def.Run(context.Background(), json.RawMessage(`{"task":"x"}`)); err == nil {
		t.Fatal("nil runner should error")
	}
}

func TestTaskIsReadOnly(t *testing.T) {
	if !Task(nil).ReadOnly {
		t.Fatal("task tool gates via inner tools; itself should be read-only")
	}
}
