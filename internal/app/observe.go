package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/observe"
	tea "github.com/charmbracelet/bubbletea"
)

// observeState is the app command-center view over metadata-only telemetry:
// route behavior, subagent/tool failures, model/token usage, hooks and runtime
// stress. It deliberately renders aggregates, not transcript content.
type observeState struct{ list list }

func (s *observeState) init(d *Data) { s.sync(d) }

func (s *observeState) sync(d *Data) {
	count := 6 // hero + section headers/details baseline
	if d != nil {
		count += len(nonZeroCounts(d.Observe.ByKind))
		count += len(nonZeroCounts(d.Observe.Errors))
		count += len(nonZeroCounts(d.Observe.Notes))
		count += len(d.Observe.Models)
		count += len(d.Observe.Tools)
		count += len(d.Observe.Hooks)
		count += len(d.Observe.Skills)
	}
	if count < 1 {
		count = 1
	}
	s.list.count = count
}

func (s *observeState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.sync(m.data)
	s.list.key(msg.String(), m.height-6)
	return m, nil
}

func (s *observeState) view(m *Model, w, h int) string {
	s.sync(m.data)
	d := m.data
	out := pageTitle("observe", "metadata-only telemetry", w)
	if d.ObserveErr != "" {
		out += sErr.Render("  unavailable: "+truncate(d.ObserveErr, w-17)) + "\n"
		return out
	}
	obs := d.Observe
	out += observeHero(obs, w)
	out += observeSubagents(obs, w)
	out += observeCounts("errors", obs.Errors, w, func(v string) string { return sErr.Render(v) })
	out += observeCounts("route / system notes", obs.Notes, w, func(v string) string { return sAccent.Render(v) })
	out += observeModels(obs, w)
	out += observeSkills(obs, w)
	out += observeTools(obs, w)
	out += observeHooks(obs, w)
	out += observeRuntime(obs, w)
	out += "\n" + sFaint.Render("  source: "+truncate(d.ObservePath, max(10, w-12)))
	out += "\n" + sFaint.Render("  CLI: eigen observe summary --limit=5000")
	return out
}

func observeHero(s observe.Summary, w int) string {
	cards := []string{
		fmt.Sprintf("events %d", s.Records),
		fmt.Sprintf("models %d", len(s.Models)),
		fmt.Sprintf("tools %d", len(s.Tools)),
		fmt.Sprintf("skills %d", len(s.Skills)),
		fmt.Sprintf("errors %d", countTotal(s.Errors)),
	}
	if s.Subagents.Total() > 0 {
		cards = append(cards, fmt.Sprintf("subagents %d", s.Subagents.Total()))
	}
	var parts []string
	for _, c := range cards {
		parts = append(parts, sViolet.Render(c))
	}
	line := "  " + strings.Join(parts, sFaint.Render("  ·  "))
	return truncate(line, w) + "\n\n"
}

func observeSubagents(s observe.Summary, w int) string {
	sg := s.Subagents
	if sg.Total() == 0 {
		return ""
	}
	out := "  " + sectionLabel("subagents / spawning", min(w, 70)-2) + "\n"
	rows := []struct {
		name          string
		calls, errors int
	}{
		{"task", sg.TaskCalls, sg.TaskErrors},
		{"task_group", sg.GroupCalls, sg.GroupErrors},
		{"task_group_mutating", sg.MutatingCalls, sg.MutatingErrors},
		{"task_status", sg.StatusChecks, 0},
		{"task_promote", sg.Promotes, sg.PromoteErrors},
	}
	for _, r := range rows {
		if r.calls == 0 && r.errors == 0 {
			continue
		}
		status := sOk.Render(fmt.Sprintf("%d", r.calls))
		if r.errors > 0 {
			status = sErr.Render(fmt.Sprintf("%d err / %d", r.errors, r.calls))
		}
		out += "  " + pad(r.name, 22) + status + "\n"
	}
	if sg.BackgroundDone > 0 || sg.BackgroundNotes > 0 || sg.RouteNotes > 0 {
		out += "  " + sDim.Render(fmt.Sprintf("background done %d · background notes %d · route notes %d", sg.BackgroundDone, sg.BackgroundNotes, sg.RouteNotes)) + "\n"
	}
	return out + "\n"
}

func observeCounts(title string, m map[string]int, w int, style func(string) string) string {
	items := nonZeroCounts(m)
	if len(items) == 0 {
		return ""
	}
	out := "  " + sectionLabel(title, min(w, 70)-2) + "\n"
	for _, it := range items {
		out += "  " + pad(it.k, 24) + style(fmt.Sprintf("%d", it.v)) + "\n"
	}
	return out + "\n"
}

func observeModels(s observe.Summary, w int) string {
	if len(s.Models) == 0 {
		return ""
	}
	out := "  " + sectionLabel("model / token usage", min(w, 70)-2) + "\n"
	for _, k := range sortedKeys(s.Models) {
		m := s.Models[k]
		line := fmt.Sprintf("%s turns=%d tokens=%d/%d cache=%d/%d avg=%dms", pad(truncate(k, 28), 28), m.Turns, m.InTokens, m.OutTokens, m.CacheReadTokens, m.CacheWriteTokens, avg64(m.DurationMS, m.Turns))
		out += "  " + truncate(line, w-2) + "\n"
	}
	return out + "\n"
}

func observeSkills(s observe.Summary, w int) string {
	if len(s.Skills) == 0 {
		return ""
	}
	out := "  " + sectionLabel("skill invocations", min(w, 70)-2) + "\n"
	for _, k := range sortedKeys(s.Skills) {
		sk := s.Skills[k]
		status := sOk.Render(fmt.Sprintf("calls=%d", sk.Calls))
		if sk.Errors > 0 {
			status = sErr.Render(fmt.Sprintf("calls=%d errors=%d", sk.Calls, sk.Errors))
		}
		out += "  " + pad(truncate(k, 26), 28) + status + sDim.Render(fmt.Sprintf(" avg=%dms", avg64(sk.DurationMS, sk.Calls))) + "\n"
	}
	return out + "\n"
}

func observeTools(s observe.Summary, w int) string {
	if len(s.Tools) == 0 {
		return ""
	}
	out := "  " + sectionLabel("tool calls", min(w, 70)-2) + "\n"
	for _, k := range sortedKeys(s.Tools) {
		t := s.Tools[k]
		status := sOk.Render(fmt.Sprintf("calls=%d", t.Calls))
		if t.Errors > 0 {
			status = sErr.Render(fmt.Sprintf("calls=%d errors=%d", t.Calls, t.Errors))
		}
		out += "  " + pad(truncate(k, 24), 24) + status + sDim.Render(fmt.Sprintf(" avg=%dms", avg64(t.DurationMS, t.Calls))) + "\n"
	}
	return out + "\n"
}

func observeHooks(s observe.Summary, w int) string {
	if len(s.Hooks) == 0 {
		return ""
	}
	out := "  " + sectionLabel("hooks", min(w, 70)-2) + "\n"
	for _, k := range sortedKeys(s.Hooks) {
		h := s.Hooks[k]
		status := sOk.Render(fmt.Sprintf("done=%d", h.Done))
		if h.Errors > 0 {
			status = sErr.Render(fmt.Sprintf("done=%d errors=%d", h.Done, h.Errors))
		}
		out += "  " + pad(truncate(k, 24), 24) + status + sDim.Render(fmt.Sprintf(" start=%d avg=%dms", h.Starts, avg64(h.DurationMS, h.Done))) + "\n"
	}
	return out + "\n"
}

func observeRuntime(s observe.Summary, w int) string {
	r := s.Runtime
	if r.MaxMemAllocBytes == 0 && r.MaxGoroutines == 0 {
		return ""
	}
	out := "  " + sectionLabel("runtime stress", min(w, 70)-2) + "\n"
	line := fmt.Sprintf("alloc %s · heap_inuse %s · heap_sys %s · goroutines %d", observeBytes(r.MaxMemAllocBytes), observeBytes(r.MaxHeapInuseBytes), observeBytes(r.MaxHeapSysBytes), r.MaxGoroutines)
	return out + "  " + truncate(line, w-2) + "\n\n"
}

type countItem struct {
	k string
	v int
}

func nonZeroCounts(m map[string]int) []countItem {
	items := make([]countItem, 0, len(m))
	for k, v := range m {
		if v > 0 {
			items = append(items, countItem{k, v})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v == items[j].v {
			return items[i].k < items[j].k
		}
		return items[i].v > items[j].v
	})
	return items
}

func countTotal(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func avg64(n int64, d int) int64 {
	if d <= 0 {
		return 0
	}
	return n / int64(d)
}

func observeBytes(n uint64) string {
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
