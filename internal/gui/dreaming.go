package gui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/memory"
)

// Dreaming bridge layer. "Dreaming" is the background consolidation of memory:
// per-session rollout summaries get distilled, and MEMORY.md is periodically
// rewritten (each rewrite snapshots a timestamped .bak). This view reconstructs
// that history from local files — rollout summaries + consolidation backups —
// so the user can see what the agent distilled and diff a consolidation against
// the current memory.

// RolloutDTO is one per-session distilled rollout summary.
type RolloutDTO struct {
	Index   int    `json:"index"`
	Text    string `json:"text"`
	Outcome string `json:"outcome,omitempty"`
}

// ConsolidationDTO is one memory snapshot (a .bak), newest first. The frontend
// can request a diff of this snapshot vs the current memory via MemoryDiff.
type ConsolidationDTO struct {
	Path      string `json:"path"`
	Label     string `json:"label"`     // human timestamp parsed from the filename
	WhenMs    int64  `json:"whenMs"`    // best-effort from filename, else mtime
	Bytes     int    `json:"bytes"`
}

// DreamingScopeDTO is the dreaming history for one scope.
type DreamingScopeDTO struct {
	Scope          string             `json:"scope"`
	Rollouts       []RolloutDTO       `json:"rollouts"`
	Consolidations []ConsolidationDTO `json:"consolidations"`
	CurrentBytes   int                `json:"currentBytes"`
}

// DreamingDTO is the full dreaming snapshot: project + global.
type DreamingDTO struct {
	Project *DreamingScopeDTO `json:"project"`
	Global  *DreamingScopeDTO `json:"global"`
}

func dreamScope(s *memory.Store, scope string) *DreamingScopeDTO {
	if s == nil {
		return &DreamingScopeDTO{Scope: scope}
	}
	rawRollouts := s.RawSummaries(60)
	rollouts := make([]RolloutDTO, 0, len(rawRollouts))
	for i, r := range rawRollouts {
		t := strings.TrimSpace(r)
		if t == "" {
			continue
		}
		rollouts = append(rollouts, RolloutDTO{Index: i, Text: t, Outcome: parseOutcome(t)})
	}
	// Newest rollout first for the timeline.
	for i, j := 0, len(rollouts)-1; i < j; i, j = i+1, j-1 {
		rollouts[i], rollouts[j] = rollouts[j], rollouts[i]
	}

	baks := s.Backups()
	sort.Sort(sort.Reverse(sort.StringSlice(baks))) // newest first (timestamp suffix)
	cons := make([]ConsolidationDTO, 0, len(baks))
	for _, p := range baks {
		label, whenMs := parseBakStamp(p)
		size := 0
		if fi, err := os.Stat(p); err == nil {
			size = int(fi.Size())
			if whenMs == 0 {
				whenMs = fi.ModTime().UnixMilli()
			}
		}
		cons = append(cons, ConsolidationDTO{Path: p, Label: label, WhenMs: whenMs, Bytes: size})
	}

	return &DreamingScopeDTO{
		Scope:          scope,
		Rollouts:       rollouts,
		Consolidations: cons,
		CurrentBytes:   len(s.Read()),
	}
}

// parseOutcome pulls a leading outcome marker out of a rollout summary, if the
// distiller embedded one (success/partial/failed/skip).
func parseOutcome(s string) string {
	low := strings.ToLower(s)
	for _, o := range []string{"success", "partial", "failed", "skip"} {
		if strings.Contains(low, "outcome: "+o) || strings.Contains(low, "**"+o+"**") {
			return o
		}
	}
	return ""
}

// parseBakStamp turns MEMORY.md.20060102-150405.bak into a label + unix millis.
func parseBakStamp(path string) (label string, whenMs int64) {
	base := filepath.Base(path)
	// strip leading "MEMORY.md." and trailing ".bak"
	stamp := strings.TrimSuffix(base, ".bak")
	if i := strings.LastIndex(stamp, "."); i >= 0 {
		stamp = stamp[i+1:]
	}
	if t, err := time.ParseInLocation("20060102-150405", stamp, time.Local); err == nil {
		return t.Format("Jan 2, 15:04"), t.UnixMilli()
	}
	return stamp, 0
}

// Dreaming returns the project + global dreaming history.
func (b *Bridge) Dreaming() (*DreamingDTO, error) {
	proj, _ := memory.Open("")
	glob, _ := memory.OpenGlobal()
	return &DreamingDTO{
		Project: dreamScope(proj, "project"),
		Global:  dreamScope(glob, "global"),
	}, nil
}

// ConsolidationContent returns the raw content of a consolidation snapshot, so
// the frontend can diff it against the current memory.
func (b *Bridge) ConsolidationContent(path string) (string, error) {
	// Only serve files that look like a memory backup (defense against a path
	// the frontend shouldn't reach).
	if !strings.HasSuffix(path, ".bak") || !strings.Contains(filepath.Base(path), "MEMORY.md.") {
		return "", os.ErrPermission
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CurrentMemory returns the current MEMORY.md content for a scope (the "after"
// side of a consolidation diff).
func (b *Bridge) CurrentMemory(scope string) (string, error) {
	s, err := openScope(scope)
	if err != nil {
		return "", err
	}
	return s.Read(), nil
}
