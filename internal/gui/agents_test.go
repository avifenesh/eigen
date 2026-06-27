package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
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

func TestTasksAPIListsTasksAndKeepsAssetsStable(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-2 * time.Minute)
	writeTaskState(t, dir, agent.BgTask{
		ID:      "bg-1700000000-1",
		Task:    "inspect flaky test",
		Status:  "running",
		Started: start,
		Pid:     os.Getpid(),
		Model:   "gpt-test",
		Steps:   2,
	})
	writeTaskState(t, dir, agent.BgTask{
		ID:       "bg-1700000001-2",
		Task:     "summarize docs",
		Status:   "done",
		Result:   "ready",
		Started:  start.Add(-time.Minute),
		Finished: start,
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("asset ok"))
	})
	h := newTasksAPIHandler(next, func() string { return dir })

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "asset ok" {
		t.Fatalf("non-task asset path was not delegated: code=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tasks status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("GET /api/tasks content-type = %q", got)
	}
	var dto TasksDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("GET /api/tasks invalid json: %v", err)
	}
	if dto.Dir != dir || dto.Running != 1 || dto.Done != 1 || dto.Errored != 0 {
		t.Fatalf("unexpected task counts: %+v", dto)
	}
	if len(dto.Tasks) != 2 || dto.Tasks[0].ID != "bg-1700000000-1" || dto.Tasks[0].Status != "running" {
		t.Fatalf("tasks not returned running-first: %+v", dto.Tasks)
	}
	if dto.Tasks[0].Model != "gpt-test" || dto.Tasks[0].Steps != 2 || dto.Tasks[1].Result != "ready" {
		t.Fatalf("task detail fields missing: %+v", dto.Tasks)
	}
}

func TestTasksAPICancelCreatesMarkerAndSnapshotShowsCanceling(t *testing.T) {
	dir := t.TempDir()
	id := "bg-1700000000-1"
	writeTaskState(t, dir, agent.BgTask{
		ID:      id,
		Task:    "long scan",
		Status:  "running",
		Started: time.Now(),
		Pid:     os.Getpid(),
	})
	h := newTasksAPIHandler(nil, func() string { return dir })

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id+"/cancel", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST cancel status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, id+".cancel")); err != nil {
		t.Fatalf("cancel marker was not written: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var dto TasksDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("GET /api/tasks invalid json after cancel: %v", err)
	}
	if len(dto.Tasks) != 1 || !dto.Tasks[0].Canceling {
		t.Fatalf("snapshot did not expose canceling task: %+v", dto.Tasks)
	}
}

func TestTasksAPICancelRejectsBadOrFinishedTasks(t *testing.T) {
	dir := t.TempDir()
	writeTaskState(t, dir, agent.BgTask{
		ID:       "bg-1700000000-1",
		Task:     "already done",
		Status:   "done",
		Started:  time.Now().Add(-time.Minute),
		Finished: time.Now(),
	})
	h := newTasksAPIHandler(nil, func() string { return dir })

	for _, tc := range []struct {
		name string
		path string
		want int
	}{
		{name: "invalid id", path: "/api/tasks/not-a-bg/cancel", want: http.StatusBadRequest},
		{name: "finished", path: "/api/tasks/bg-1700000000-1/cancel", want: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("POST %s status = %d, want %d body=%s", tc.path, rec.Code, tc.want, rec.Body.String())
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("error content-type = %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestTasksAPITranscript(t *testing.T) {
	dir := t.TempDir()
	id := "bg-1700000000-1"
	if err := os.WriteFile(filepath.Join(dir, id+".transcript.jsonl"), []byte(`{"Role":"assistant","Text":"done"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTasksAPIHandler(nil, func() string { return dir })

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+id+"/transcript", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET transcript status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Transcript string `json:"transcript"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET transcript invalid json: %v", err)
	}
	if !strings.Contains(got.Transcript, `"Text":"done"`) {
		t.Fatalf("transcript not returned: %q", got.Transcript)
	}
}

func writeTaskState(t *testing.T, dir string, task agent.BgTask) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, task.ID+".jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
