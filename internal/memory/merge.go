package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scope merge. The canonical-root fix (gitroot.go) stops NEW fragmentation, but
// projects opened before it can already own several scopes (a worktree's hash, a
// subdir's hash, …). MergeFrom folds one such orphan scope INTO the canonical
// one, non-destructively, so a project's hard-won memory reunites in a single
// store instead of being split across stores the agent will never see together.
//
// Safety model (this is irreversible-ish data movement, so it is conservative):
//   - The destination MEMORY.md is snapshotted first (Rewrite does this).
//   - Source content is APPENDED under a dated provenance header — nothing in the
//     destination is overwritten or dropped; a later Phase 2 consolidation
//     (RECENCY WINS) reconciles any duplicates/contradictions.
//   - Ad-hoc notes + rollout summaries are COPIED (collision-safe names), then a
//     consolidation is enqueued so the merged ad-hoc notes get folded.
//   - The source scope dir is ARCHIVED (renamed to "<key>.merged-<ts>"), never
//     deleted — so a bad merge is recoverable on disk.

// MergeResult reports what a MergeFrom moved.
type MergeResult struct {
	SrcKey        string
	DstKey        string
	MemoryBytes   int    // bytes of source MEMORY.md folded in
	AdHocCopied   int    // ad-hoc notes copied
	RolloutCopied int    // rollout summaries copied
	ArchivedAs    string // the renamed source dir, "" if source was empty/removed
}

// MergeFrom folds the src scope into s (the destination, normally the canonical
// store). It is a no-op that returns (nil, nil) when src is nil, src == s, or src
// has no content. On success the source is archived and a consolidation +
// summary refresh is enqueued on the destination so the fold actually lands.
func (s *Store) MergeFrom(src *Store, when time.Time) (*MergeResult, error) {
	if s == nil || src == nil {
		return nil, nil
	}
	if filepath.Clean(s.dir) == filepath.Clean(src.dir) {
		return nil, nil // same scope — nothing to do
	}
	if s.global || src.global {
		return nil, fmt.Errorf("MergeFrom is for project scopes; use promote/demote for global")
	}
	res := &MergeResult{SrcKey: baseName(src.dir), DstKey: baseName(s.dir)}

	// 1) Fold source MEMORY.md into the destination under a provenance header.
	srcMem := strings.TrimSpace(src.Read())
	if srcMem != "" {
		dstMem := strings.TrimSpace(s.Read())
		header := fmt.Sprintf("## Merged from scope %s (%s)", res.SrcKey, when.Format("2006-01-02"))
		var b strings.Builder
		if dstMem != "" {
			b.WriteString(dstMem)
			b.WriteString("\n\n")
		}
		b.WriteString(header)
		b.WriteString("\n\n")
		b.WriteString(srcMem)
		b.WriteString("\n")
		if err := s.Rewrite(b.String()); err != nil {
			return nil, fmt.Errorf("merge memory: %w", err)
		}
		res.MemoryBytes = len(srcMem)
	}

	// 2) Copy ad-hoc notes (collision-safe) so they re-enter the dest's Phase 2.
	if n, err := copyMarkdown(src.AdHocNotesDir(), s.AdHocNotesDir(), res.SrcKey); err != nil {
		return nil, fmt.Errorf("merge ad-hoc notes: %w", err)
	} else {
		res.AdHocCopied = n
	}

	// 3) Copy rollout summaries (evidence for future consolidation).
	if n, err := copyMarkdown(src.RawDir(), s.RawDir(), res.SrcKey); err != nil {
		return nil, fmt.Errorf("merge rollouts: %w", err)
	} else {
		res.RolloutCopied = n
	}

	// 4) Archive the source scope (rename — recoverable, never deleted).
	if res.MemoryBytes > 0 || res.AdHocCopied > 0 || res.RolloutCopied > 0 {
		archived, err := archiveDir(src.dir, when)
		if err != nil {
			return nil, fmt.Errorf("archive source scope: %w", err)
		}
		res.ArchivedAs = archived
	}

	// 5) Enqueue consolidation + summary on the destination so the folded ad-hoc
	//    notes get reconciled and the injected summary refreshes.
	if res.AdHocCopied > 0 || res.MemoryBytes > 0 {
		s.enqueueMaintenance()
	}
	return res, nil
}

// MergeByKey folds the scope with on-disk key srcKey into dstKey. The manual /
// deterministic path: used by the GUI + the memory tool, and for orphan scopes
// whose source directory is gone (a deleted worktree) so the canonical root can
// no longer be computed from a live path. Both keys must be existing project
// scopes (not "global").
func MergeByKey(srcKey, dstKey string, when time.Time) (*MergeResult, error) {
	if srcKey == dstKey {
		return nil, fmt.Errorf("source and destination scope are the same")
	}
	src, err := OpenByKey(srcKey)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", srcKey, err)
	}
	dst, err := OpenByKey(dstKey)
	if err != nil {
		return nil, fmt.Errorf("destination %q: %w", dstKey, err)
	}
	if _, statErr := os.Stat(src.dir); statErr != nil {
		return nil, fmt.Errorf("source scope %q does not exist", srcKey)
	}
	return dst.MergeFrom(src, when)
}

// ReconcileScopes auto-heals pre-existing fragmentation for a set of known
// project directories (e.g. the daemon's persisted-session dirs). For each dir
// whose LEGACY cwd-derived key differs from its CANONICAL git-root key, and
// whose legacy scope still exists on disk with content, it folds the legacy
// scope into the canonical one. Dirs whose canonical root can't be resolved
// (non-git, or a now-deleted worktree) are left alone — their legacy key equals
// their canonical key, so there is nothing to merge automatically; use the
// manual MergeByKey path for those. Returns the merges performed.
func ReconcileScopes(knownDirs []string, when time.Time) ([]MergeResult, error) {
	base, err := baseDir()
	if err != nil {
		return nil, err
	}
	var out []MergeResult
	seen := map[string]bool{} // legacyKey already handled this pass
	for _, dir := range knownDirs {
		if dir == "" {
			continue
		}
		legacyAbs, aerr := filepath.Abs(dir)
		if aerr != nil {
			continue
		}
		legacyKey := key(legacyAbs)
		if seen[legacyKey] {
			continue
		}
		canonical := canonicalProjectDir(dir)
		if canonical == "" {
			continue
		}
		canonicalKey := key(canonical)
		if legacyKey == canonicalKey {
			continue // not fragmented (or canonical unresolvable — same key)
		}
		// Only merge when the legacy scope actually exists with content and is
		// distinct from the (live) canonical scope.
		legacyDir := filepath.Join(base, legacyKey)
		if fi, serr := os.Stat(legacyDir); serr != nil || !fi.IsDir() {
			continue
		}
		seen[legacyKey] = true
		dst, derr := OpenByKey(canonicalKey)
		if derr != nil {
			continue
		}
		src, serr := OpenByKey(legacyKey)
		if serr != nil {
			continue
		}
		r, merr := dst.MergeFrom(src, when)
		if merr != nil {
			return out, merr
		}
		if r != nil {
			out = append(out, *r)
		}
	}
	return out, nil
}

// copyMarkdown copies every *.md from srcDir into dstDir, prefixing each name
// with "merged-<tag>-" to avoid clobbering a same-named file already there.
// Returns the count copied. A missing srcDir is not an error (0 copied).
func copyMarkdown(srcDir, dstDir, tag string) (int, error) {
	matches, _ := filepath.Glob(filepath.Join(srcDir, "*.md"))
	if len(matches) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, err
	}
	copied := 0
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		name := "merged-" + tag + "-" + filepath.Base(m)
		dst := filepath.Join(dstDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue // already merged in a prior run — idempotent
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// archiveDir renames dir to "<dir>.merged-<ts>" so the merged scope is retained
// on disk (recoverable) but no longer enumerated as a live project store.
func archiveDir(dir string, when time.Time) (string, error) {
	archived := dir + ".merged-" + when.Format("20060102-150405")
	if err := os.Rename(dir, archived); err != nil {
		return "", err
	}
	return archived, nil
}
