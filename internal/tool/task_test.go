package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTaskDelegatesToRunner(t *testing.T) {
	var got string
	var gotOpts TaskOpts
	var gotBg bool
	def := Task(func(_ context.Context, task string, opts TaskOpts, background bool) (string, error) {
		got = task
		gotOpts = opts
		gotBg = background
		return "subtask result", nil
	})
	args, _ := json.Marshal(map[string]any{"task": "do the thing", "kind": "search", "difficulty": "hard", "model": "grok-4", "role": "demo-agent", "background": true})
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
	if gotOpts.Kind != "search" || gotOpts.Difficulty != "hard" || gotOpts.Model != "grok-4" || gotOpts.Role != "demo-agent" || !gotBg {
		t.Fatalf("opts not forwarded: %+v background=%v", gotOpts, gotBg)
	}
}

func TestTaskRequiresTask(t *testing.T) {
	def := Task(func(context.Context, string, TaskOpts, bool) (string, error) { return "", nil })
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

func TestTaskStatusTool(t *testing.T) {
	def := TaskStatus(func(_ context.Context, id string, all, verbose bool) (string, error) {
		if id != "bg1" || all || !verbose {
			t.Fatalf("bad status args id=%q all=%v verbose=%v", id, all, verbose)
		}
		return "done", nil
	})
	out, err := def.Run(context.Background(), json.RawMessage(`{"id":"bg1","verbose":true}`))
	if err != nil || out != "done" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
