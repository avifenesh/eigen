package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/observe"
)

func (m *model) observeLines(h int) []string {
	pw := m.rightCols()
	contentW := pw - 2
	if contentW < 1 {
		contentW = 1
	}
	lines := []string{changesPad(m.rightPanelTitleLine(pw-2), pw)}
	path := observe.DefaultPath()
	s, err := observe.ReadSummary(path, 5000)
	if err != nil {
		msg := "no telemetry yet"
		if !os.IsNotExist(err) {
			msg = "observe error: " + err.Error()
		}
		lines = append(lines, changesPad(dim(ansiTrunc(msg, contentW)), pw))
		lines = append(lines, changesPad(dim("events land in ~/.eigen/observe/events.jsonl"), pw))
		return padObserveLines(lines, h, pw)
	}
	add := func(s string) { lines = append(lines, changesPad(s, pw)) }
	add(styleAsk.Bold(true).Render(fmt.Sprintf("events %d", s.Records)) + dim(" · ") + styleAccent.Render(fmt.Sprintf("errors %d", sumCounts(s.Errors))) + dim(" · ") + styleAccent.Render(fmt.Sprintf("tools %d", len(s.Tools))))
	if s.Routes.Routed > 0 || s.Routes.Skipped > 0 {
		add("")
		add(styleSel.Bold(true).Render("routing decisions"))
		add(styleUser.Render(fmt.Sprintf("routed %d  skipped %d", s.Routes.Routed, s.Routes.Skipped)))
		add(dim(fmt.Sprintf("model-assessed %d  orchestrator %d", s.Routes.Assessed, s.Routes.Orchestrator)))
	}
	if s.Subagents.Total() > 0 || s.Subagents.BackgroundDone > 0 || s.Subagents.RouteNotes > 0 {
		add("")
		add(styleSel.Bold(true).Render("subagents / spawns"))
		add(styleUser.Render(fmt.Sprintf("task %d/%d  group %d/%d", s.Subagents.TaskCalls, s.Subagents.TaskErrors, s.Subagents.GroupCalls, s.Subagents.GroupErrors)))
		add(styleUser.Render(fmt.Sprintf("bg done %d  route notes %d", s.Subagents.BackgroundDone, s.Subagents.RouteNotes)))
	}
	addCountSection(&lines, pw, "errors", s.Errors, styleErr.Render)
	addCountSection(&lines, pw, "route/system", s.Notes, styleAsk.Render)
	if len(s.Models) > 0 {
		add("")
		add(styleSel.Bold(true).Render("models"))
		for _, k := range sortedObserveKeys(s.Models) {
			m := s.Models[k]
			add(ansiTrunc(fmt.Sprintf("%s %d turn %d/%d tok", k, m.Turns, m.InTokens, m.OutTokens), contentW))
		}
	}
	if len(s.Skills) > 0 {
		add("")
		add(styleSel.Bold(true).Render("skills"))
		for _, k := range sortedObserveKeys(s.Skills) {
			sk := s.Skills[k]
			line := fmt.Sprintf("%s calls=%d", k, sk.Calls)
			if sk.Errors > 0 {
				line += fmt.Sprintf(" errors=%d", sk.Errors)
			}
			add(ansiTrunc(line, contentW))
		}
	}
	if len(s.Tools) > 0 {
		add("")
		add(styleSel.Bold(true).Render("tools"))
		for _, k := range sortedObserveKeys(s.Tools) {
			t := s.Tools[k]
			line := fmt.Sprintf("%s calls=%d", k, t.Calls)
			if t.Errors > 0 {
				line += fmt.Sprintf(" errors=%d", t.Errors)
			}
			add(ansiTrunc(line, contentW))
			if len(lines) >= h-5 {
				break
			}
		}
	}
	if s.Runtime.MaxMemAllocBytes > 0 || s.Runtime.MaxGoroutines > 0 {
		add("")
		add(styleSel.Bold(true).Render("runtime"))
		add(ansiTrunc(fmt.Sprintf("alloc %s · goroutines %d", observeBytes(s.Runtime.MaxMemAllocBytes), s.Runtime.MaxGoroutines), contentW))
	}
	add("")
	add(dim("full: eigen observe summary"))
	return padObserveLines(lines, h, pw)
}

func addCountSection(lines *[]string, pw int, title string, counts map[string]int, style func(...string) string) {
	items := observeCountItems(counts)
	if len(items) == 0 {
		return
	}
	*lines = append(*lines, changesPad("", pw), changesPad(styleSel.Bold(true).Render(title), pw))
	for _, it := range items {
		*lines = append(*lines, changesPad(ansiTrunc(fmt.Sprintf("%-16s %s", it.k, style(fmt.Sprintf("%d", it.v))), pw-2), pw))
	}
}

func padObserveLines(lines []string, h, pw int) []string {
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", pw))
	}
	return lines
}

type observeCount struct {
	k string
	v int
}

func observeCountItems(m map[string]int) []observeCount {
	items := make([]observeCount, 0, len(m))
	for k, v := range m {
		if v > 0 {
			items = append(items, observeCount{k, v})
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

func sortedObserveKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
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

func _observePanelNoRawContentGuard() string { return strings.TrimSpace("metadata-only") }
