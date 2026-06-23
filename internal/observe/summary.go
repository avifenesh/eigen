package observe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Summary is an aggregate view over events.jsonl. It is intentionally compact:
// counts and resource maxima, no transcript/content.
type Summary struct {
	Records   int
	ByKind    map[string]int
	Tools     map[string]ToolSummary
	Errors    map[string]int
	Notes     map[string]int
	Hooks     map[string]HookSummary
	Skills    map[string]SkillSummary
	Models    map[string]ModelSummary
	Routes    RouteSummary
	Subagents SubagentSummary
	Runtime   RuntimeSummary
}

type ToolSummary struct {
	Calls      int
	Errors     int
	DurationMS int64
}

type HookSummary struct {
	Starts     int
	Done       int
	Errors     int
	DurationMS int64
}

type SkillSummary struct {
	Calls      int
	Errors     int
	DurationMS int64
}

type ModelSummary struct {
	Turns            int
	InTokens         int
	OutTokens        int
	CacheReadTokens  int
	CacheWriteTokens int
	DurationMS       int64
}

type RouteSummary struct {
	Routed       int
	Skipped      int
	Assessed     int
	Orchestrator int
	ByModel      map[string]int
	ByKind       map[string]int
	ByDifficulty map[string]int
	SkipReasons  map[string]int
}

type SubagentSummary struct {
	TaskCalls       int
	TaskErrors      int
	GroupCalls      int
	GroupErrors     int
	MutatingCalls   int
	MutatingErrors  int
	StatusChecks    int
	Promotes        int
	PromoteErrors   int
	BackgroundDone  int
	BackgroundNotes int
	RouteNotes      int
}

func (s SubagentSummary) Total() int {
	return s.TaskCalls + s.GroupCalls + s.MutatingCalls + s.StatusChecks + s.Promotes
}

type RuntimeSummary struct {
	MaxMemAllocBytes  uint64
	MaxHeapInuseBytes uint64
	MaxHeapSysBytes   uint64
	MaxGoroutines     int
}

// ReadSummary reads up to the last limit records from path (0 = all) and returns
// aggregate observability stats. Malformed/partial JSONL lines are ignored.
func ReadSummary(path string, limit int) (Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return Summary{}, err
	}
	defer f.Close()

	// Tail-read: rather than parse a multi-million-line log fully into memory
	// just to keep the last `limit` records, seek to a window near the end
	// sized to the record cap (records are small JSON lines) and parse only
	// that. Bounds both time and memory regardless of total file size. The
	// window grows from the end so we always cover at least `limit` records.
	var start int64
	if limit > 0 {
		if fi, statErr := f.Stat(); statErr == nil {
			// ~512 bytes/record is a generous upper bound for these metadata
			// lines; cap the window so a pathological file can't blow memory.
			const maxWindow = 64 << 20 // 64 MiB
			window := int64(limit) * 512
			if window > maxWindow {
				window = maxWindow
			}
			if fi.Size() > window {
				start = fi.Size() - window
				if _, err := f.Seek(start, 0); err != nil {
					start = 0
					_, _ = f.Seek(0, 0)
				}
			}
		}
	}

	var records []Record
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)
	first := true
	for sc.Scan() {
		// When we seeked into the middle of the file, the first line is almost
		// certainly a partial record — skip it so we start on a clean boundary.
		if first {
			first = false
			if start > 0 {
				continue
			}
		}
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Kind != "" {
			records = append(records, r)
		}
	}
	if err := sc.Err(); err != nil {
		return Summary{}, err
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return summarize(records), nil
}

func summarize(records []Record) Summary {
	s := Summary{
		ByKind: map[string]int{},
		Tools:  map[string]ToolSummary{},
		Errors: map[string]int{},
		Notes:  map[string]int{},
		Hooks:  map[string]HookSummary{},
		Skills: map[string]SkillSummary{},
		Models: map[string]ModelSummary{},
		Routes: RouteSummary{ByModel: map[string]int{}, ByKind: map[string]int{}, ByDifficulty: map[string]int{}, SkipReasons: map[string]int{}},
	}
	for _, r := range records {
		s.Records++
		s.ByKind[r.Kind]++
		if r.IsError {
			key := r.ErrorKind
			if key == "" {
				key = "error"
			}
			s.Errors[key]++
		}
		if r.Tool != "" && r.Kind == "tool_result" {
			st := s.Tools[r.Tool]
			st.Calls++
			if r.IsError {
				st.Errors++
			}
			st.DurationMS += r.DurationMS
			s.Tools[r.Tool] = st
			accumulateSubagentTool(&s.Subagents, r)
			accumulateSkill(&s, r)
		}
		if r.NoteKind != "" {
			s.Notes[r.NoteKind]++
			if r.NoteKind == "background" {
				s.Subagents.BackgroundNotes++
			}
			if r.NoteKind == "route" {
				s.Subagents.RouteNotes++
				accumulateRoute(&s.Routes, r)
			}
		}
		if r.Kind == "background_done" {
			s.Subagents.BackgroundDone++
		}
		if strings.HasPrefix(r.Kind, "hook_") {
			key := r.HookEvent
			if key == "" {
				key = "hook"
			}
			h := s.Hooks[key]
			if r.Kind == "hook_start" {
				h.Starts++
			}
			if r.Kind == "hook_done" {
				h.Done++
				h.DurationMS += r.DurationMS
				if r.IsError {
					h.Errors++
				}
			}
			s.Hooks[key] = h
		}
		if r.Kind == "done" {
			key := r.Model
			if key == "" {
				key = r.Provider
			}
			if key == "" {
				key = "unknown"
			}
			m := s.Models[key]
			m.Turns++
			m.InTokens += r.InTokens
			m.OutTokens += r.OutTokens
			m.CacheReadTokens += r.CacheReadTokens
			m.CacheWriteTokens += r.CacheWriteTokens
			m.DurationMS += r.DurationMS
			s.Models[key] = m
		}
		if r.MemAllocBytes > s.Runtime.MaxMemAllocBytes {
			s.Runtime.MaxMemAllocBytes = r.MemAllocBytes
		}
		if r.HeapInuseBytes > s.Runtime.MaxHeapInuseBytes {
			s.Runtime.MaxHeapInuseBytes = r.HeapInuseBytes
		}
		if r.HeapSysBytes > s.Runtime.MaxHeapSysBytes {
			s.Runtime.MaxHeapSysBytes = r.HeapSysBytes
		}
		if r.Goroutines > s.Runtime.MaxGoroutines {
			s.Runtime.MaxGoroutines = r.Goroutines
		}
	}
	return s
}

func accumulateRoute(s *RouteSummary, r Record) {
	switch r.RouteStatus {
	case "routed":
		s.Routed++
		if r.RouteModel != "" {
			s.ByModel[r.RouteModel]++
		}
		if r.RouteKind != "" {
			s.ByKind[r.RouteKind]++
		}
		if r.RouteDifficulty != "" {
			s.ByDifficulty[r.RouteDifficulty]++
		}
		if r.RouteAssessor == "" || r.RouteAssessor == "orchestrator" {
			s.Orchestrator++
		} else {
			s.Assessed++
		}
	case "skipped":
		s.Skipped++
		reason := r.RouteSkipReason
		if reason == "" {
			reason = "unknown"
		}
		s.SkipReasons[reason]++
	}
}

func accumulateSkill(s *Summary, r Record) {
	if r.Tool != "skill" || r.Skill == "" {
		return
	}
	st := s.Skills[r.Skill]
	st.Calls++
	if r.IsError {
		st.Errors++
	}
	st.DurationMS += r.DurationMS
	s.Skills[r.Skill] = st
}

func accumulateSubagentTool(s *SubagentSummary, r Record) {
	switch r.Tool {
	case "task":
		s.TaskCalls++
		if r.IsError {
			s.TaskErrors++
		}
	case "task_group":
		s.GroupCalls++
		if r.IsError {
			s.GroupErrors++
		}
	case "task_group_mutating":
		s.MutatingCalls++
		if r.IsError {
			s.MutatingErrors++
		}
	case "task_status":
		s.StatusChecks++
	case "task_promote":
		s.Promotes++
		if r.IsError {
			s.PromoteErrors++
		}
	}
}

func FormatSummary(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "observability summary — %d record(s)\n", s.Records)
	writeTop(&b, "events", s.ByKind, 8)
	writeTop(&b, "errors", s.Errors, 8)
	writeTop(&b, "notes", s.Notes, 8)
	if s.Routes.Routed > 0 || s.Routes.Skipped > 0 {
		b.WriteString("\nrouting decisions:\n")
		fmt.Fprintf(&b, "  routed=%d skipped=%d assessed=%d orchestrator=%d\n", s.Routes.Routed, s.Routes.Skipped, s.Routes.Assessed, s.Routes.Orchestrator)
		writeInlineCounts(&b, "  models", s.Routes.ByModel)
		writeInlineCounts(&b, "  kinds", s.Routes.ByKind)
		writeInlineCounts(&b, "  difficulty", s.Routes.ByDifficulty)
		writeInlineCounts(&b, "  skips", s.Routes.SkipReasons)
	}
	if len(s.Skills) > 0 {
		b.WriteString("\nskills:\n")
		for _, k := range sortedKeys(s.Skills) {
			st := s.Skills[k]
			fmt.Fprintf(&b, "  %s  calls=%d errors=%d avg_ms=%d\n", k, st.Calls, st.Errors, safeDiv64(st.DurationMS, st.Calls))
		}
	}
	if s.Subagents.Total() > 0 || s.Subagents.BackgroundDone > 0 || s.Subagents.RouteNotes > 0 {
		fmt.Fprintf(&b, "\nsubagents/spawns:\n")
		fmt.Fprintf(&b, "  task=%d/%d task_group=%d/%d mutating=%d/%d status=%d promote=%d/%d bg_done=%d bg_notes=%d route_notes=%d\n",
			s.Subagents.TaskCalls, s.Subagents.TaskErrors,
			s.Subagents.GroupCalls, s.Subagents.GroupErrors,
			s.Subagents.MutatingCalls, s.Subagents.MutatingErrors,
			s.Subagents.StatusChecks,
			s.Subagents.Promotes, s.Subagents.PromoteErrors,
			s.Subagents.BackgroundDone, s.Subagents.BackgroundNotes, s.Subagents.RouteNotes)
	}
	if len(s.Models) > 0 {
		b.WriteString("\nmodels:\n")
		for _, k := range sortedKeys(s.Models) {
			m := s.Models[k]
			fmt.Fprintf(&b, "  %s  turns=%d tokens=%d/%d cache=%d/%d avg_ms=%d\n", k, m.Turns, m.InTokens, m.OutTokens, m.CacheReadTokens, m.CacheWriteTokens, safeDiv64(m.DurationMS, m.Turns))
		}
	}
	if len(s.Tools) > 0 {
		b.WriteString("\ntools:\n")
		for _, k := range sortedKeys(s.Tools) {
			t := s.Tools[k]
			fmt.Fprintf(&b, "  %s  calls=%d errors=%d avg_ms=%d\n", k, t.Calls, t.Errors, safeDiv64(t.DurationMS, t.Calls))
		}
	}
	if len(s.Hooks) > 0 {
		b.WriteString("\nhooks:\n")
		for _, k := range sortedKeys(s.Hooks) {
			h := s.Hooks[k]
			fmt.Fprintf(&b, "  %s  start=%d done=%d errors=%d avg_ms=%d\n", k, h.Starts, h.Done, h.Errors, safeDiv64(h.DurationMS, h.Done))
		}
	}
	if s.Runtime.MaxMemAllocBytes > 0 || s.Runtime.MaxGoroutines > 0 {
		fmt.Fprintf(&b, "\nruntime max: alloc=%s heap_inuse=%s heap_sys=%s goroutines=%d\n", bytesHuman(s.Runtime.MaxMemAllocBytes), bytesHuman(s.Runtime.MaxHeapInuseBytes), bytesHuman(s.Runtime.MaxHeapSysBytes), s.Runtime.MaxGoroutines)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeInlineCounts(b *strings.Builder, title string, m map[string]int) {
	if len(m) == 0 {
		return
	}
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	fmt.Fprintf(b, "%s: %s\n", title, strings.Join(parts, " "))
}

func writeTop(b *strings.Builder, title string, m map[string]int, n int) {
	if len(m) == 0 {
		return
	}
	b.WriteString("\n" + title + ":\n")
	keys := sortedKeys(m)
	if n > 0 && len(keys) > n {
		keys = keys[:n]
	}
	for _, k := range keys {
		fmt.Fprintf(b, "  %s  %d\n", k, m[k])
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func safeDiv64(n int64, d int) int64 {
	if d <= 0 {
		return 0
	}
	return n / int64(d)
}

func bytesHuman(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
