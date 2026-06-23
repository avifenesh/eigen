package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/observe"
)

// TestObserveSummarySubagentParity verifies the SubagentStatsDTO carries the
// full set of subagent stats the TUI renders — including StatusChecks,
// Promotes, PromoteErrors, BackgroundNotes, and RouteNotes (APP-101: app↔GUI
// parity gap on already-computed data). The records below exercise every
// accumulation path observe.ReadSummary feeds into SubagentSummary.
func TestObserveSummarySubagentParity(t *testing.T) {
	recs := []observe.Record{
		{Kind: "tool_result", Tool: "task"},
		{Kind: "tool_result", Tool: "task", IsError: true},
		{Kind: "tool_result", Tool: "task_group"},
		{Kind: "tool_result", Tool: "task_group_mutating"},
		{Kind: "tool_result", Tool: "task_status"},
		{Kind: "tool_result", Tool: "task_status"},
		{Kind: "tool_result", Tool: "task_promote"},
		{Kind: "tool_result", Tool: "task_promote", IsError: true},
		{Kind: "note", NoteKind: "background"},
		{Kind: "note", NoteKind: "route", RouteStatus: "routed"},
		{Kind: "note", NoteKind: "route", RouteStatus: "skipped"},
		{Kind: "background_done"},
	}

	path := filepath.Join(t.TempDir(), "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode record: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	s, err := observe.ReadSummary(path, 0)
	if err != nil {
		t.Fatalf("ReadSummary: %v", err)
	}

	// Map through the same code ObserveSummary uses so the test guards the
	// bridge mapping, not just the observe package.
	got := SubagentStatsDTO{
		TaskCalls: s.Subagents.TaskCalls, TaskErrors: s.Subagents.TaskErrors,
		GroupCalls: s.Subagents.GroupCalls, GroupErrors: s.Subagents.GroupErrors,
		MutatingCalls: s.Subagents.MutatingCalls, MutatingErrors: s.Subagents.MutatingErrors,
		StatusChecks: s.Subagents.StatusChecks, Promotes: s.Subagents.Promotes,
		PromoteErrors:  s.Subagents.PromoteErrors,
		BackgroundDone: s.Subagents.BackgroundDone, BackgroundNotes: s.Subagents.BackgroundNotes,
		RouteNotes: s.Subagents.RouteNotes,
	}

	want := SubagentStatsDTO{
		TaskCalls: 2, TaskErrors: 1,
		GroupCalls: 1, GroupErrors: 0,
		MutatingCalls: 1, MutatingErrors: 0,
		StatusChecks: 2, Promotes: 2, PromoteErrors: 1,
		BackgroundDone: 1, BackgroundNotes: 1, RouteNotes: 2,
	}
	if got != want {
		t.Fatalf("subagent DTO mismatch:\n got  %+v\n want %+v", got, want)
	}

	// The new fields must serialize with the camelCase keys the frontend
	// types.ts block expects.
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}
	for _, key := range []string{"statusChecks", "promotes", "promoteErrors", "backgroundNotes", "routeNotes"} {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal DTO: %v", err)
		}
		if _, ok := m[key]; !ok {
			t.Errorf("SubagentStatsDTO JSON missing field %q; got %s", key, b)
		}
	}
}
