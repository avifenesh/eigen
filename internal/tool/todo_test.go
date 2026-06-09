package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func runTodo(t *testing.T, args any) (string, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	return Todo().Run(context.Background(), b)
}

func TestTodoRendersChecklist(t *testing.T) {
	out, err := runTodo(t, map[string]any{
		"todos": []map[string]any{
			{"content": "design", "status": "completed", "priority": "high"},
			{"content": "build", "status": "in_progress", "priority": "high"},
			{"content": "test", "status": "pending", "priority": "medium"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1/3 done") {
		t.Fatalf("missing progress count: %q", out)
	}
	for _, want := range []string{"[x] design", "[~] build", "[ ] test"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTodoRejectsMultipleInProgress(t *testing.T) {
	_, err := runTodo(t, map[string]any{
		"todos": []map[string]any{
			{"content": "a", "status": "in_progress"},
			{"content": "b", "status": "in_progress"},
		},
	})
	if err == nil {
		t.Fatal("more than one in_progress should error")
	}
}

func TestTodoRejectsBadStatus(t *testing.T) {
	if _, err := runTodo(t, map[string]any{
		"todos": []map[string]any{{"content": "a", "status": "doing"}},
	}); err == nil {
		t.Fatal("invalid status should error")
	}
}

func TestTodoRequiresContent(t *testing.T) {
	if _, err := runTodo(t, map[string]any{
		"todos": []map[string]any{{"content": "", "status": "pending"}},
	}); err == nil {
		t.Fatal("empty content should error")
	}
}

func TestTodoIsReadOnly(t *testing.T) {
	if !Todo().ReadOnly {
		t.Fatal("todo should be read-only so it auto-runs")
	}
}
