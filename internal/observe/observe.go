// Package observe records a structured, append-only log of agent activity —
// tool calls, errors, notes, and turn outcomes — for long-term learning
// (feeding dreaming/memory) and debugging. It is a thin EventSink wrapper: it
// observes the existing agent event stream and writes one JSON object per line.
package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

// Record is one logged event (a flattened, durable view of agent.Event plus a
// timestamp and session id).
type Record struct {
	Time    string `json:"time"`
	Session string `json:"session,omitempty"`
	Kind    string `json:"kind"`
	Step    int    `json:"step,omitempty"`
	Tool    string `json:"tool,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
	// Bytes of the result/text, not the content itself — the log is metadata
	// for learning/observability, not a transcript (which is saved separately).
	TextLen   int `json:"text_len,omitempty"`
	ResultLen int `json:"result_len,omitempty"`
}

// Logger appends event Records to a JSONL file. Safe for concurrent use.
type Logger struct {
	mu      sync.Mutex
	f       *os.File
	session string
	enc     *json.Encoder
}

// Open creates/opens the observability log at path (parent dirs created). A
// nil Logger (path == "") is a valid no-op logger.
func Open(path, session string) (*Logger, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f, session: session, enc: json.NewEncoder(f)}, nil
}

// DefaultPath is ~/.eigen/observe/events.jsonl.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "observe", "events.jsonl")
}

// Wrap returns an EventSink that logs each event (as metadata) and then
// forwards it to next (which may be nil). A nil Logger returns next unchanged,
// so observability is zero-overhead when disabled.
func (l *Logger) Wrap(next agent.EventSink) agent.EventSink {
	if l == nil {
		return next
	}
	return func(e agent.Event) {
		l.record(e)
		if next != nil {
			next(e)
		}
	}
}

func (l *Logger) record(e agent.Event) {
	rec := Record{
		Time:      time.Now().UTC().Format(time.RFC3339),
		Session:   l.session,
		Kind:      kindName(e.Kind),
		Step:      e.Step,
		Tool:      e.ToolName,
		IsError:   e.IsError,
		TextLen:   len(e.Text),
		ResultLen: len(e.Result),
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(&rec) // best-effort; observability must never break a turn
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

func kindName(k agent.EventKind) string {
	switch k {
	case agent.EventTextDelta:
		return "text"
	case agent.EventReasoningDelta:
		return "reasoning"
	case agent.EventToolStart:
		return "tool_start"
	case agent.EventToolResult:
		return "tool_result"
	case agent.EventDone:
		return "done"
	case agent.EventNote:
		return "note"
	}
	return "unknown"
}
