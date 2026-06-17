// Package observe records a structured, append-only log of agent activity —
// tool calls, errors, notes, and turn outcomes — for long-term learning
// (feeding dreaming/memory) and debugging. It is a thin EventSink wrapper: it
// observes the existing agent event stream and writes one JSON object per line.
package observe

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/hook"
)

// Record is one logged event (a flattened, durable view of agent.Event plus a
// timestamp and session id).
type Record struct {
	Time     string `json:"time"`
	Session  string `json:"session,omitempty"`
	Kind     string `json:"kind"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Step     int    `json:"step,omitempty"`
	Tool     string `json:"tool,omitempty"`
	ToolID   string `json:"tool_id,omitempty"`
	Skill    string `json:"skill,omitempty"`
	IsError  bool   `json:"is_error,omitempty"`

	// DurationMS is filled for tool_result (time since matching tool_start) and
	// done (time since the first observed event in the turn) when known.
	DurationMS int64 `json:"duration_ms,omitempty"`
	// ErrorKind is a coarse, content-free classifier for failing tool calls.
	ErrorKind string `json:"error_kind,omitempty"`
	// ErrorHash lets repeated failures be correlated without storing the error
	// text itself. The log remains metadata-only.
	ErrorHash string `json:"error_hash,omitempty"`
	// NoteKind classifies EventNote text (route/compaction/background/goal/etc.)
	// without storing the text itself.
	NoteKind        string `json:"note_kind,omitempty"`
	HookEvent       string `json:"hook_event,omitempty"`
	HookPhase       string `json:"hook_phase,omitempty"`
	HookCommandHash string `json:"hook_command_hash,omitempty"`
	HookArgc        int    `json:"hook_argc,omitempty"`

	// Bytes of the result/text, not the content itself — the log is metadata
	// for learning/observability, not a transcript (which is saved separately).
	TextLen   int `json:"text_len,omitempty"`
	ResultLen int `json:"result_len,omitempty"`

	InTokens         int `json:"in_tokens,omitempty"`
	OutTokens        int `json:"out_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`

	// Runtime samples are attached only to milestone/error records, not every
	// text delta, so observability can catch memory stress/leaks without adding
	// material overhead to streaming.
	MemAllocBytes  uint64 `json:"mem_alloc_bytes,omitempty"`
	HeapInuseBytes uint64 `json:"heap_inuse_bytes,omitempty"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes,omitempty"`
	Goroutines     int    `json:"goroutines,omitempty"`
	NumGC          uint32 `json:"num_gc,omitempty"`
}

// Logger appends event Records to a JSONL file. Safe for concurrent use.
type Logger struct {
	mu                sync.Mutex
	f                 *os.File
	session           string
	enc               *json.Encoder
	toolStart         map[string]time.Time
	skillStart        map[string]string
	turnStart         time.Time
	lastRuntimeSample time.Time
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
	return &Logger{f: f, session: session, enc: json.NewEncoder(f), toolStart: map[string]time.Time{}, skillStart: map[string]string{}}, nil
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
	now := time.Now()
	rec := Record{
		Time:             now.UTC().Format(time.RFC3339),
		Session:          l.session,
		Kind:             kindName(e.Kind),
		Provider:         e.Provider,
		Model:            e.Model,
		Step:             e.Step,
		Tool:             e.ToolName,
		ToolID:           e.ToolID,
		IsError:          e.IsError,
		TextLen:          len(e.Text),
		ResultLen:        len(e.Result),
		InTokens:         e.InTokens,
		OutTokens:        e.OutTokens,
		CacheReadTokens:  e.CacheReadTokens,
		CacheWriteTokens: e.CacheWriteTokens,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.turnStart.IsZero() {
		l.turnStart = now
	}
	switch e.Kind {
	case agent.EventToolStart:
		key := toolKey(e)
		l.toolStart[key] = now
		if e.ToolName == "skill" {
			rec.Skill = skillNameFromArgs(e.ToolArgs)
			if rec.Skill != "" {
				l.skillStart[key] = rec.Skill
			}
		}
	case agent.EventToolResult:
		key := toolKey(e)
		if started, ok := l.toolStart[key]; ok {
			rec.DurationMS = now.Sub(started).Milliseconds()
			delete(l.toolStart, key)
		}
		if skillName, ok := l.skillStart[key]; ok {
			rec.Skill = skillName
			delete(l.skillStart, key)
		}
		if e.IsError {
			rec.ErrorKind = classifyError(e.Result)
			rec.ErrorHash = hashText(e.Result)
		}
	case agent.EventDone:
		if !l.turnStart.IsZero() {
			rec.DurationMS = now.Sub(l.turnStart).Milliseconds()
		}
		l.turnStart = time.Time{}
		l.toolStart = map[string]time.Time{}
		l.skillStart = map[string]string{}
	case agent.EventNote:
		rec.NoteKind = classifyNote(e.Text)
	}
	if l.shouldSampleRuntime(now, e) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		rec.MemAllocBytes = ms.Alloc
		rec.HeapInuseBytes = ms.HeapInuse
		rec.HeapSysBytes = ms.HeapSys
		rec.Goroutines = runtime.NumGoroutine()
		rec.NumGC = ms.NumGC
		l.lastRuntimeSample = now
	}
	_ = l.enc.Encode(&rec) // best-effort; observability must never break a turn
}

func toolKey(e agent.Event) string {
	if e.ToolID != "" {
		return e.ToolID
	}
	return fmt.Sprintf("%d:%s", e.Step, e.ToolName)
}

func skillNameFromArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var in struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	return strings.TrimSpace(in.Name)
}

func (l *Logger) shouldSampleRuntime(now time.Time, e agent.Event) bool {
	if e.Kind != agent.EventToolResult && e.Kind != agent.EventDone && e.Kind != agent.EventBgDone {
		return false
	}
	if e.Kind == agent.EventDone {
		return true
	}
	return l.lastRuntimeSample.IsZero() || now.Sub(l.lastRuntimeSample) >= time.Second
}

func classifyError(s string) string {
	p := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(p, "unknown tool"):
		return "unknown_tool"
	case strings.HasPrefix(p, "denied:") || strings.Contains(p, "not approved"):
		return "denied"
	case strings.Contains(p, "panic") || strings.Contains(p, "panicked"):
		return "panic"
	case strings.Contains(p, "context deadline") || strings.Contains(p, "context canceled") || strings.Contains(p, "deadline exceeded"):
		return "context"
	case strings.Contains(p, "no such file") || strings.Contains(p, "not found"):
		return "not_found"
	case strings.Contains(p, "permission denied"):
		return "permission"
	default:
		return "tool_error"
	}
}

func classifyNote(s string) string {
	p := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(p, "routed") || strings.Contains(p, "route skipped") || strings.HasPrefix(p, "task →"):
		return "route"
	case strings.Contains(p, "context auto") || strings.Contains(p, "compact"):
		return "compaction"
	case strings.Contains(p, "background task") || strings.Contains(p, "moved to background"):
		return "background"
	case strings.Contains(p, "goal"):
		return "goal"
	case strings.Contains(p, "error") || strings.Contains(p, "failed"):
		return "error"
	default:
		return "note"
	}
}

// HookObserver returns a hook.Observer that logs hook start/done metadata. A nil
// Logger returns nil so callers can pass it unconditionally.
func (l *Logger) HookObserver() hook.Observer {
	if l == nil {
		return nil
	}
	return func(o hook.Observation) { l.recordHook(o) }
}

func (l *Logger) recordHook(o hook.Observation) {
	now := time.Now()
	rec := Record{
		Time:            now.UTC().Format(time.RFC3339),
		Session:         firstNonEmpty(o.Session, l.session),
		Kind:            "hook_" + o.Phase,
		HookEvent:       o.Event,
		HookPhase:       o.Phase,
		HookCommandHash: o.CommandHash,
		HookArgc:        o.Argc,
		DurationMS:      o.Duration.Milliseconds(),
	}
	if o.Err != nil {
		rec.IsError = true
		rec.ErrorKind = classifyError(o.Err.Error())
		rec.ErrorHash = hashText(o.Err.Error())
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(&rec)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func hashText(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("sha256:%x", sum[:8])
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
	case agent.EventApproval:
		return "approval"
	case agent.EventBgDone:
		return "background_done"
	}
	return "unknown"
}
