package agent

import (
	"context"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
)

// Subtask lifecycle: idle-based stall detection + a foreground window that
// promotes a still-working subtask to the background.
//
// The rule (user-specified): a (sub)agent is "stalled" when it makes NO tool
// call for stallIdle — NOT when it exceeds some global wall-clock. A subtask
// doing heavy work with steady tool calls runs as long as it needs; one that
// goes silent is hung and gets cancelled. Separately, a FOREGROUND subtask runs
// in front for frontWindow; if it's still active past that, it moves to the
// background so the orchestrator regains control (and can task_status / cancel
// it) instead of blocking.

var (
	// stallIdle is the max time a running (sub)agent may go with no tool
	// activity before it's considered hung and cancelled. Per-agent, not
	// global — measured from the last tool start/result (or stream delta).
	stallIdle = 2 * time.Minute
	// frontWindow is how long a foreground subtask runs inline before it's
	// promoted to the background (if still active). The orchestrator blocks
	// only this long on any one subtask.
	frontWindow = 2 * time.Minute
	// heartbeatGrace lets a just-started child take its first action before the
	// idle clock can fire (the first model call may be slow).
	heartbeatGrace = 30 * time.Second
)

// SetLifecycle overrides the foreground front-window and idle-stall windows
// (minutes; 0 keeps the current default). Called once at startup from config —
// not while subtasks run (each run snapshots the values at start, so a change
// never races a running watchdog, but the intended use is startup config).
func SetLifecycle(frontMin, stallMin int) {
	if frontMin > 0 {
		frontWindow = time.Duration(frontMin) * time.Minute
	}
	if stallMin > 0 {
		stallIdle = time.Duration(stallMin) * time.Minute
	}
}

// heartbeat tracks the last time a watched agent did something (tool call or
// stream delta). It is bumped from the event sink and read by the stall watch.
type heartbeat struct {
	mu   sync.Mutex
	last time.Time
}

func newHeartbeat() *heartbeat { return &heartbeat{last: time.Now()} }

func (h *heartbeat) beat() {
	h.mu.Lock()
	h.last = time.Now()
	h.mu.Unlock()
}

func (h *heartbeat) idleFor() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Since(h.last)
}

// activitySink wraps an OnEvent sink so every tool start/result and stream
// delta bumps the heartbeat (the chained sink still runs). Activity = the agent
// is making progress; silence past stallIdle = hung.
func activitySink(hb *heartbeat, chain EventSink) EventSink {
	return func(e Event) {
		switch e.Kind {
		case EventToolStart, EventToolResult, EventTextDelta, EventReasoningDelta, EventNote:
			hb.beat()
		}
		if chain != nil {
			chain(e)
		}
	}
}

// relay is a settable indirection for a child agent's OnEvent/Persist sinks. It
// is installed ONCE on the sub-agent before the run goroutine starts, so the
// agent's fields are never mutated mid-flight (the run goroutine reads them
// concurrently — a swap would race). Promotion to background re-points the
// relay's target under a mutex instead of swapping the agent field.
type relay struct {
	mu      sync.Mutex
	onEvent EventSink
	persist func([]llm.Message)
}

func (r *relay) emit(e Event) {
	r.mu.Lock()
	fn := r.onEvent
	r.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}

func (r *relay) save(msgs []llm.Message) {
	r.mu.Lock()
	fn := r.persist
	r.mu.Unlock()
	if fn != nil {
		fn(msgs)
	}
}

func (r *relay) setEvent(fn EventSink) {
	r.mu.Lock()
	r.onEvent = fn
	r.mu.Unlock()
}

func (r *relay) setPersist(fn func([]llm.Message)) {
	r.mu.Lock()
	r.persist = fn
	r.mu.Unlock()
}

// watchStall cancels cancel() when hb goes idle longer than idle. It stops when
// ctx is done (the turn finished or was cancelled elsewhere). The durations are
// passed in (snapshotted by the caller at run start) so a later config change
// can't race the running watchdog. Returns a func reporting whether the stall
// fired, so callers can label the outcome.
func watchStall(ctx context.Context, hb *heartbeat, cancel context.CancelFunc, idle, grace time.Duration) func() bool {
	var fired bool
	var mu sync.Mutex
	go func() {
		tick := time.NewTicker(idle / 4)
		defer tick.Stop()
		start := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if time.Since(start) < grace {
					continue
				}
				if hb.idleFor() >= idle {
					mu.Lock()
					fired = true
					mu.Unlock()
					cancel()
					return
				}
			}
		}
	}()
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		return fired
	}
}
