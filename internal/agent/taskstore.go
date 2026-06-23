package agent

// The background-task DISK STORE (Tier 12). The tasks dir (~/.eigen/tasks) is
// the observability surface: every state change of a background task is one
// appended JSON line in <id>.jsonl, so any process — the daemon that runs the
// task, a TUI window in another process, a CLI — reads the same truth without
// a wire protocol. This file is the read side's single implementation: robust
// last-line parsing (a reader can catch the writer mid-append), lost detection
// (a "running" record whose host process died), the cancel-marker protocol,
// and retention pruning. Writers stay in background.go (BgRegistry.put).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// bgIDRe constrains task ids used in file paths (cancel markers arrive from
// other processes; never let an id traverse paths).
var bgIDRe = regexp.MustCompile(`^bg-[0-9]+-[0-9]+$`)

// ValidTaskID reports whether id is a well-formed background-task id (the same
// constraint readers use before joining it into a file path). Exported so other
// packages (e.g. the GUI bridge) can reject path-traversal ids before any
// filesystem access, rather than re-deriving the pattern.
func ValidTaskID(id string) bool {
	return bgIDRe.MatchString(id)
}

// lostGrace is how much past bgMaxRuntime a "running" record may age before it
// is considered lost even when pid liveness can't be checked (old records
// without a pid, or another host).
const lostGrace = 5 * time.Minute

// LoadBgTasks reads every task's current state from dir: the last complete
// JSON line of each <id>.jsonl (transcript files excluded). Stale "running"
// records are surfaced as "lost" (read-side interpretation; the durable mark
// happens in NewBgRegistry's adoption pass). Markers pending cancellation set
// Canceling. Sorted running-first, then most recently started. Missing dir or
// malformed files are skipped, never fatal: this is an observability read.
func LoadBgTasks(dir string) []BgTask {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []BgTask
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".transcript.jsonl") {
			continue
		}
		t, ok := readTaskFile(filepath.Join(dir, name))
		if !ok {
			continue
		}
		if t.Status == "running" {
			if taskLost(t) {
				t.Status = "lost"
			} else if _, err := os.Stat(filepath.Join(dir, t.ID+".cancel")); err == nil {
				t.Canceling = true
			}
		}
		out = append(out, t)
	}
	sortBgTasks(out)
	return out
}

// sortBgTasks orders running tasks first, then by recency.
func sortBgTasks(out []BgTask) {
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Status == "running") != (out[j].Status == "running") {
			return out[i].Status == "running"
		}
		return out[i].Started.After(out[j].Started)
	})
}

// readTaskFile parses the LAST COMPLETE valid JSON line of a task state file.
// The writer appends whole lines, but a reader can still observe a partial
// final line (mid-append, or after a crash) — scan backward to the newest
// line that parses; ignore the rest.
func readTaskFile(path string) (BgTask, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return BgTask{}, false
	}
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		ln := bytes.TrimSpace(lines[i])
		if len(ln) == 0 {
			continue
		}
		var t BgTask
		if json.Unmarshal(ln, &t) == nil && t.ID != "" {
			return t, true
		}
	}
	// Compatibility fallback: if a future/old writer ever stores a single
	// whole-file JSON object (possibly pretty-printed) rather than JSONL, keep
	// task_status from making the task disappear.
	var t BgTask
	if json.Unmarshal(bytes.TrimSpace(data), &t) == nil && t.ID != "" {
		return t, true
	}
	return BgTask{}, false
}

// readTaskHistory parses every complete valid JSON line of a task state file in
// append order. Malformed/partial lines are ignored; readers use this for
// verbose task_status attempt timelines while still being robust to mid-append
// observations.
func readTaskHistory(path string) []BgTask {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	lines := bytes.Split(data, []byte("\n"))
	out := make([]BgTask, 0, len(lines))
	for _, raw := range lines {
		ln := bytes.TrimSpace(raw)
		if len(ln) == 0 {
			continue
		}
		var t BgTask
		if json.Unmarshal(ln, &t) == nil && t.ID != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		// Compatibility fallback for a single pretty-printed JSON object.
		var t BgTask
		if json.Unmarshal(bytes.TrimSpace(data), &t) == nil && t.ID != "" {
			out = append(out, t)
		}
	}
	return out
}

// ReadTaskHistory returns a task's full append-only state trail (every
// recorded state change in order: attempt 1, escalation, attempt 2, terminal)
// straight from the disk store under dir, without needing a live BgRegistry.
// The id is validated against the same constraint as cancel markers so a
// caller's id can never traverse paths. A missing/empty/malformed file yields
// an empty slice (never fatal — this is an observability read); only a bad id
// is an error.
func ReadTaskHistory(dir, id string) ([]BgTask, error) {
	if !bgIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid task id %q", id)
	}
	return readTaskHistory(filepath.Join(dir, id+".jsonl")), nil
}

// taskLost reports whether a persisted "running" record's task is actually
// gone. Pid liveness is checked when the record carries one for this host;
// otherwise (old records, other hosts) age beyond the hard runtime cap +
// grace is decisive — bgMaxRuntime guarantees no legitimate task runs longer.
func taskLost(t BgTask) bool {
	if !t.Started.IsZero() && time.Since(t.Started) > bgMaxRuntime+lostGrace {
		return true
	}
	if t.Pid > 0 && sameHost(t.Host) {
		return !pidAlive(t.Pid)
	}
	return false
}

func sameHost(h string) bool {
	if h == "" {
		return true // pre-Host records: assume local
	}
	cur, err := os.Hostname()
	return err == nil && cur == h
}

// pidAlive probes a pid with signal 0. EPERM means "exists but not ours" —
// alive. Only ESRCH (no such process) is dead.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// RequestCancel asks a running background task (possibly hosted by ANOTHER
// process) to stop, by dropping a <id>.cancel marker the hosting goroutine
// polls. Returns an error for malformed ids or tasks that are not running
// (per the disk record), so stale markers are never created for finished
// work.
func RequestCancel(dir, id string) error {
	if !bgIDRe.MatchString(id) {
		return fmt.Errorf("invalid task id %q", id)
	}
	t, ok := readTaskFile(filepath.Join(dir, id+".jsonl"))
	if !ok {
		return fmt.Errorf("no such background task: %s", id)
	}
	if t.Status != "running" {
		return fmt.Errorf("task %s is %s — nothing to cancel", id, t.Status)
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".cancel"), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// adoptStale durably marks lost tasks and prunes old terminal ones. Called
// from NewBgRegistry (i.e. on daemon/session start): any persisted "running"
// record whose process is gone gets one appended "lost" line (so every later
// reader agrees without re-deriving), and terminal tasks older than the
// retention window are removed (state, transcript, and marker files).
// Pid-guarded: tasks of LIVE processes (e.g. another daemon) are untouched.
func (r *BgRegistry) adoptStale() {
	const retention = 7 * 24 * time.Hour
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".transcript.jsonl") {
			continue
		}
		path := filepath.Join(r.dir, name)
		t, ok := readTaskFile(path)
		if !ok {
			continue
		}
		if t.Status == "running" && taskLost(t) {
			t.Status = "lost"
			t.Updated = time.Now()
			r.put(&t)
		}
		if t.Status != "running" && t.Status != "" {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > retention {
				os.Remove(path)
				os.Remove(filepath.Join(r.dir, t.ID+".transcript.jsonl"))
				os.Remove(filepath.Join(r.dir, t.ID+".cancel"))
			}
		}
	}
}
