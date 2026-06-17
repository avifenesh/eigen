package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

func TestFormatTaskStatusShowsEscalatedAttempt(t *testing.T) {
	dir := t.TempDir()
	rec := agent.BgTask{
		ID:         "bg-1-1",
		Task:       "hard work",
		Status:     "running",
		Difficulty: "medium",
		Attempts:   2,
		Escalated:  true,
		Started:    time.Now().Add(-2 * time.Second),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bg-1-1.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	out := formatTaskStatus(agent.NewBgRegistry(dir), "", true)
	if !strings.Contains(out, "bg-1-1") || !strings.Contains(out, "attempt 2") {
		t.Fatalf("task_status should show escalated attempt, got:\n%s", out)
	}
}
