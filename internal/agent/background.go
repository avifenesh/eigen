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
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	Role       string    `json:"role,omitempty"`
	Attempts   int       `json:"attempts,omitempty"`
	Escalated  bool      `json:"escalated,omitempty"`
	Status     string    `json:"status"` // running | done | error | canceled | lost
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished,omitempty"`

	// Observability (Tier 12): which process hosts the goroutine (lost
	// detection), live progress from the sanitized event bridge, and when the
	// record last changed (staleness display). LastTool is the most recent
	// tool the subtask STARTED — "running <tool> for 40s" while in flight,
	// cleared on the tool's result.
	Pid         int       `json:"pid,omitempty"`
	Host        string    `json:"host,omitempty"`
	Steps       int       `json:"steps,omitempty"`
	LastTool    string    `json:"last_tool,omitempty"`
	ToolStarted time.Time `json:"tool_started,omitempty"`
	LastNote    string    `json:"last_note,omitempty"`
	InTokens    int       `json:"in_tokens,omitempty"`
	OutTokens   int       `json:"out_tokens,omitempty"`
	Updated     time.Time `json:"updated,omitempty"`
	// Canceling is derived read-side from the presence of a cancel marker
	// (and persisted once the host observes it): "stop requested, not yet stopped".
	Canceling bool `json:"canceling,omitempty"`
}

// Format returns a human-readable status/result string for a background task.
func (t BgTask) Format() string {
	base := fmt.Sprintf("%s  %s", t.ID, t.Status)
	if t.Where != "" {
		base += "  " + t.Where
	}
	if t.Attempts > 1 || t.Escalated {
		attempt := t.Attempts
		if attempt < 1 {
			attempt = 1
		}
		base += fmt.Sprintf("  attempt %d", attempt)
	}
	if !t.Finished.IsZero() {
		base += "  finished " + t.Finished.Format(time.RFC3339)
	}
	switch t.Status {
	case "done":
		return base + "\n\n" + t.Result
	case "error":
		return base + "\n\nERROR: " + t.Error
	case "canceled":
		return base + "  canceled after " + t.Finished.Sub(t.Started).Round(time.Second).String()
	case "lost":
		return base + "  (host process gone; transcript snapshot may remain: " + t.ID + ".transcript.jsonl)"
	default:
		base += "  started " + t.Started.Format(time.RFC3339)
		if t.Steps > 0 {
			base += fmt.Sprintf("  step %d", t.Steps)
		}
		if t.LastTool != "" {
			base += "  tool: " + t.LastTool
		}
		if t.Canceling {
			base += "  (cancel requested)"
		}
		return base
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
	r := &BgRegistry{dir: dir, tasks: map[string]*BgTask{}}
	if dir != "" {
		// Adopt persisted state: durably mark lost tasks (their host died
		// without writing a terminal line) and prune old terminal ones, so
		// every reader of the dir agrees from here on.
		r.adoptStale()
	}
	return r
}

// TasksDir returns the background-task directory: ~/.eigen/tasks for the
// default instance, ~/.eigen/tasks-<instance> when EIGEN_INSTANCE is set (a
// dev daemon's tasks stay isolated from production's). Mirrors the daemon's
// instance-name validation (a malformed env value is ignored → default).
func TasksDir() string {
	home, err := os.UserHomeDir()
	base := "tasks" + tasksInstanceSuffix()
	if err != nil {
		return filepath.Join(os.TempDir(), "eigen-"+base)
	}
	return filepath.Join(home, ".eigen", base)
}

var validTaskInstance = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,31}$`)

func tasksInstanceSuffix() string {
	if n := os.Getenv("EIGEN_INSTANCE"); validTaskInstance.MatchString(n) {
		return "-" + n
	}
	return ""
}

// next allocates a unique task id (time-based prefix keeps files sortable).
func (r *BgRegistry) next() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return fmt.Sprintf("bg-%d-%d", time.Now().UnixNano(), r.seq)
}

// put records (and persists) a task state.
// SeedDone registers a pre-completed background task with the given result and
// returns its id. Used to inject a result the registry didn't itself run (e.g.
// tests, or adopting an external computation) so the wake-on-done path can
// surface it.
func (r *BgRegistry) SeedDone(task, result string) string {
	id := r.next()
	r.put(&BgTask{ID: id, Task: task, Status: "done", Result: result, Started: time.Now(), Finished: time.Now()})
	return id
}

func (r *BgRegistry) put(t *BgTask) {
	r.mu.Lock()
	t.Updated = time.Now()
	cp := *t
	r.tasks[t.ID] = &cp
	r.reapLocked() // bound the in-memory map over long daemon uptime
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

// maxRetainedTasks bounds the in-memory task map. Running tasks are ALWAYS kept
// (never reaped); only finished/terminal records beyond this many (most-recent
// first) are dropped from memory — their jsonl stays on disk, and `task_status`
// can still read it. Prevents the registry growing unbounded over a long-lived
// daemon that runs many subtasks.
const maxRetainedTasks = 200

// reapLocked drops the oldest terminal tasks when the map exceeds the cap.
// Caller holds r.mu. Running tasks are exempt.
func (r *BgRegistry) reapLocked() {
	if len(r.tasks) <= maxRetainedTasks {
		return
	}
	// Collect terminal tasks (everything except still-running), oldest first.
	type ref struct {
		id string
		at time.Time
	}
	var term []ref
	for id, t := range r.tasks {
		if t.Status == "running" || t.Status == "" {
			continue // never reap a live task
		}
		when := t.Finished
		if when.IsZero() {
			when = t.Updated
		}
		term = append(term, ref{id, when})
	}
	// How many we must drop to get back under the cap.
	over := len(r.tasks) - maxRetainedTasks
	if over > len(term) {
		over = len(term) // can't drop running tasks; cap may be exceeded by live work
	}
	if over <= 0 {
		return
	}
	sort.Slice(term, func(i, j int) bool { return term[i].at.Before(term[j].at) })
	for i := 0; i < over; i++ {
		delete(r.tasks, term[i].id)
	}
}

// update applies fn to a live task record under the registry lock, then
// persists the new state. This is the single mutation path for the event
// bridge + lifecycle transitions, so concurrent updates (tool events vs
// completion vs cancel) serialize instead of racing.
func (r *BgRegistry) update(id string, fn func(*BgTask)) {
	r.mu.Lock()
	t, ok := r.tasks[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	fn(t)
	cp := *t
	r.mu.Unlock()
	r.put(&cp)
}

// Get returns a task by id (nil when unknown).
func (r *BgRegistry) Get(id string) *BgTask {
	r.mu.Lock()
	if t, ok := r.tasks[id]; ok {
		cp := *t
		r.mu.Unlock()
		return &cp
	}
	dir := r.dir
	r.mu.Unlock()
	// Disk fallback: tasks from a previous process live on as files (the
	// whole point of the durable store) — task_status must find them after a
	// restart too.
	if dir != "" && bgIDRe.MatchString(id) {
		if t, ok := readTaskFile(filepath.Join(dir, id+".jsonl")); ok {
			return &t
		}
	}
	return nil
}

// List returns all tasks, running first, then most recently started.
// Disk records (previous processes) are merged in; in-memory wins on overlap.
func (r *BgRegistry) List() []BgTask {
	r.mu.Lock()
	out := make([]BgTask, 0, len(r.tasks))
	seen := make(map[string]bool, len(r.tasks))
	for _, t := range r.tasks {
		out = append(out, *t)
		seen[t.ID] = true
	}
	dir := r.dir
	r.mu.Unlock()
	if dir != "" {
		for _, t := range LoadBgTasks(dir) {
			if !seen[t.ID] {
				out = append(out, t)
			}
		}
	}
	sortBgTasks(out)
	return out
}

// History returns the persisted state-change history for a task. If there is
// no disk store (tests/in-memory embedding), it falls back to the current record.
func (r *BgRegistry) History(id string) []BgTask {
	if !bgIDRe.MatchString(id) {
		return nil
	}
	r.mu.Lock()
	var cur BgTask
	curOK := false
	if t, ok := r.tasks[id]; ok {
		cur = *t
		curOK = true
	}
	dir := r.dir
	r.mu.Unlock()
	if dir != "" {
		if hist := readTaskHistory(filepath.Join(dir, id+".jsonl")); len(hist) > 0 {
			return hist
		}
	}
	if curOK {
		return []BgTask{cur}
	}
	return nil
}

// StatePath and TranscriptPath return durable observability paths for a task.
func (r *BgRegistry) StatePath(id string) string {
	if r == nil || r.dir == "" || !bgIDRe.MatchString(id) {
		return ""
	}
	return filepath.Join(r.dir, id+".jsonl")
}

func (r *BgRegistry) TranscriptPath(id string) string {
	if r == nil || r.dir == "" || !bgIDRe.MatchString(id) {
		return ""
	}
	return filepath.Join(r.dir, id+".transcript.jsonl")
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
	host, _ := os.Hostname()
	rec := &BgTask{
		ID: id, Task: task, Where: where,
		Kind: opts.Kind, Difficulty: opts.Difficulty, Model: opts.Model, Role: opts.Role,
		Attempts: 1,
		Status:   "running", Started: time.Now(),
		Pid: os.Getpid(), Host: host,
	}
	if dir := a.Bg.dir; dir != "" {
		// A stale <id>.cancel must never kill a fresh task: clear it BEFORE
		// the task becomes visible as running. RequestCancel only writes
		// markers for tasks the disk says are running, so any marker dropped
		// after this point is a genuine request for THIS task.
		os.Remove(filepath.Join(dir, id+".cancel"))
	}
	a.Bg.put(rec)

	go func() {
		// This is deliberately detached from the caller's context: interrupting the
		// foreground turn does not cancel it. It still shares the daemon process;
		// the durable jsonl/transcript make it process-like to the orchestrator:
		// start, poll, collect.
		//
		// One task-level deadline spans ALL attempts: bgMaxRuntime is the per-TASK
		// cap, not the per-attempt cap. Each attempt derives its context from
		// taskCtx, so a retry/escalation runs against the time the first attempt
		// left rather than a fresh bgMaxRuntime (which let one task run ~2x the cap).
		taskCtx, taskCancel := context.WithTimeout(context.Background(), bgMaxRuntime)
		defer taskCancel()
		attempt := a.runBackgroundAttempt(taskCtx, id, task, opts, depth, 1, sub, where)
		res, err := attempt.result, attempt.err
		canceled, stalled := attempt.canceled, attempt.stalled
		var firstSummary string
		if compactedTask, reason, ok := a.nextBackgroundContextRetry(context.Background(), task, attempt); ok && !canceled {
			firstSummary = reason
			a.Bg.update(id, func(t *BgTask) {
				t.LastNote = "attempt 1 " + reason
			})
			retry := a.runBackgroundAttempt(taskCtx, id, compactedTask, opts, depth, 2, nil, "")
			res, err = retry.result, retry.err
			canceled, stalled = retry.canceled, retry.stalled
			if err != nil && !canceled && firstSummary != "" {
				err = fmt.Errorf("attempt 1 %s; attempt 2: %w", firstSummary, err)
			}
		} else if next, reason, ok := nextBackgroundEscalation(opts, err, res, stalled); ok && !canceled {
			firstSummary = backgroundAttemptSummary(reason, err)
			a.Bg.update(id, func(t *BgTask) {
				t.LastNote = "attempt 1 " + firstSummary + " → escalating to difficulty " + next.Difficulty
				t.Escalated = true
			})
			// runBackgroundAttempt is synchronous: the first attempt's context has
			// returned before retry starts, so the same task id never has two live
			// result writers racing to publish a terminal state.
			retry := a.runBackgroundAttempt(taskCtx, id, task, next, depth, 2, nil, "")
			res, err = retry.result, retry.err
			canceled, stalled = retry.canceled, retry.stalled
			if err != nil && !canceled && firstSummary != "" {
				err = fmt.Errorf("attempt 1 %s; attempt 2: %w", firstSummary, err)
			}
		}
		status := "done"
		a.Bg.update(id, func(t *BgTask) {
			t.Finished = time.Now()
			switch {
			case err != nil && canceled:
				t.Status, t.Error = "canceled", ""
			case err != nil && stalled:
				t.Status, t.Error = "error", "stalled (no tool activity for "+stallIdle.String()+")"
			case err != nil:
				t.Status, t.Error = "error", err.Error()
			default:
				t.Status, t.Result = "done", res
			}
			status = t.Status
		})
		if dir := a.Bg.dir; dir != "" {
			os.Remove(filepath.Join(dir, id+".cancel")) // never leave stale markers
		}
		a.emitBgFinished(id, status, err)
	}()

	label := "started background task " + id
	if where != "" {
		label += " (" + where + ")"
	}
	return label + " — continue working; check with task_status, or collect when the finish note arrives", nil
}

type bgAttemptOutcome struct {
	result   string
	err      error
	canceled bool
	stalled  bool
	where    string
	messages []llm.Message
}

// backgroundRetryContext builds the task-level context for a promoted task's
// retry. The promoted first attempt ran on prior (created with bgMaxRuntime in
// runChild and already canceled by the time we retry); a retry must honor the
// SAME absolute task deadline so promote-then-retry can't exceed the per-task
// cap. It carries only prior's deadline, not its cancellation. Falls back to a
// fresh bgMaxRuntime timeout if prior somehow has no deadline.
func backgroundRetryContext(prior context.Context) (context.Context, context.CancelFunc) {
	if dl, ok := prior.Deadline(); ok {
		return context.WithDeadline(context.Background(), dl)
	}
	return context.WithTimeout(context.Background(), bgMaxRuntime)
}

// runBackgroundAttempt runs one attempt of a background task against taskCtx,
// which carries the SINGLE task-level deadline (bgMaxRuntime) shared by all
// attempts. The attempt's own context is derived from taskCtx with a fresh
// cancel so stall/cancel-marker watchers can stop just this attempt, while the
// shared deadline still bounds the task's total runtime across retries.
func (a *Agent) runBackgroundAttempt(taskCtx context.Context, id, task string, opts SubtaskOpts, depth, attempt int, sub *Agent, where string) bgAttemptOutcome {
	if sub == nil {
		sub, where = a.subAgent(context.Background(), task, opts)
	}
	a.Bg.update(id, func(t *BgTask) {
		t.Where = where
		t.Kind = opts.Kind
		t.Difficulty = opts.Difficulty
		t.Model = opts.Model
		t.Role = opts.Role
		t.Attempts = attempt
		t.Status = "running"
		t.Error = ""
		t.Result = ""
	})

	// The background transcript is its own durable artifact: every message the
	// subtask exchanges is rewritten to <id>.transcript.jsonl (one JSON message
	// per line, same shape as session files) so the run is observable from
	// outside the process while it lives and auditable after. A retry rewrites the
	// same artifact with the latest attempt's transcript; the task jsonl keeps the
	// full attempt history/status trail.
	if dir := a.Bg.dir; dir != "" {
		tpath := filepath.Join(dir, id+".transcript.jsonl")
		sub.Persist = func(msgs []llm.Message) { writeTranscript(tpath, msgs) }
	}

	// Sanitized event bridge (Tier 12) plus heartbeat-based stall detection. The
	// subtask stays silent in the parent transcript, while task_status gets live
	// progress and a hung background task can be escalated instead of wedging.
	hb := newHeartbeat()
	sub.OnEvent = activitySink(hb, bgEventSink(a.Bg, id, nil))
	sub.onModelCall = hb.modelStart

	// Derive this attempt from the task-level deadline: cancel() stops only this
	// attempt (stall/cancel-marker), while taskCtx's bgMaxRuntime deadline still
	// caps the task across all attempts.
	bgCtx, cancel := context.WithCancel(taskCtx)
	defer cancel()
	bgCtx = context.WithValue(bgCtx, subtaskDepthKey{}, depth+1)
	canceled := watchCancelMarker(bgCtx, cancel, a.Bg, id)
	stalled := watchStall(bgCtx, hb, cancel, stallIdle, modelMaxWait, heartbeatGrace)
	sess := sub.NewSession()
	res, err := sess.Send(bgCtx, task)
	return bgAttemptOutcome{result: res, err: err, canceled: canceled(), stalled: stalled(), where: where, messages: sess.snapshot()}
}

func (a *Agent) nextBackgroundContextRetry(ctx context.Context, task string, attempt bgAttemptOutcome) (string, string, bool) {
	if !llm.IsContextOverflow(attempt.err) {
		return "", "", false
	}
	target := backgroundContextRetryTarget(a, attempt.messages)
	if len(attempt.messages) > 1 {
		// Use deterministic compaction here rather than another model call: this path
		// is already recovering from a model context rejection, so the retry should
		// split/shed locally and avoid a second potentially-too-large compactor call.
		if compacted, err := llm.CompactWith(ctx, nil, attempt.messages, target); err == nil && llm.EstimateTokens(compacted) < llm.EstimateTokens(attempt.messages) {
			return compactedBackgroundTask(task, compacted), "exceeded context window; retrying with compacted transcript", true
		}
	}
	compactedTask := compactOversizedTaskText(task, target)
	if compactedTask == task {
		return "", "exceeded context window; unable to compact task", false
	}
	return compactedTask, "exceeded context window; retrying with compacted task prompt", true
}

func backgroundContextRetryTarget(a *Agent, msgs []llm.Message) int {
	budget := a.maxContextTokens()
	target := 8000
	if budget > 0 {
		target = budget / 2
		if target > 16000 {
			target = 16000
		}
	}
	if cur := llm.EstimateTokens(msgs); cur > 0 && cur/2 < target {
		target = cur / 2
	}
	if target < 1000 {
		target = 1000
	}
	return target
}

func compactedBackgroundTask(original string, msgs []llm.Message) string {
	var b strings.Builder
	b.WriteString("The previous background subtask attempt exceeded the model context window. Continue from this compacted transcript instead of rerouting to a stronger model. If omitted details are needed, inspect the project/files with tools rather than guessing.\n\n")
	if strings.TrimSpace(original) != "" {
		b.WriteString("Original task summary/excerpt:\n")
		b.WriteString(compactOversizedText(original, 1200))
		b.WriteString("\n\n")
	}
	b.WriteString("Compacted transcript:\n")
	for _, m := range msgs {
		label := string(m.Role)
		if m.Role == llm.RoleTool && m.ToolName != "" {
			label += "/" + m.ToolName
		}
		text := m.Text
		if text == "" && len(m.ToolCalls) > 0 {
			var names []string
			for _, tc := range m.ToolCalls {
				if tc.Name != "" {
					names = append(names, tc.Name)
				}
			}
			if len(names) > 0 {
				text = "tool calls: " + strings.Join(names, ", ")
			}
		}
		if text == "" {
			continue
		}
		b.WriteString("\n[" + label + "]\n")
		b.WriteString(compactOversizedText(text, 4000))
		b.WriteString("\n")
	}
	return b.String()
}

func compactOversizedTaskText(task string, maxTokens int) string {
	if strings.TrimSpace(task) == "" {
		return task
	}
	body := compactOversizedText(task, maxTokens*4)
	if body == task {
		return task
	}
	return "The original background subtask prompt exceeded the model context window and was compacted before retrying. Preserve the user's intent from this head/tail excerpt. If omitted details are needed, inspect the repo/files with tools rather than guessing.\n\n" + body
}

func compactOversizedText(s string, maxChars int) string {
	if maxChars < 1000 {
		maxChars = 1000
	}
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	head := maxChars * 3 / 5
	tail := maxChars - head
	if head < 1 {
		head = 1
	}
	if tail < 1 {
		tail = 1
	}
	omitted := len(r) - head - tail
	return string(r[:head]) + fmt.Sprintf("\n\n[... %d characters omitted from oversized prompt ...]\n\n", omitted) + string(r[len(r)-tail:])
}

func nextBackgroundEscalation(opts SubtaskOpts, err error, result string, stalled bool) (SubtaskOpts, string, bool) {
	if strings.TrimSpace(opts.Model) != "" || llm.IsContextOverflow(err) {
		return opts, "", false
	}
	up := escalateDifficulty(opts.Difficulty)
	if up == opts.Difficulty {
		return opts, "", false
	}
	var reason string
	switch {
	case stalled:
		reason = "stalled"
	case err != nil:
		reason = "failed"
	case reportsUnderpowered(result):
		reason = "reported underpowered model"
	default:
		return opts, "", false
	}
	next := opts
	next.Difficulty = up
	// Reset the explicit effort override so the type/difficulty policy is
	// re-evaluated at the new (harder) tier — an escalated attempt should be
	// allowed to think harder, not stay pinned to the prior level.
	next.Effort = ""
	return next, reason, true
}

func backgroundAttemptSummary(reason string, err error) string {
	if err != nil && reason == "failed" {
		return reason + ": " + truncateForNote(err.Error())
	}
	return reason
}

// reportsUnderpowered is intentionally narrow and checks only the final answer,
// not streamed partials. Context-window complaints are not retried here: raising
// difficulty is not guaranteed to increase context, so context overflow needs a
// separate split/compact strategy rather than a blind stronger-model retry.
func reportsUnderpowered(s string) bool {
	lower := strings.ToLower(s)
	phrases := []string{
		"underpowered model",
		"need a stronger model",
		"needs a stronger model",
		"stronger model required",
		"model too weak",
		"too weak for this task",
	}
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// watchCancelMarker polls for <id>.cancel while the task runs (the
// cross-process cancel protocol: any process may drop the marker; the hosting
// goroutine honors it). On observing the marker it persists Canceling (so
// readers see "stop requested" immediately) and cancels the task context.
// The returned func reports whether cancellation was requested — the
// completion path uses it to distinguish "canceled" from a plain error
// (context cancellation surfaces as an error from Send).
func watchCancelMarker(ctx context.Context, cancel context.CancelFunc, r *BgRegistry, id string) func() bool {
	if r.dir == "" {
		return func() bool { return false }
	}
	marker := filepath.Join(r.dir, id+".cancel")
	var hit atomic.Bool
	go func() {
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if _, err := os.Stat(marker); err == nil {
					hit.Store(true)
					r.update(id, func(t *BgTask) { t.Canceling = true })
					cancel()
					return
				}
			}
		}
	}()
	return hit.Load
}

// sanitizeNote bounds and flattens a note for the durable record.
func sanitizeNote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	// Rune-safe cap: byte-slicing split multibyte runes, emitting invalid UTF-8
	// into the durable BgTask.LastNote record and the panels that render it.
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
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
	// Rune-safe cap so a multibyte rune is never split into invalid UTF-8.
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

// emitBgFinished emits the completion note + a structured EventBgDone (carrying
// the task id) when a background/promoted task ends. The EventBgDone lets an
// idle orchestrator session WAKE and collect the result (the daemon starts a
// fresh turn on it); front-ends that don't wake just render the note.
func (a *Agent) emitBgFinished(id, status string, err error) {
	note := "background task " + id + " finished"
	switch status {
	case "error":
		if err != nil {
			note = "background task " + id + " FAILED: " + truncateForNote(err.Error())
		} else {
			note = "background task " + id + " FAILED"
		}
	case "canceled":
		note = "background task " + id + " canceled"
	}
	a.emit(Event{Kind: EventNote, Text: note + " — task_status " + id + " to collect"})
	a.emit(Event{Kind: EventBgDone, Result: id, Text: note})
}

// BgResult returns a finished background task's result text (empty when the
// task is unknown, still running, errored, or canceled — i.e. nothing useful to
// hand an orchestrator). Used by the daemon to wake an idle session.
func (a *Agent) BgResult(id string) string {
	if a.Bg == nil {
		return ""
	}
	t := a.Bg.Get(id)
	if t == nil || t.Status != "done" {
		return ""
	}
	return t.Result
}

// progress into its durable BgTask record (bounded — never text deltas), then
// calls chain (if any). Shared by SubtaskBackground and promoteRunning.
func bgEventSink(bg *BgRegistry, id string, chain EventSink) EventSink {
	return func(e Event) {
		switch e.Kind {
		case EventToolStart:
			bg.update(id, func(t *BgTask) {
				t.Steps++
				t.LastTool = e.ToolName
				t.ToolStarted = time.Now()
			})
		case EventToolResult:
			bg.update(id, func(t *BgTask) {
				t.LastTool = ""
				t.ToolStarted = time.Time{}
			})
		case EventNote:
			bg.update(id, func(t *BgTask) { t.LastNote = sanitizeNote(e.Text) })
		case EventDone:
			if e.InTokens > 0 || e.OutTokens > 0 {
				bg.update(id, func(t *BgTask) {
					t.InTokens += e.InTokens
					t.OutTokens += e.OutTokens
				})
			}
		}
		if chain != nil {
			chain(e)
		}
	}
}

// promoteRunning adopts an ALREADY-RUNNING foreground child into the background
// registry: the child keeps running on its existing context (cctx) and result
// channel (ch); this records a BgTask, rewires the child's event sink to update
// it, installs the cancel-marker watcher + idle-stall, and spawns a collector
// that records the final result and emits the completion note. Returns the new
// task id, or "" when there is no registry (caller falls back to blocking).
//
// This is the foreground→background PROMOTION: a subtask that outran the front
// window but is still active is handed off so the orchestrator regains control.
func (a *Agent) promoteRunning(cctx context.Context, cancel context.CancelFunc, c childRun, rl *relay, ch <-chan childDone, stalled func() bool, idle, front time.Duration) string {
	if a.Bg == nil {
		return ""
	}
	id := a.Bg.next()
	host, _ := os.Hostname()
	rec := &BgTask{
		ID: id, Task: c.task, Where: c.where,
		Kind: c.opts.Kind, Difficulty: c.opts.Difficulty, Model: c.opts.Model, Role: c.opts.Role,
		Attempts: 1,
		Status:   "running", Started: time.Now(),
		Pid: os.Getpid(), Host: host,
		LastNote: "promoted from foreground (still working past " + front.String() + ")",
	}
	if dir := a.Bg.dir; dir != "" {
		os.Remove(filepath.Join(dir, id+".cancel")) // a stale marker must not kill a fresh task
		tpath := filepath.Join(dir, id+".transcript.jsonl")
		rl.setPersist(func(msgs []llm.Message) { writeTranscript(tpath, msgs) })
	}
	a.Bg.put(rec)
	// Re-point the relay (NOT the agent fields — the run goroutine reads those
	// concurrently) so the child's events now update THIS bg record.
	rl.setEvent(bgEventSink(a.Bg, id, nil))
	// Honor a cross-process cancel marker for the promoted task.
	canceled := watchCancelMarker(cctx, cancel, a.Bg, id)

	bg := a.Bg
	go func() {
		d := <-ch // the same goroutine started in runChild is still running
		// Snapshot the first attempt's terminal state before cancel() tears down the
		// promoted context. cctx is detached from the parent turn, so cctx.Err()
		// here means the bgMaxRuntime deadline (or stall/cancel) fired — not a
		// parent interrupt. A deadline-exhausted run should not be retried; an
		// idle-stall cancellation is eligible for one stronger retry.
		firstCtxCanceled := cctx.Err() != nil
		firstCanceled := canceled()
		firstStalled := stalled()
		cancel()

		res, err := d.out, d.err
		canceledNow, stalledNow := firstCanceled, firstStalled
		var firstSummary string
		// deadlineExhausted: the detached context ended on its own (bgMaxRuntime)
		// without a stall or an explicit cancel marker — don't burn a retry on it.
		deadlineExhausted := firstCtxCanceled && !firstStalled && !firstCanceled
		parentCanceled := deadlineExhausted
		// A retry must honor the SAME task-level deadline as the promoted first
		// attempt (cctx was created with bgMaxRuntime in runChild): the cap is
		// per-task, so the retry runs against the deadline's REMAINING time rather
		// than a fresh bgMaxRuntime. cctx is already canceled here, so derive a
		// fresh context that only carries its absolute deadline.
		retryCtx, retryCancel := backgroundRetryContext(cctx)
		defer retryCancel()
		if compactedTask, reason, ok := a.nextBackgroundContextRetry(context.Background(), c.task, bgAttemptOutcome{err: err, messages: d.messages}); ok && !firstCanceled && !parentCanceled {
			firstSummary = reason
			bg.update(id, func(t *BgTask) {
				t.LastNote = "attempt 1 " + reason
			})
			retry := a.runBackgroundAttempt(retryCtx, id, compactedTask, c.opts, c.depth, 2, nil, "")
			res, err = retry.result, retry.err
			canceledNow, stalledNow = retry.canceled, retry.stalled
			if err != nil && !canceledNow && firstSummary != "" {
				err = fmt.Errorf("attempt 1 %s; attempt 2: %w", firstSummary, err)
			}
		} else if next, reason, ok := nextBackgroundEscalation(c.opts, err, res, firstStalled); ok && !firstCanceled && !parentCanceled {
			firstSummary = backgroundAttemptSummary(reason, err)
			bg.update(id, func(t *BgTask) {
				t.LastNote = "attempt 1 " + firstSummary + " → escalating to difficulty " + next.Difficulty
				t.Escalated = true
			})
			// The original promoted attempt has ended and its context is canceled before
			// retry starts, so the task id still has only one live writer at a time.
			retry := a.runBackgroundAttempt(retryCtx, id, c.task, next, c.depth, 2, nil, "")
			res, err = retry.result, retry.err
			canceledNow, stalledNow = retry.canceled, retry.stalled
			if err != nil && !canceledNow && firstSummary != "" {
				err = fmt.Errorf("attempt 1 %s; attempt 2: %w", firstSummary, err)
			}
		}

		status := "done"
		bg.update(id, func(t *BgTask) {
			t.Finished = time.Now()
			switch {
			case err != nil && canceledNow:
				t.Status, t.Error = "canceled", ""
			case err != nil && stalledNow:
				t.Status, t.Error = "error", "stalled (no tool activity for "+idle.String()+")"
			case err != nil:
				t.Status, t.Error = "error", err.Error()
			default:
				t.Status, t.Result = "done", res
			}
			status = t.Status
		})
		if dir := bg.dir; dir != "" {
			os.Remove(filepath.Join(dir, id+".cancel"))
		}
		a.emitBgFinished(id, status, err)
	}()
	a.emit(Event{Kind: EventNote, Text: "subtask still working past " + front.String() + " → moved to background " + id + " (task_status " + id + " to collect; you can keep working)"})
	return id
}
