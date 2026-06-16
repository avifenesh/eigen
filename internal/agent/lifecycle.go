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
	// activity BETWEEN actions before it's considered hung and cancelled.
	// Per-agent, not global — measured from the last tool start/result (or
	// stream delta). It does NOT apply while a model call is in flight (see
	// modelMaxWait): a slow non-streaming Complete() emits nothing yet is not
	// idle.
	stallIdle = 2 * time.Minute
	// modelMaxWait caps a SINGLE in-flight model call. A non-streaming
	// Complete() (e.g. Converse/opus) is silent until it returns, so the
	// between-actions stallIdle must not apply to it — but a genuinely hung
	// call still needs a ceiling. This matches the providers' own HTTP timeout
	// (5 min), so a real hang is caught here at roughly the same time the
	// transport would give up, while a slow-but-healthy inference is tolerated.
	modelMaxWait = 5 * time.Minute
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
		// Keep the in-flight model cap at least as large as the between-actions
		// budget — an explicit stallIdle larger than the default 5-min model cap
		// would otherwise make the model cap the TIGHTER of the two.
		if modelMaxWait < stallIdle {
			modelMaxWait = stallIdle
		}
	}
}

// heartbeat tracks the last time a watched agent did something (tool call or
// stream delta), and whether a model call is currently in flight. Read by the
// stall watch.
//
// inFlight is true while a model call executes (drive() is strictly sequential
// — one call at a time per agent — so it's a flag, not a counter). The stall
// watchdog applies the larger modelMaxWait budget while in flight and the
// tighter stallIdle between actions: a slow non-streaming Complete() is not
// mistaken for a hang, but a model call that never returns is still capped.
type heartbeat struct {
	mu       sync.Mutex
	last     time.Time
	inFlight bool
}

func newHeartbeat() *heartbeat { return &heartbeat{last: time.Now()} }

func (h *heartbeat) beat() {
	h.mu.Lock()
	h.last = time.Now()
	h.mu.Unlock()
}

// modelStart marks a model call as in flight (switches the watchdog to the
// modelMaxWait budget). Set semantics (not increment): a new call definitively
// means any prior one is over, so a reasoning-only turn that loops without
// emitting an event can't leak the in-flight state.
func (h *heartbeat) modelStart() {
	h.mu.Lock()
	h.inFlight = true
	h.last = time.Now()
	h.mu.Unlock()
}

// modelEnd marks a model call as finished (back to the stallIdle budget,
// measured from now).
func (h *heartbeat) modelEnd() {
	h.mu.Lock()
	h.inFlight = false
	h.last = time.Now()
	h.mu.Unlock()
}

// idle reports how long since the last activity AND whether a model call is in
// flight — the watchdog picks the budget accordingly.
func (h *heartbeat) idle() (d time.Duration, inFlight bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Since(h.last), h.inFlight
}

// activitySink wraps an OnEvent sink so every tool start/result and stream
// delta ends any in-flight model window and bumps the heartbeat (the chained
// sink still runs). Activity = the agent is making progress; silence past the
// budget (stallIdle between actions, modelMaxWait while a call is in flight) =
// hung. The in-flight window is opened separately by the onModelCall hook.
func activitySink(hb *heartbeat, chain EventSink) EventSink {
	return func(e Event) {
		switch e.Kind {
		case EventToolStart, EventToolResult, EventTextDelta, EventReasoningDelta, EventNote:
			// Real progress: a model call (if any) produced output, so end the
			// in-flight window and beat.
			hb.modelEnd()
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

// watchStall cancels cancel() when the agent goes quiet too long. It uses two
// budgets: the tight `idle` (between actions) and the larger modelMaxWait while
// a model call is in flight (a slow non-streaming Complete() is silent but not
// hung — only a call that never returns should be cancelled). It stops when ctx
// is done. The durations are snapshotted by the caller at run start so a config
// change can't race the running watchdog. Returns a func reporting whether the
// stall fired, so callers can label the outcome.
func watchStall(ctx context.Context, hb *heartbeat, cancel context.CancelFunc, idle, modelWait, grace time.Duration) func() bool {
	var fired bool
	var mu sync.Mutex
	go func() {
		// Tick fast enough to honor the tighter of the two budgets.
		tickEvery := idle / 4
		if modelWait > 0 && modelWait < idle {
			tickEvery = modelWait / 4
		}
		if tickEvery <= 0 {
			tickEvery = time.Millisecond
		}
		tick := time.NewTicker(tickEvery)
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
				since, inFlight := hb.idle()
				budget := idle
				if inFlight {
					budget = modelWait // a slow model call gets the larger cap
				}
				if since >= budget {
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
