// Package hook runs user-configured commands on agent lifecycle events
// (session start/stop/resume, tool calls, turn done, notes). Hooks are the
// extension seam: a hook is any program the user writes; eigen's job is only to
// EXPOSE the events and feed each hook a small JSON payload on stdin. Hooks are
// fire-and-forget and best-effort — a failing or slow hook never blocks a turn.
package hook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

// Event names exposed to hooks. Stable strings (config references them).
const (
	OnSessionStart  = "session_start"
	OnSessionStop   = "session_stop"
	OnSessionResume = "session_resume"
	OnToolStart     = "tool_start"
	OnToolResult    = "tool_result"
	OnTurnDone      = "turn_done"
	OnNote          = "note"
)

// Spec is one configured hook: run Command when Event fires. Command is argv
// (no shell); the payload JSON is written to the process's stdin.
type Spec struct {
	Event    string   `json:"event"`
	Command  []string `json:"command"`
	Disabled bool     `json:"disabled,omitempty"` // kept in config, not fired
}

// Payload is the JSON handed to a hook on stdin.
type Payload struct {
	Event   string `json:"event"`
	Session string `json:"session,omitempty"`
	Tool    string `json:"tool,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
	Step    int    `json:"step,omitempty"`
	Text    string `json:"text,omitempty"`
}

// Observation is metadata about hook execution. It intentionally omits the raw
// command and stderr/stdout; observers get a hash/argc/status for correlation
// without leaking hook payloads or paths into the activity log.
type Observation struct {
	Event       string
	Phase       string // "start" | "done"
	Session     string
	CommandHash string
	Argc        int
	Duration    time.Duration
	Err         error
}

type Observer func(Observation)

// hookTimeout bounds a hook process so a hung hook can't leak forever.
const hookTimeout = 30 * time.Second

// Runner dispatches events to the hooks registered for them.
type Runner struct {
	byEvent map[string][]Spec
	observe Observer
}

// SetObserver registers a best-effort hook execution observer (observability).
func (r *Runner) SetObserver(o Observer) {
	if r != nil {
		r.observe = o
	}
}

// New builds a Runner from specs (ignoring malformed ones: empty event or
// command). A nil/empty Runner is a valid no-op.
func New(specs []Spec) *Runner {
	m := map[string][]Spec{}
	for _, s := range specs {
		if s.Event == "" || len(s.Command) == 0 || s.Disabled {
			continue
		}
		m[s.Event] = append(m[s.Event], s)
	}
	if len(m) == 0 {
		return nil
	}
	return &Runner{byEvent: m}
}

// Fire runs every hook registered for p.Event, fire-and-forget. Safe on a nil
// Runner.
func (r *Runner) Fire(p Payload) {
	if r == nil {
		return
	}
	specs := r.byEvent[p.Event]
	if len(specs) == 0 {
		return
	}
	data, _ := json.Marshal(p)
	for _, s := range specs {
		obs := r.observe
		go runOne(s.Command, data, p, obs)
	}
}

func runOne(argv []string, stdin []byte, p Payload, obs Observer) {
	start := time.Now()
	if obs != nil {
		obs(Observation{Event: p.Event, Phase: "start", Session: p.Session, CommandHash: commandHash(argv), Argc: len(argv)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	err := cmd.Run() // best-effort: a hook must never break a turn
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if obs != nil {
		obs(Observation{Event: p.Event, Phase: "done", Session: p.Session, CommandHash: commandHash(argv), Argc: len(argv), Duration: time.Since(start), Err: err})
	}
}

func commandHash(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(argv, "\x00")))
	return fmt.Sprintf("sha256:%x", sum[:8])
}

// Wrap composes hook firing onto an agent.EventSink: it fires the matching
// tool/turn/note hooks, then forwards the event to next. Session lifecycle
// hooks are fired explicitly (see Fire) since they originate outside the loop.
func (r *Runner) Wrap(next agent.EventSink, session string) agent.EventSink {
	if r == nil {
		return next
	}
	return func(e agent.Event) {
		switch e.Kind {
		case agent.EventToolStart:
			r.Fire(Payload{Event: OnToolStart, Session: session, Tool: e.ToolName, Step: e.Step})
		case agent.EventToolResult:
			r.Fire(Payload{Event: OnToolResult, Session: session, Tool: e.ToolName, IsError: e.IsError, Step: e.Step})
		case agent.EventDone:
			r.Fire(Payload{Event: OnTurnDone, Session: session})
		case agent.EventNote:
			r.Fire(Payload{Event: OnNote, Session: session, Text: e.Text})
		}
		if next != nil {
			next(e)
		}
	}
}

// Load reads a hooks config file: a JSON array of Spec, or an object
// {"hooks":[...]}. Missing file → nil Runner (no hooks), not an error.
func Load(path string) (*Runner, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, nil // absent/unreadable config = no hooks
	}
	var specs []Spec
	if err := json.Unmarshal(data, &specs); err != nil {
		var wrap struct {
			Hooks []Spec `json:"hooks"`
		}
		if err2 := json.Unmarshal(data, &wrap); err2 != nil {
			return nil, err // malformed
		}
		specs = wrap.Hooks
	}
	return New(specs), nil
}

// readFile is os.ReadFile (indirected for testability).
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
