package memory

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Forgetting (the mem_forget tier). The append-only evidence tiers grow without
// bound: rollout_summaries/ gains a file per reflected session (observed: 100+
// in a busy scope after ~9 days) and retired ad-hoc notes accumulate. None of
// these are injected, so they don't bloat the prompt — but they grow disk and
// slow the globs that feed consolidation. PruneEvidence keeps the recent tail
// and ARCHIVES the rest (moved under a dated archive dir, never deleted), so a
// scope's working set stays bounded while history remains recoverable.
//
// This is deliberately AGE + COUNT based, not "usage" based: the usage_count /
// last_used columns in the index are not yet wired to a real consult signal, so
// demoting on them would act on noise. Age + a recency cap are signals that are
// actually true today.

const (
	// keepRecentRollouts is how many newest rollout summaries a scope retains
	// live. Phase 2 reads at most 64 as evidence (RawSummaries/Phase2Inputs), so
	// a tail comfortably above that loses nothing consolidation would use.
	keepRecentRollouts = 80
	// rolloutMaxAge archives rollout summaries older than this regardless of
	// count — old session evidence has almost certainly been consolidated.
	rolloutMaxAge = 45 * 24 * time.Hour
	// retiredMaxAge archives folded ad-hoc notes out of retired/ after this long;
	// by then any contradiction they encoded has been reconciled into MEMORY.md.
	retiredMaxAge = 90 * 24 * time.Hour
)

// ForgetResult reports what a PruneEvidence sweep moved to the archive.
type ForgetResult struct {
	RolloutsArchived int
	RetiredArchived  int
}

// PruneEvidence archives stale, already-absorbed evidence for this scope:
// rollout summaries beyond the recent cap or older than rolloutMaxAge, and
// retired ad-hoc notes older than retiredMaxAge. Files are MOVED under
// <dir>/archive/ (recoverable), never deleted. Best-effort; a per-file failure
// is skipped. No-op for the global scope's profile (it has no rollout tier of
// note) beyond whatever evidence it happens to hold.
func (s *Store) PruneEvidence(now time.Time) ForgetResult {
	if s == nil {
		return ForgetResult{}
	}
	var res ForgetResult
	res.RolloutsArchived = s.pruneRollouts(now)
	res.RetiredArchived = s.pruneRetiredAdHoc(now)
	return res
}

// pruneRollouts archives rollout summaries past the recency cap or age bound.
func (s *Store) pruneRollouts(now time.Time) int {
	matches, _ := filepath.Glob(filepath.Join(s.RawDir(), "*.md"))
	sort.Strings(matches) // timestamp-prefixed → chronological, oldest first
	if len(matches) == 0 {
		return 0
	}
	dst := filepath.Join(s.RawDir(), "archive")
	// Index of the first file to KEEP by the count cap: everything before this
	// (the oldest) is a candidate to archive.
	keepFrom := 0
	if len(matches) > keepRecentRollouts {
		keepFrom = len(matches) - keepRecentRollouts
	}
	cutoff := now.Add(-rolloutMaxAge)
	archived := 0
	for i, m := range matches {
		// Archive if it's beyond the count cap OR older than the age bound. Files
		// within the recent cap are kept regardless (recent evidence), unless they
		// are genuinely ancient by mtime.
		old := false
		if fi, err := os.Stat(m); err == nil {
			old = fi.ModTime().Before(cutoff)
		}
		if i >= keepFrom && !old {
			continue
		}
		if archiveFile(m, dst) {
			archived++
		}
	}
	return archived
}

// pruneRetiredAdHoc archives folded ad-hoc notes older than retiredMaxAge.
func (s *Store) pruneRetiredAdHoc(now time.Time) int {
	matches, _ := filepath.Glob(filepath.Join(s.RetiredAdHocDir(), "*.md"))
	if len(matches) == 0 {
		return 0
	}
	dst := filepath.Join(s.RetiredAdHocDir(), "archive")
	cutoff := now.Add(-retiredMaxAge)
	archived := 0
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil || !fi.ModTime().Before(cutoff) {
			continue
		}
		if archiveFile(m, dst) {
			archived++
		}
	}
	return archived
}

// archiveFile moves src into dstDir (creating it), disambiguating name clashes.
// Returns whether the move succeeded.
func archiveFile(src, dstDir string) bool {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return false
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if _, err := os.Stat(dst); err == nil {
		dst = uniqueRetiredPath(dst)
	}
	return os.Rename(src, dst) == nil
}
