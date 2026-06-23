package gui

import (
	"sort"

	"github.com/avifenesh/eigen/internal/observe"
)

// Observe bridge layer. The live KPIs come from the daemon stats stream; this
// adds the historical observability summary read from the local metadata-only
// log (~/.eigen/observe/events.jsonl) — tool usage, models/tokens, routes,
// hooks, errors, subagents. Maps are flattened to sorted slices for clean TS
// binding.

type ToolStatDTO struct {
	Name       string `json:"name"`
	Calls      int    `json:"calls"`
	Errors     int    `json:"errors"`
	DurationMS int64  `json:"durationMs"`
}
type ModelStatDTO struct {
	Name             string `json:"name"`
	Turns            int    `json:"turns"`
	InTokens         int    `json:"inTokens"`
	OutTokens        int    `json:"outTokens"`
	CacheReadTokens  int    `json:"cacheReadTokens"`
	CacheWriteTokens int    `json:"cacheWriteTokens"`
	DurationMS       int64  `json:"durationMs"`
}
type HookStatDTO struct {
	Name       string `json:"name"`
	Starts     int    `json:"starts"`
	Done       int    `json:"done"`
	Errors     int    `json:"errors"`
	DurationMS int64  `json:"durationMs"`
}
type CountDTO struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type RouteStatsDTO struct {
	Routed       int        `json:"routed"`
	Skipped      int        `json:"skipped"`
	Assessed     int        `json:"assessed"`
	Orchestrator int        `json:"orchestrator"`
	ByModel      []CountDTO `json:"byModel"`
	ByKind       []CountDTO `json:"byKind"`
	ByDifficulty []CountDTO `json:"byDifficulty"`
	SkipReasons  []CountDTO `json:"skipReasons"`
}
type SubagentStatsDTO struct {
	TaskCalls      int `json:"taskCalls"`
	TaskErrors     int `json:"taskErrors"`
	GroupCalls     int `json:"groupCalls"`
	GroupErrors    int `json:"groupErrors"`
	MutatingCalls  int `json:"mutatingCalls"`
	MutatingErrors int `json:"mutatingErrors"`
	BackgroundDone int `json:"backgroundDone"`
}

// ObserveSummaryDTO is the historical observability summary for the dashboard's
// sub-views (routes / tools / models / hooks / errors).
type ObserveSummaryDTO struct {
	Records   int              `json:"records"`
	ByKind    []CountDTO       `json:"byKind"`
	Tools     []ToolStatDTO    `json:"tools"`
	Models    []ModelStatDTO   `json:"models"`
	Hooks     []HookStatDTO    `json:"hooks"`
	Errors    []CountDTO       `json:"errors"`
	Routes    RouteStatsDTO    `json:"routes"`
	Subagents SubagentStatsDTO `json:"subagents"`
	Available bool             `json:"available"` // false when no log exists yet
}

func sortedCounts(m map[string]int) []CountDTO {
	out := make([]CountDTO, 0, len(m))
	for k, v := range m {
		out = append(out, CountDTO{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ObserveSummary returns the historical observability summary (metadata-only
// log). Available=false (not an error) when no log exists yet.
func (b *Bridge) ObserveSummary(limit int) (*ObserveSummaryDTO, error) {
	if limit <= 0 {
		limit = 5000
	}
	s, err := observe.ReadSummary(observe.DefaultPath(), limit)
	if err != nil {
		// No log yet is the common first-run case — report unavailable, not error.
		return &ObserveSummaryDTO{Available: false}, nil
	}

	tools := make([]ToolStatDTO, 0, len(s.Tools))
	for name, t := range s.Tools {
		tools = append(tools, ToolStatDTO{Name: name, Calls: t.Calls, Errors: t.Errors, DurationMS: t.DurationMS})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Calls != tools[j].Calls {
			return tools[i].Calls > tools[j].Calls
		}
		return tools[i].Name < tools[j].Name
	})

	models := make([]ModelStatDTO, 0, len(s.Models))
	for name, m := range s.Models {
		models = append(models, ModelStatDTO{
			Name: name, Turns: m.Turns, InTokens: m.InTokens, OutTokens: m.OutTokens,
			CacheReadTokens: m.CacheReadTokens, CacheWriteTokens: m.CacheWriteTokens, DurationMS: m.DurationMS,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Turns != models[j].Turns {
			return models[i].Turns > models[j].Turns
		}
		return models[i].Name < models[j].Name
	})

	hooks := make([]HookStatDTO, 0, len(s.Hooks))
	for name, h := range s.Hooks {
		hooks = append(hooks, HookStatDTO{Name: name, Starts: h.Starts, Done: h.Done, Errors: h.Errors, DurationMS: h.DurationMS})
	}
	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Name < hooks[j].Name })

	return &ObserveSummaryDTO{
		Records: s.Records,
		ByKind:  sortedCounts(s.ByKind),
		Tools:   tools,
		Models:  models,
		Hooks:   hooks,
		Errors:  sortedCounts(s.Errors),
		Routes: RouteStatsDTO{
			Routed: s.Routes.Routed, Skipped: s.Routes.Skipped, Assessed: s.Routes.Assessed,
			Orchestrator: s.Routes.Orchestrator,
			ByModel:      sortedCounts(s.Routes.ByModel),
			ByKind:       sortedCounts(s.Routes.ByKind),
			ByDifficulty: sortedCounts(s.Routes.ByDifficulty),
			SkipReasons:  sortedCounts(s.Routes.SkipReasons),
		},
		Subagents: SubagentStatsDTO{
			TaskCalls: s.Subagents.TaskCalls, TaskErrors: s.Subagents.TaskErrors,
			GroupCalls: s.Subagents.GroupCalls, GroupErrors: s.Subagents.GroupErrors,
			MutatingCalls: s.Subagents.MutatingCalls, MutatingErrors: s.Subagents.MutatingErrors,
			BackgroundDone: s.Subagents.BackgroundDone,
		},
		Available: true,
	}, nil
}
