package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// writeTaskLines fabricates a task state file from a sequence of records.
func writeTaskLines(t *testing.T, dir, id string, recs ...BgTask) {
	t.Helper()
	var data []byte
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadTaskHistorySupportsLargeLinesAndPrettyJSONFallback(t *testing.T) {
	dir := t.TempDir()
	big := BgTask{ID: "bg-8-1", Status: "error", Error: strings.Repeat("x", 100000), Started: time.Now()}
	writeTaskLines(t, dir, "bg-8-1", big)
	if got, ok := readTaskFile(filepath.Join(dir, "bg-8-1.jsonl")); !ok || got.ID != "bg-8-1" || len(got.Error) != len(big.Error) {
		t.Fatalf("large task line should parse, ok=%v got=%+v", ok, got)
	}
	hist := readTaskHistory(filepath.Join(dir, "bg-8-1.jsonl"))
	if len(hist) != 1 || len(hist[0].Error) != len(big.Error) {
		t.Fatalf("large history line should parse, got %+v", hist)
	}

	prettyPath := filepath.Join(dir, "bg-8-2.jsonl")
	pretty, err := json.MarshalIndent(BgTask{ID: "bg-8-2", Status: "done", Result: "legacy", Started: time.Now()}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prettyPath, pretty, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := readTaskFile(prettyPath); !ok || got.ID != "bg-8-2" || got.Result != "legacy" {
		t.Fatalf("pretty whole-file fallback failed, ok=%v got=%+v", ok, got)
	}
	if hist := readTaskHistory(prettyPath); len(hist) != 1 || hist[0].ID != "bg-8-2" {
		t.Fatalf("pretty history fallback failed: %+v", hist)
	}
}

func TestReadTaskHistorySkipsPartialLines(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTaskLines(t, dir, "bg-7-1",
		BgTask{ID: "bg-7-1", Status: "running", Attempts: 1, Started: now},
		BgTask{ID: "bg-7-1", Status: "running", Attempts: 2, Escalated: true, Started: now},
		BgTask{ID: "bg-7-1", Status: "done", Attempts: 2, Result: "ok", Started: now})
	f, _ := os.OpenFile(filepath.Join(dir, "bg-7-1.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"id":"bg-7-1","status":"trunc`)
	f.Close()
	hist := readTaskHistory(filepath.Join(dir, "bg-7-1.jsonl"))
	if len(hist) != 3 || hist[0].Attempts != 1 || hist[1].Attempts != 2 || hist[2].Result != "ok" {
		t.Fatalf("bad history: %+v", hist)
	}
}

func TestReadTaskHistoryExportedValidatesIDAndReadsTrail(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTaskLines(t, dir, "bg-9-1",
		BgTask{ID: "bg-9-1", Status: "running", Attempts: 1, Started: now},
		BgTask{ID: "bg-9-1", Status: "running", Attempts: 2, Escalated: true, Started: now},
		BgTask{ID: "bg-9-1", Status: "done", Attempts: 2, Result: "ok", Started: now})

	hist, err := ReadTaskHistory(dir, "bg-9-1")
	if err != nil {
		t.Fatalf("ReadTaskHistory: %v", err)
	}
	if len(hist) != 3 || hist[0].Attempts != 1 || !hist[1].Escalated || hist[2].Result != "ok" {
		t.Fatalf("exported history trail wrong: %+v", hist)
	}

	// Malformed id is the only error; it must not touch the filesystem.
	if _, err := ReadTaskHistory(dir, "../etc/passwd"); err == nil {
		t.Fatal("expected error for invalid task id")
	}

	// Missing task is not an error — observability reads return empty.
	if got, err := ReadTaskHistory(dir, "bg-9-2"); err != nil || len(got) != 0 {
		t.Fatalf("missing task should yield empty, no error: got=%+v err=%v", got, err)
	}
}

func TestLoadBgTasksSkipsTranscriptsAndPartialLines(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTaskLines(t, dir, "bg-1-1",
		BgTask{ID: "bg-1-1", Status: "running", Started: now},
		BgTask{ID: "bg-1-1", Status: "done", Result: "ok", Started: now})
	// Transcript file must NOT be parsed as a task.
	os.WriteFile(filepath.Join(dir, "bg-1-1.transcript.jsonl"), []byte(`{"role":"user"}`+"\n"), 0o644)
	// A partial final line (writer mid-append) must fall back to the last
	// complete line, not lose the task.
	f, _ := os.OpenFile(filepath.Join(dir, "bg-1-1.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"id":"bg-1-1","status":"trunc`)
	f.Close()
	// Garbage file: skipped, not fatal.
	os.WriteFile(filepath.Join(dir, "bg-9-9.jsonl"), []byte("not json\n"), 0o644)

	tasks := LoadBgTasks(dir)
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Status != "done" || tasks[0].Result != "ok" {
		t.Fatalf("want last complete state done/ok, got %+v", tasks[0])
	}
}

func TestLoadBgTasksMarksLost(t *testing.T) {
	dir := t.TempDir()
	host, _ := os.Hostname()
	// Dead pid on this host → lost.
	writeTaskLines(t, dir, "bg-1-1",
		BgTask{ID: "bg-1-1", Status: "running", Started: time.Now(), Pid: 999999, Host: host})
	// Our own pid → still running.
	writeTaskLines(t, dir, "bg-2-2",
		BgTask{ID: "bg-2-2", Status: "running", Started: time.Now(), Pid: os.Getpid(), Host: host})
	// No pid but ancient → lost by age.
	writeTaskLines(t, dir, "bg-3-3",
		BgTask{ID: "bg-3-3", Status: "running", Started: time.Now().Add(-2 * time.Hour)})

	got := map[string]string{}
	for _, task := range LoadBgTasks(dir) {
		got[task.ID] = task.Status
	}
	if got["bg-1-1"] != "lost" {
		t.Errorf("dead pid: want lost, got %s", got["bg-1-1"])
	}
	if got["bg-2-2"] != "running" {
		t.Errorf("live pid: want running, got %s", got["bg-2-2"])
	}
	if got["bg-3-3"] != "lost" {
		t.Errorf("ancient: want lost, got %s", got["bg-3-3"])
	}
}

func TestRequestCancelValidation(t *testing.T) {
	dir := t.TempDir()
	if err := RequestCancel(dir, "../evil"); err == nil {
		t.Fatal("path-traversal id must be rejected")
	}
	if err := RequestCancel(dir, "bg-1-1"); err == nil {
		t.Fatal("unknown task must be rejected")
	}
	writeTaskLines(t, dir, "bg-1-1", BgTask{ID: "bg-1-1", Status: "done", Started: time.Now()})
	if err := RequestCancel(dir, "bg-1-1"); err == nil {
		t.Fatal("finished task must be rejected (no stale markers)")
	}
	writeTaskLines(t, dir, "bg-2-2",
		BgTask{ID: "bg-2-2", Status: "running", Started: time.Now(), Pid: os.Getpid()})
	if err := RequestCancel(dir, "bg-2-2"); err != nil {
		t.Fatalf("running task cancel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bg-2-2.cancel")); err != nil {
		t.Fatal("marker file missing")
	}
	// Reader surfaces the pending cancel.
	for _, task := range LoadBgTasks(dir) {
		if task.ID == "bg-2-2" && !task.Canceling {
			t.Fatal("want Canceling=true with marker present")
		}
	}
}

func TestAdoptStaleMarksLostAndPrunes(t *testing.T) {
	dir := t.TempDir()
	host, _ := os.Hostname()
	// Lost running task: adopted durably.
	writeTaskLines(t, dir, "bg-1-1",
		BgTask{ID: "bg-1-1", Status: "running", Started: time.Now(), Pid: 999999, Host: host})
	// Old terminal task: pruned (with transcript + marker).
	writeTaskLines(t, dir, "bg-2-2", BgTask{ID: "bg-2-2", Status: "done", Started: time.Now().Add(-30 * 24 * time.Hour)})
	old := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(filepath.Join(dir, "bg-2-2.jsonl"), old, old)
	os.WriteFile(filepath.Join(dir, "bg-2-2.transcript.jsonl"), []byte("{}\n"), 0o644)

	NewBgRegistry(dir) // adoptStale runs in the constructor

	got, ok := readTaskFile(filepath.Join(dir, "bg-1-1.jsonl"))
	if !ok || got.Status != "lost" {
		t.Fatalf("want durable lost mark, got %+v ok=%v", got, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "bg-2-2.jsonl")); !os.IsNotExist(err) {
		t.Fatal("old terminal task not pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "bg-2-2.transcript.jsonl")); !os.IsNotExist(err) {
		t.Fatal("old transcript not pruned")
	}
}

func TestRegistryDiskFallback(t *testing.T) {
	dir := t.TempDir()
	writeTaskLines(t, dir, "bg-1-1", BgTask{ID: "bg-1-1", Status: "done", Result: "from disk", Started: time.Now()})
	r := NewBgRegistry(dir)
	if got := r.Get("bg-1-1"); got == nil || got.Result != "from disk" {
		t.Fatalf("Get must fall back to disk, got %+v", got)
	}
	list := r.List()
	if len(list) != 1 || list[0].ID != "bg-1-1" {
		t.Fatalf("List must merge disk records, got %+v", list)
	}
}

// bgToolProv drives one tool call then finishes, so the event bridge sees
// tool start/result and done.
type bgToolProv struct{ step int }

func (p *bgToolProv) ModelID() string { return "test" }
func (p *bgToolProv) Name() string    { return "test" }
func (p *bgToolProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.step++
	if p.step == 1 {
		return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "t1", Name: "probe", Arguments: json.RawMessage(`{}`)}}}, nil
	}
	return &llm.Response{Text: "BGDONE", Usage: llm.Usage{InputTokens: 7, OutputTokens: 3}}, nil
}

func TestBackgroundProgressBridge(t *testing.T) {
	dir := t.TempDir()
	reg := newTestRegistry(t, dir)
	a := &Agent{
		Provider: &bgToolProv{},
		Tools:    proberRegistry(t),
		Perm:     PermAuto,
		Bg:       reg,
	}
	if _, err := a.SubtaskBackground(context.Background(), "probe it", SubtaskOpts{}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "done")
	got := reg.Get(id)
	if got.Steps != 1 {
		t.Errorf("want 1 step recorded, got %d", got.Steps)
	}
	if got.LastTool != "" {
		t.Errorf("LastTool must clear after the tool result, got %q", got.LastTool)
	}
	if got.InTokens != 7 || got.OutTokens != 3 {
		t.Errorf("usage not bridged: in=%d out=%d", got.InTokens, got.OutTokens)
	}
	if got.Pid != os.Getpid() {
		t.Errorf("pid not recorded: %d", got.Pid)
	}
	if got.Result != "BGDONE" {
		t.Errorf("result: %q", got.Result)
	}
}

type bgErrorThenDoneProv struct{ calls atomic.Int32 }

func (p *bgErrorThenDoneProv) ModelID() string { return "test" }
func (p *bgErrorThenDoneProv) Name() string    { return "test" }
func (p *bgErrorThenDoneProv) Complete(context.Context, llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		return nil, fmt.Errorf("small model failed")
	}
	return &llm.Response{Text: "RECOVERED"}, nil
}

func TestBackgroundEscalatesAfterError(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	a := &Agent{Provider: &bgErrorThenDoneProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "harder than expected", SubtaskOpts{Difficulty: "trivial"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "done")
	got := reg.Get(id)
	if got.Result != "RECOVERED" || got.Attempts != 2 || !got.Escalated || got.Difficulty != "easy" {
		t.Fatalf("background task should retry one tier up, got %+v", got)
	}
	if got.LastNote == "" || !strings.Contains(got.LastNote, "escalating") {
		t.Fatalf("escalation note missing: %+v", got)
	}
}

type bgUnderpoweredThenDoneProv struct{ calls atomic.Int32 }

func (p *bgUnderpoweredThenDoneProv) ModelID() string { return "test" }
func (p *bgUnderpoweredThenDoneProv) Name() string    { return "test" }
func (p *bgUnderpoweredThenDoneProv) Complete(context.Context, llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		return &llm.Response{Text: "I need a stronger model for this task."}, nil
	}
	return &llm.Response{Text: "STRONGER_RESULT"}, nil
}

type bgOverflowThenDoneProv struct {
	calls        atomic.Int32
	originalTask string
}

func (p *bgOverflowThenDoneProv) ModelID() string { return "test" }
func (p *bgOverflowThenDoneProv) Name() string    { return "test" }
func (p *bgOverflowThenDoneProv) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		return nil, fmt.Errorf("ValidationException: context window exceeded")
	}
	if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Text, "compacted") {
		return nil, fmt.Errorf("retry did not use compacted task prompt: %+v", req.Messages)
	}
	if strings.Contains(req.Messages[0].Text, p.originalTask) {
		return nil, fmt.Errorf("retry prompt still contains full oversized task")
	}
	return &llm.Response{Text: "CONTEXT_RECOVERED"}, nil
}

func TestBackgroundCompactsContextOverflowInsteadOfEscalating(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	longTask := "important head " + strings.Repeat("middle detail ", 6000) + " important tail"
	prov := &bgOverflowThenDoneProv{originalTask: longTask}
	a := &Agent{Provider: prov, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg, MaxContextTokens: 4000}
	if _, err := a.SubtaskBackground(context.Background(), longTask, SubtaskOpts{Difficulty: "medium", Model: "fixed-model"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "done")
	got := reg.Get(id)
	if got.Result != "CONTEXT_RECOVERED" || got.Attempts != 2 || got.Escalated || got.Difficulty != "medium" || got.Model != "fixed-model" {
		t.Fatalf("context overflow should compact/retry without difficulty escalation, got %+v", got)
	}
	if !strings.Contains(got.LastNote, "context window") || !strings.Contains(got.LastNote, "compacted task") {
		t.Fatalf("context compaction note missing: %+v", got)
	}
}

func TestBackgroundContextOverflowDoesNotEscalateWhenCompactionImpossible(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	prov := &bgOverflowThenDoneProv{originalTask: "short"}
	a := &Agent{Provider: prov, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg, MaxContextTokens: 4000}
	if _, err := a.SubtaskBackground(context.Background(), "short", SubtaskOpts{Difficulty: "trivial"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "error")
	got := reg.Get(id)
	if got.Attempts != 1 || got.Escalated || got.Difficulty != "trivial" || !strings.Contains(got.Error, "context window") {
		t.Fatalf("uncompactable context overflow should not reroute, got %+v", got)
	}
}

func TestBackgroundEscalatesExplicitUnderpoweredResult(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	a := &Agent{Provider: &bgUnderpoweredThenDoneProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "do it", SubtaskOpts{}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "done")
	got := reg.Get(id)
	if got.Result != "STRONGER_RESULT" || got.Attempts != 2 || got.Difficulty != "easy" {
		t.Fatalf("explicit underpowered result should retry, got %+v", got)
	}
}

func TestBackgroundDoesNotEscalateExplicitModel(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	a := &Agent{Provider: &bgErrorThenDoneProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "use pinned model", SubtaskOpts{Model: "fixed-model", Difficulty: "trivial"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "error")
	got := reg.Get(id)
	if got.Attempts != 1 || got.Escalated || !strings.Contains(got.Error, "small model failed") {
		t.Fatalf("explicit model should not be escalated, got %+v", got)
	}
}

func TestBackgroundDoesNotEscalateAtHard(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	a := &Agent{Provider: &bgErrorThenDoneProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "already max", SubtaskOpts{Difficulty: "hard"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "error")
	got := reg.Get(id)
	if got.Attempts != 1 || got.Escalated || got.Difficulty != "hard" {
		t.Fatalf("hard task should not retry same tier, got %+v", got)
	}
}

type bgAlwaysErrorProv struct{ calls atomic.Int32 }

func (p *bgAlwaysErrorProv) ModelID() string { return "test" }
func (p *bgAlwaysErrorProv) Name() string    { return "test" }
func (p *bgAlwaysErrorProv) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return nil, fmt.Errorf("failure %d", p.calls.Add(1))
}

func TestBackgroundRetryFailurePreservesFirstError(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	a := &Agent{Provider: &bgAlwaysErrorProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "fails twice", SubtaskOpts{Difficulty: "trivial"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "error")
	got := reg.Get(id)
	if got.Attempts != 2 || !strings.Contains(got.Error, "attempt 1 failed: failure 1") || !strings.Contains(got.Error, "attempt 2: failure 2") {
		t.Fatalf("retry failure should keep both errors, got %+v", got)
	}
}

// bgDeadlineCaptureProv fails the first call then succeeds, recording the
// context deadline each attempt runs under — used to prove all attempts of one
// task share a SINGLE task-level deadline (APP-026) rather than each getting a
// fresh bgMaxRuntime.
type bgDeadlineCaptureProv struct {
	mu        sync.Mutex
	calls     int
	deadlines []time.Time
}

func (p *bgDeadlineCaptureProv) ModelID() string { return "test" }
func (p *bgDeadlineCaptureProv) Name() string    { return "test" }
func (p *bgDeadlineCaptureProv) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	dl, _ := ctx.Deadline()
	p.deadlines = append(p.deadlines, dl)
	p.mu.Unlock()
	if n == 1 {
		// Spend measurable wall-time so a per-attempt fresh bgMaxRuntime (the
		// bug) would push attempt 2's deadline visibly later than attempt 1's.
		time.Sleep(250 * time.Millisecond)
		return nil, fmt.Errorf("small model failed")
	}
	return &llm.Response{Text: "RECOVERED"}, nil
}

func TestBackgroundAttemptsShareOneTaskDeadline(t *testing.T) {
	reg := newTestRegistry(t, t.TempDir())
	prov := &bgDeadlineCaptureProv{}
	a := &Agent{Provider: prov, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "retry shares deadline", SubtaskOpts{Difficulty: "trivial"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "done")
	if got := reg.Get(id); got.Attempts != 2 {
		t.Fatalf("expected an escalation retry, got %+v", got)
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.deadlines) != 2 {
		t.Fatalf("expected 2 attempt deadlines, got %d", len(prov.deadlines))
	}
	// Both attempts derive from the SAME task-level deadline, so their absolute
	// deadlines are effectively identical. The bug — a per-attempt fresh
	// bgMaxRuntime — would put attempt 2's deadline later than attempt 1's by
	// roughly the time attempt 1 spent running (the 250ms sleep above), so a
	// tight bound discriminates the shared deadline from the per-attempt one.
	skew := prov.deadlines[1].Sub(prov.deadlines[0])
	if skew < 0 {
		skew = -skew
	}
	if skew > 50*time.Millisecond {
		t.Fatalf("attempts should share one task deadline; skew=%s (attempt 1 %s, attempt 2 %s)",
			skew, prov.deadlines[0], prov.deadlines[1])
	}
	// Sanity: the shared deadline is bounded by bgMaxRuntime from task start.
	if until := time.Until(prov.deadlines[0]); until > bgMaxRuntime+time.Minute {
		t.Fatalf("task deadline exceeds bgMaxRuntime: %s out", until)
	}
}

type bgErrorThenBlockProv struct{ calls atomic.Int32 }

func (p *bgErrorThenBlockProv) ModelID() string { return "test" }
func (p *bgErrorThenBlockProv) Name() string    { return "test" }
func (p *bgErrorThenBlockProv) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		return nil, fmt.Errorf("first failure")
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestBackgroundCancelDuringRetryWins(t *testing.T) {
	dir := t.TempDir()
	reg := newTestRegistry(t, dir)
	prov := &bgErrorThenBlockProv{}
	a := &Agent{Provider: prov, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "cancel retry", SubtaskOpts{Difficulty: "trivial"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "running")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && prov.calls.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if prov.calls.Load() < 2 {
		t.Fatal("retry attempt never started")
	}
	if err := RequestCancel(dir, id); err != nil {
		t.Fatal(err)
	}
	waitForSpecificTaskStatus(t, reg, id, "canceled")
	got := reg.Get(id)
	if got.Attempts != 2 || got.Status != "canceled" {
		t.Fatalf("cancel during retry should win, got %+v", got)
	}
}

type bgStallThenDoneProv struct{ calls atomic.Int32 }

func (p *bgStallThenDoneProv) ModelID() string { return "test" }
func (p *bgStallThenDoneProv) Name() string    { return "test" }
func (p *bgStallThenDoneProv) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &llm.Response{Text: "UNSTUCK"}, nil
}

func TestBackgroundEscalatesAfterStall(t *testing.T) {
	oldIdle, oldGrace, oldModel := stallIdle, heartbeatGrace, modelMaxWait
	stallIdle = 120 * time.Millisecond
	modelMaxWait = 120 * time.Millisecond
	heartbeatGrace = 0
	defer func() { stallIdle, heartbeatGrace, modelMaxWait = oldIdle, oldGrace, oldModel }()

	reg := newTestRegistry(t, t.TempDir())
	a := &Agent{Provider: &bgStallThenDoneProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "unstick", SubtaskOpts{Difficulty: "easy"}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "done")
	got := reg.Get(id)
	if got.Result != "UNSTUCK" || got.Attempts != 2 || got.Difficulty != "medium" {
		t.Fatalf("stalled background task should retry one tier up, got %+v", got)
	}
}

// slowBgProv blocks until its context dies (cancel-marker test).
type slowBgProv struct{}

func (slowBgProv) ModelID() string { return "test" }
func (slowBgProv) Name() string    { return "test" }
func (slowBgProv) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestBackgroundCancelMarker(t *testing.T) {
	dir := t.TempDir()
	reg := newTestRegistry(t, dir)
	a := &Agent{Provider: slowBgProv{}, Tools: proberRegistry(t), Perm: PermAuto, Bg: reg}
	if _, err := a.SubtaskBackground(context.Background(), "spin forever", SubtaskOpts{}); err != nil {
		t.Fatal(err)
	}
	id := waitForStatus(t, reg, "running")
	if err := RequestCancel(dir, id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := reg.Get(id); got != nil && got.Status == "canceled" {
			if _, err := os.Stat(filepath.Join(dir, id+".cancel")); !os.IsNotExist(err) {
				t.Fatal("marker must be cleaned up after cancellation")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("task never canceled: %+v", reg.Get(id))
}

// --- helpers ---

func newTestRegistry(t *testing.T, dir string) *BgRegistry {
	t.Helper()
	return NewBgRegistry(dir)
}

func proberRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg, err := tool.NewRegistry(tool.Definition{
		Name:       "probe",
		ReadOnly:   true,
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { return "probed", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func waitForStatus(t *testing.T, reg *BgRegistry, status string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, task := range reg.List() {
			if task.Status == status {
				return task.ID
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	var dump string
	for _, task := range reg.List() {
		dump += fmt.Sprintf("%s=%s ", task.ID, task.Status)
	}
	t.Fatalf("no task reached %q (have: %s)", status, dump)
	return ""
}

func waitForSpecificTaskStatus(t *testing.T, reg *BgRegistry, id, status string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := reg.Get(id); got != nil && got.Status == status {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %q (got %+v)", id, status, reg.Get(id))
}

func TestTasksDirInstanceScoping(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("EIGEN_INSTANCE", "")
	if got := TasksDir(); got != filepath.Join(home, ".eigen", "tasks") {
		t.Errorf("default tasks dir = %q", got)
	}
	t.Setenv("EIGEN_INSTANCE", "dev")
	if got := TasksDir(); got != filepath.Join(home, ".eigen", "tasks-dev") {
		t.Errorf("dev tasks dir = %q", got)
	}
	t.Setenv("EIGEN_INSTANCE", "bad/name")
	if got := TasksDir(); got != filepath.Join(home, ".eigen", "tasks") {
		t.Errorf("invalid instance should fall back to default, got %q", got)
	}
}
