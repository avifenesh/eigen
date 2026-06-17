package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

func TestPromoteTaskTranscriptCreatesResumableSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	id := "bg-9-1"
	rec := agent.BgTask{ID: id, Task: "continue this background work", Status: "done", Result: "ok", Model: "demo-model", Started: time.Now(), Finished: time.Now()}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{{Role: llm.RoleUser, Text: "do work"}, {Role: llm.RoleAssistant, Text: "done"}}
	if err := transcript.Save(filepath.Join(dir, id+".transcript.jsonl"), msgs); err != nil {
		t.Fatal(err)
	}
	out, err := promoteTaskTranscript(agent.NewBgRegistry(dir), id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "promoted background task "+id) || !strings.Contains(out, "eigen --resume") {
		t.Fatalf("unexpected promote output:\n%s", out)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".eigen", "sessions", "*"+id+"*.eigen.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one promoted session, matches=%v err=%v", matches, err)
	}
	got, err := transcript.Load(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(msgs) || got[0].Text != "do work" || got[1].Text != "done" {
		t.Fatalf("promoted transcript mismatch: %+v", got)
	}
	meta, ok := transcript.LoadMeta(matches[0])
	if !ok || meta.Model != "demo-model" || !strings.Contains(meta.Title, id) {
		t.Fatalf("promoted session meta missing, ok=%v meta=%+v", ok, meta)
	}
}

func TestPromoteTaskTranscriptErrorsWithoutTranscript(t *testing.T) {
	dir := t.TempDir()
	id := "bg-9-2"
	line, err := json.Marshal(agent.BgTask{ID: id, Task: "missing transcript", Status: "done", Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = promoteTaskTranscript(agent.NewBgRegistry(dir), id)
	if err == nil || !strings.Contains(err.Error(), "read background transcript") {
		t.Fatalf("expected missing transcript error, got %v", err)
	}
}

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
	out := formatTaskStatus(agent.NewBgRegistry(dir), "", true, false, 0)
	if !strings.Contains(out, "bg-1-1") || !strings.Contains(out, "attempt 2") {
		t.Fatalf("task_status should show escalated attempt, got:\n%s", out)
	}
}

func TestFormatTaskStatusVerboseMarksMissingTranscript(t *testing.T) {
	dir := t.TempDir()
	rec := agent.BgTask{ID: "bg-3-1", Task: "quiet", Status: "running", Started: time.Now()}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bg-3-1.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	out := formatTaskStatus(agent.NewBgRegistry(dir), "bg-3-1", false, true, 0)
	if !strings.Contains(out, "transcript: "+filepath.Join(dir, "bg-3-1.transcript.jsonl")+" (not created)") {
		t.Fatalf("missing transcript should be explicit, got:\n%s", out)
	}
}

func TestFormatTaskStatusTailShowsTranscriptMessages(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Add(-10 * time.Second)
	rec := agent.BgTask{ID: "bg-4-1", Task: "tail", Status: "done", Result: "ok", Started: now, Finished: time.Now()}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bg-4-1.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: "first"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Name: "grep"}}},
		{Role: llm.RoleTool, ToolName: "grep", Text: strings.Repeat("tool output ", 40)},
		{Role: llm.RoleAssistant, Text: "final"},
	}
	var transcript []byte
	for _, msg := range msgs {
		line, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		transcript = append(transcript, append(line, '\n')...)
	}
	if err := os.WriteFile(filepath.Join(dir, "bg-4-1.transcript.jsonl"), transcript, 0o644); err != nil {
		t.Fatal(err)
	}
	out := formatTaskStatus(agent.NewBgRegistry(dir), "bg-4-1", false, false, 3)
	for _, want := range []string{
		"transcript tail (last 3 message(s)):",
		"assistant: tool calls: grep",
		"tool/grep: tool output",
		"assistant: final",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("tail output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "user: first") {
		t.Fatalf("tail should include only last 3 messages:\n%s", out)
	}
}

func TestFormatTaskStatusVerboseShowsAttemptsAndPaths(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Add(-10 * time.Second)
	recs := []agent.BgTask{
		{ID: "bg-2-1", Task: "hard work", Status: "running", Difficulty: "trivial", Attempts: 1, Started: now},
		{ID: "bg-2-1", Task: "hard work", Status: "running", Difficulty: "trivial", Attempts: 1, Escalated: true, LastNote: "attempt 1 failed: weak → escalating to difficulty easy", Started: now},
		{ID: "bg-2-1", Task: "hard work", Status: "running", Difficulty: "easy", Attempts: 2, Escalated: true, Started: now},
		{ID: "bg-2-1", Task: "hard work", Status: "done", Difficulty: "easy", Attempts: 2, Escalated: true, Result: "ok", Started: now, Finished: time.Now()},
	}
	var data []byte
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, append(line, '\n')...)
	}
	if err := os.WriteFile(filepath.Join(dir, "bg-2-1.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bg-2-1.transcript.jsonl"), []byte(`{"role":"user","text":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := formatTaskStatus(agent.NewBgRegistry(dir), "bg-2-1", false, true, 0)
	for _, want := range []string{
		"state: " + filepath.Join(dir, "bg-2-1.jsonl"),
		"transcript: " + filepath.Join(dir, "bg-2-1.transcript.jsonl"),
		"attempts:",
		"attempt 1: retried",
		"attempt 2: done",
		"attempt 1 failed: weak",
		"ok",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("verbose task_status missing %q:\n%s", want, out)
		}
	}
}
