package agent

// Background subtasks: fire-and-forget delegations that run detached from the
// calling turn (interrupting the turn does not kill them), persist their state
// and transcript as files under ~/.eigen/tasks/, and report back like an async
// function the orchestrator doesn't await — it checks in with the task_status
// tool (or is nudged by the finish note) and collects the result when ready.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
)

// bgMaxRuntime caps a detached background task so an abandoned one cannot run
// (and bill) forever.
const bgMaxRuntime = 30 * time.Minute

// BgTask is one background delegation's durable record. Every state change is
// appended to <id>.jsonl in the tasks dir (the last line is current), so tasks
// are observable from outside the process too.
type BgTask struct {
	ID         string    `json:"id"`
	Task       string    `json:"task"`
	Where      string    `json:"where,omitempty"` // model/route the task ran on
	Kind       string    `json:"kind,omitempty"`
	Difficulty string    `json:"difficulty,omitempty"`
	Model      string    `json:"model,omitempty"` // explicit override, if any
	Status     string    `json:"status"`          // running | done | error
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished,omitempty"`
}

// Format returns a human-readable status/result string for a background task.
func (t BgTask) Format() string {
	base := fmt.Sprintf("%s  %s", t.ID, t.Status)
	if t.Where != "" {
		base += "  " + t.Where
	}
	if !t.Finished.IsZero() {
		base += "  finished " + t.Finished.Format(time.RFC3339)
	}
	switch t.Status {
	case "done":
		return base + "\n\n" + t.Result
	case "error":
		return base + "\n\nERROR: " + t.Error
	default:
		return base + "  started " + t.Started.Format(time.RFC3339)
	}
}

// BgRegistry tracks a session's background tasks and persists their records.
type BgRegistry struct {
	mu    sync.Mutex
	dir   string // e.g. ~/.eigen/tasks
	seq   int
	tasks map[string]*BgTask
}

// NewBgRegistry creates a registry persisting under dir (created on demand).
func NewBgRegistry(dir string) *BgRegistry {
	return &BgRegistry{dir: dir, tasks: map[string]*BgTask{}}
}

// TasksDir returns the default background-task directory (~/.eigen/tasks).
func TasksDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "eigen-tasks")
	}
	return filepath.Join(home, ".eigen", "tasks")
}

// next allocates a unique task id (time-based prefix keeps files sortable).
func (r *BgRegistry) next() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return fmt.Sprintf("bg-%d-%d", time.Now().UnixNano(), r.seq)
}

// put records (and persists) a task state.
func (r *BgRegistry) put(t *BgTask) {
	r.mu.Lock()
	cp := *t
	r.tasks[t.ID] = &cp
	dir := r.dir
	r.mu.Unlock()
	// Append the state change to the task's jsonl (best-effort: persistence
	// failures must not break the task itself).
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	line, err := json.Marshal(cp)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, t.ID+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// Get returns a task by id (nil when unknown).
func (r *BgRegistry) Get(id string) *BgTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[id]; ok {
		cp := *t
		return &cp
	}
	return nil
}

// List returns all tasks, running first, then most recently started.
func (r *BgRegistry) List() []BgTask {
	r.mu.Lock()
	out := make([]BgTask, 0, len(r.tasks))
	for _, t := range r.tasks {
		out = append(out, *t)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Status == "running") != (out[j].Status == "running") {
			return out[i].Status == "running"
		}
		return out[i].Started.After(out[j].Started)
	})
	return out
}

// SubtaskBackground launches task as a DETACHED background delegation: it
// returns immediately with the task id while the subtask runs on its own
// context (canceling the calling turn does not kill it; bgMaxRuntime caps it).
// State + transcript persist under the registry dir; completion emits an
// EventNote on this agent so the user sees it land and the orchestrator knows
// to collect via task_status.
func (a *Agent) SubtaskBackground(ctx context.Context, task string, opts SubtaskOpts) (string, error) {
	if a.Bg == nil {
		return "", fmt.Errorf("background tasks unavailable (no registry)")
	}
	depth, _ := ctx.Value(subtaskDepthKey{}).(int)
	if depth >= maxSubtaskDepth {
		return "", fmt.Errorf("subtask depth limit (%d) reached", maxSubtaskDepth)
	}
	sub, where := a.subAgent(ctx, task, opts)
	id := a.Bg.next()
	rec := &BgTask{
		ID: id, Task: task, Where: where,
		Kind: opts.Kind, Difficulty: opts.Difficulty, Model: opts.Model,
		Status: "running", Started: time.Now(),
	}
	a.Bg.put(rec)

	// The background transcript is its own durable artifact: every message the
	// subtask exchanges is rewritten to <id>.transcript.jsonl (one JSON message
	// per line, same shape as session files) so the run is observable from
	// outside the process while it lives and auditable after.
	if dir := a.Bg.dir; dir != "" {
		tpath := filepath.Join(dir, id+".transcript.jsonl")
		sub.Persist = func(msgs []llm.Message) { writeTranscript(tpath, msgs) }
	}

	go func() {
		// This is deliberately detached from the caller's context: interrupting the
		// foreground turn does not cancel it. It still shares the daemon process;
		// the durable jsonl/transcript make it process-like to the orchestrator:
		// start, poll, collect.
		bgCtx, cancel := context.WithTimeout(context.Background(), bgMaxRuntime)
		defer cancel()
		bgCtx = context.WithValue(bgCtx, subtaskDepthKey{}, depth+1)
		res, err := sub.NewSession().Send(bgCtx, task)
		rec.Finished = time.Now()
		if err != nil {
			rec.Status, rec.Error = "error", err.Error()
		} else {
			rec.Status, rec.Result = "done", res
		}
		a.Bg.put(rec)
		note := "background task " + id + " finished"
		if rec.Status == "error" {
			note = "background task " + id + " FAILED: " + truncateForNote(rec.Error)
		}
		a.emit(Event{Kind: EventNote, Text: note + " — task_status " + id + " to collect"})
	}()

	label := "started background task " + id
	if where != "" {
		label += " (" + where + ")"
	}
	return label + " — continue working; check with task_status, or collect when the finish note arrives", nil
}

// writeTranscript atomically rewrites a background task's transcript file
// (temp+rename, mirroring the session store's crash-safety).
func writeTranscript(path string, msgs []llm.Message) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if enc.Encode(m) != nil {
			f.Close()
			os.Remove(tmp)
			return
		}
	}
	if f.Close() != nil {
		os.Remove(tmp)
		return
	}
	os.Rename(tmp, path)
}

// truncateForNote keeps failure notes one-line readable.
func truncateForNote(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
