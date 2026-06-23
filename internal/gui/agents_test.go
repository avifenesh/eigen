package gui

import (
	"strings"
	"testing"
)

// TestAgentTranscriptRejectsTraversal proves AgentTranscript validates the task
// id before touching the filesystem: a crafted traversal id (the kind a
// frontend could send to this Bridge method) must error rather than read a file
// outside the tasks dir. A well-formed id is accepted (a missing file yields an
// empty transcript, not an error). This is the APP-027 path-traversal guard.
func TestAgentTranscriptRejectsTraversal(t *testing.T) {
	b := &Bridge{}

	bad := []string{
		"../../../etc/passwd",
		"../../etc/passwd",
		"..",
		"bg-1-1/../../../etc/passwd",
		"bg-1-1.transcript", // not the bg-<n>-<n> shape
		"foo",
		"",
		"bg-1",
		"bg-1-",
		"bg--1",
		"bg-1-1\x00",
		"bg-1-1/../bg-2-2",
	}
	for _, id := range bad {
		out, err := b.AgentTranscript(id)
		if err == nil {
			t.Errorf("AgentTranscript(%q): expected error, got nil (out=%q)", id, out)
		}
		if out != "" {
			t.Errorf("AgentTranscript(%q): expected empty output on rejection, got %q", id, out)
		}
		if err != nil && !strings.Contains(err.Error(), "invalid task id") {
			t.Errorf("AgentTranscript(%q): error %q does not mention invalid task id", id, err)
		}
	}

	// A well-formed id is accepted: the (almost certainly) missing file is not an
	// error — that path proves the validator let it through to os.ReadFile.
	if _, err := b.AgentTranscript("bg-1700000000-1"); err != nil {
		t.Errorf("AgentTranscript(valid id): unexpected error %v", err)
	}
}
