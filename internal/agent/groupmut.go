package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
)

// MutApprover is asked once, at apply time, to approve mutating the real
// workspace with the combined diff. Returns true to apply. nil ⇒ deny (fail
// closed). Injected so the agent package doesn't depend on the TUI.
type MutApprover func(ctx context.Context, summary string, diff []byte) (bool, error)

// mutResult is one implementer child's outcome.
type mutResult struct {
	idx     int
	task    string
	sub     GroupSubtask // original spec, for rebase (re-run on the merged state)
	where   string
	answer  string
	patch   []byte
	err     error
	dur     time.Duration
	rebased bool // patch was re-derived on top of others' changes
}

// TaskGroupMutating runs implementer children in PARALLEL, each in its own
// isolated git worktree, then validates their combined patch set in a throwaway
// worktree and — behind ONE approval — applies the clean result to the real
// workspace. Isolation + patch-merge is what makes parallel writes safe.
//
// Returns an infrastructure error (refusal) when the repo isn't safe for this
// (not git / not repo root / dirty / unborn HEAD / disabled). Otherwise returns
// a report describing each child, which patches applied, and any conflicts.
func (a *Agent) TaskGroupMutating(ctx context.Context, subs []GroupSubtask, workers int, approve MutApprover) (string, error) {
	if a.WorktreeTools == nil {
		return "", fmt.Errorf("mutating fan-out is unavailable in this build")
	}
	if len(subs) == 0 {
		return "", fmt.Errorf("task_group_mutating needs at least one subtask")
	}
	if len(subs) > maxGroupChildren {
		return "", fmt.Errorf("task_group_mutating: too many subtasks (%d > %d)", len(subs), maxGroupChildren)
	}
	depth, _ := ctx.Value(subtaskDepthKey{}).(int)
	if depth >= maxSubtaskDepth {
		return "", fmt.Errorf("subtask depth limit (%d) reached", maxSubtaskDepth)
	}
	for i, s := range subs {
		if strings.TrimSpace(s.Task) == "" {
			return "", fmt.Errorf("task_group_mutating: subtask %d has an empty task", i+1)
		}
	}

	dir := a.SessionDir
	if dir == "" {
		return "", fmt.Errorf("mutating fan-out needs a session directory")
	}
	base, err := precheckMutatingFanout(ctx, dir)
	if err != nil {
		return "", err
	}

	parent, err := mkTempWorktreeParent()
	if err != nil {
		return "", fmt.Errorf("worktree setup: %w", err)
	}
	defer os.RemoveAll(parent)

	gctx, cancel := context.WithTimeout(ctx, groupTotalTimeout)
	defer cancel()
	gctx = context.WithValue(gctx, subtaskDepthKey{}, depth+1)

	if workers <= 0 {
		workers = defaultGroupWorkers
	}
	if workers > maxGroupWorkers {
		workers = maxGroupWorkers
	}
	if workers > len(subs) {
		workers = len(subs)
	}

	// Track worktrees for guaranteed cleanup.
	var wtMu sync.Mutex
	var worktrees []string
	defer func() {
		for _, wt := range worktrees {
			if err := removeWorktree(context.Background(), base.root, wt); err != nil {
				fmt.Fprintf(os.Stderr, "eigen: worktree cleanup %s: %v\n", wt, err)
			}
		}
	}()

	sem := make(chan struct{}, workers)
	results := make([]mutResult, len(subs))
	var wg sync.WaitGroup

	for i, s := range subs {
		select {
		case <-gctx.Done():
			results[i] = mutResult{idx: i, task: s.Task, err: gctx.Err()}
			continue
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int, s GroupSubtask) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[i] = mutResult{idx: i, task: s.Task, err: fmt.Errorf("panic: %v", r)}
				}
			}()
			start := time.Now()

			wt, err := addWorktree(gctx, base.root, parent, fmt.Sprintf("child-%d", i+1), base.head)
			if err != nil {
				results[i] = mutResult{idx: i, task: s.Task, err: fmt.Errorf("worktree: %w", err)}
				return
			}
			wtMu.Lock()
			worktrees = append(worktrees, wt)
			wtMu.Unlock()

			// The implementer child: tools rooted at ITS worktree, PermAuto
			// (its writes are sandboxed to the worktree — nothing real is
			// touched until the parent applies), no Approve.
			sub, cwhere := a.implementerChild(wt, s)
			cctx, ccancel := context.WithTimeout(gctx, groupChildTimeout)
			defer ccancel()
			answer, aerr := sub.NewSession().Send(cctx, s.Task)

			patch, perr := capturePatch(gctx, wt, base.head)
			if perr != nil && aerr == nil {
				aerr = perr
			}
			results[i] = mutResult{idx: i, task: s.Task, sub: s, where: cwhere, answer: answer, patch: patch, err: aerr, dur: time.Since(start)}
		}(i, s)
	}
	wg.Wait()

	return a.mergeAndApply(gctx, base, parent, results, approve)
}

// implementerChild builds a sub-agent whose tools are rooted at the worktree
// (mutating, but confined), PermAuto, no Approve. Model selection still honors
// role-less routing/override via subAgent semantics but with the worktree
// toolset substituted.
func (a *Agent) implementerChild(wt string, s GroupSubtask) (*Agent, string) {
	prov := a.provider()
	compactor := a.compactor()
	where := ""
	if s.Model != "" && a.ModelProvider != nil {
		if p, err := a.ModelProvider(s.Model); err == nil {
			prov, compactor, where = p, llm.NewCompactor(p), "running on "+s.Model+" (explicit)"
		}
	}
	if prov == a.provider() && a.Router != nil {
		if rp, _, label := a.Router(context.Background(), s.Task, s.Kind, s.Difficulty, false); rp != nil {
			prov, compactor, where = rp, llm.NewCompactor(rp), label
		}
	}
	extra := implementerSystem
	if a.ExtraSystem != "" {
		extra = implementerSystem + "\n\n" + a.ExtraSystem
	}
	sub := &Agent{
		Provider:         prov,
		Tools:            a.WorktreeTools(wt),
		Perm:             PermAuto, // sandboxed to the worktree; nothing real until apply
		MaxSteps:         a.MaxSteps,
		MaxContextTokens: a.maxContextTokens(),
		Compactor:        compactor,
		ExtraSystem:      extra,
		Memory:           a.Memory,
		Router:           a.Router,
		ModelProvider:    a.ModelProvider,
	}
	return sub, where
}

// appliedPatch tracks whether a child's patch landed in the validation worktree.
type appliedPatch struct {
	idx      int
	patch    []byte
	ok       bool
	conflict bool
	rebased  bool
}

// mergeAndApply validates the children's patches in a throwaway worktree (in
// deterministic input order), asks for ONE approval on the clean combined diff,
// re-checks the baseline, applies to the real tree, and returns the report.
func (a *Agent) mergeAndApply(ctx context.Context, base *repoState, parent string, results []mutResult, approve MutApprover) (string, error) {
	// Validation worktree off the same base: patches are applied here in
	// deterministic input order to build the combined result, never touching
	// the real tree until approved.
	valWt, err := addWorktree(ctx, base.root, parent, "merge", base.head)
	if err != nil {
		return "", fmt.Errorf("merge worktree: %w", err)
	}
	defer func() { _ = removeWorktree(context.Background(), base.root, valWt) }()

	apps := make([]appliedPatch, 0, len(results))
	for ri := range results {
		r := &results[ri]
		if r.err != nil || len(r.patch) == 0 {
			continue
		}
		// Clean apply onto the accumulated merge state.
		if applyCheck(ctx, valWt, r.patch) && applyPatch(ctx, valWt, r.patch) == nil {
			apps = append(apps, appliedPatch{idx: r.idx, patch: r.patch, ok: true})
			continue
		}
		// Conflict: REBASE by redo — re-run the child on top of what's already
		// merged, so its work is recovered (not dropped) without conflict
		// markers. A nil result/error means the rebase couldn't recover it.
		if newPatch := a.rebaseChild(ctx, base, parent, valWt, r); len(newPatch) > 0 &&
			applyCheck(ctx, valWt, newPatch) && applyPatch(ctx, valWt, newPatch) == nil {
			r.patch = newPatch
			r.rebased = true
			apps = append(apps, appliedPatch{idx: r.idx, patch: newPatch, ok: true, rebased: true})
		} else {
			apps = append(apps, appliedPatch{idx: r.idx, conflict: true})
		}
	}

	// The combined diff is what the validation worktree now holds vs base.
	combined, derr := capturePatch(ctx, valWt, base.head)
	if derr != nil {
		return "", fmt.Errorf("combine: %w", derr)
	}
	cleanCount := 0
	for _, ap := range apps {
		if ap.ok {
			cleanCount++
		}
	}

	report := formatMutReport(results, apps)

	if cleanCount == 0 || len(combined) == 0 {
		return report + "\n\nNo changes applied (no clean patches).", nil
	}
	if len(combined) > maxCombinedBytes {
		return report + "\n\nCombined diff too large to apply automatically.", nil
	}

	// ONE approval, at apply time, on the exact combined diff.
	if approve != nil {
		summary := fmt.Sprintf("apply %d implementer patch(es) from the fan-out to the workspace", cleanCount)
		ok, err := approve(ctx, summary, combined)
		if err != nil {
			return report + "\n\nApply denied: " + err.Error(), nil
		}
		if !ok {
			return report + "\n\nApply denied by user — workspace unchanged.", nil
		}
	}

	// Re-check the baseline didn't move under us, then apply to the real tree.
	if cur, err := gitText(ctx, base.root, "rev-parse", "HEAD"); err != nil || cur != base.head {
		return report + "\n\nWorkspace HEAD moved during the fan-out — not applying (re-run on the new base).", nil
	}
	if status, _ := gitText(ctx, base.root, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		return report + "\n\nWorkspace became dirty during the fan-out — not applying.", nil
	}
	if err := applyPatch(ctx, base.root, combined); err != nil {
		return report + "\n\nFinal apply failed (no partial changes — the tree was clean): " + err.Error(), nil
	}
	return report + fmt.Sprintf("\n\nApplied %d patch(es) to the workspace. Review with `git diff`, then commit.", cleanCount), nil
}

// rebaseChild recovers a conflicting child by RE-RUNNING it on top of the
// already-merged state (the semantic equivalent of git rebase resolving by
// redoing the change). It snapshots the merge worktree's current tree as a temp
// commit, spins a fresh implementer worktree from that commit, re-runs the
// child with context that its peers already landed changes, and returns the new
// diff (against the snapshot). Returns nil if it can't recover (snapshot/spawn
// failure, the child produced nothing, or the redo still doesn't apply).
func (a *Agent) rebaseChild(ctx context.Context, base *repoState, parent, valWt string, r *mutResult) []byte {
	// Snapshot the current merged state as a commit in the merge worktree
	// (detached HEAD there, so this touches no shared branch). git apply has
	// already staged nothing reliably, so add everything explicitly.
	if _, err := gitText(ctx, valWt, "add", "-A"); err != nil {
		return nil
	}
	snap, err := gitText(ctx, valWt, "commit", "-q", "-m", "fanout-merge-snapshot", "--no-verify")
	_ = snap
	if err != nil {
		// Nothing staged (no prior patches) means there was no real conflict to
		// rebase onto — give up cleanly.
		return nil
	}
	snapSHA, err := gitText(ctx, valWt, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}

	rebaseWt, err := addWorktree(ctx, base.root, parent, fmt.Sprintf("rebase-%d", r.idx+1), snapSHA)
	if err != nil {
		return nil
	}
	defer func() { _ = removeWorktree(context.Background(), base.root, rebaseWt) }()

	redo := r.sub
	redo.Task = r.sub.Task + "\n\n[NOTE: other parallel workers have already modified the repository since you started. You are now working on top of their changes. Re-apply YOUR change cleanly on this updated codebase — read the current state of any file you touch first, and integrate rather than overwrite.]"
	sub, _ := a.implementerChild(rebaseWt, redo)
	cctx, cancel := context.WithTimeout(ctx, groupChildTimeout)
	defer cancel()
	if _, err := sub.NewSession().Send(cctx, redo.Task); err != nil {
		return nil
	}
	patch, err := capturePatch(ctx, rebaseWt, snapSHA)
	if err != nil || len(patch) == 0 {
		return nil
	}
	return patch
}

func formatMutReport(results []mutResult, apps []appliedPatch) string {
	applyState := map[int]string{}
	for _, ap := range apps {
		switch {
		case ap.ok && ap.rebased:
			applyState[ap.idx] = "applied (rebased onto the others' changes)"
		case ap.ok:
			applyState[ap.idx] = "applied"
		case ap.conflict:
			applyState[ap.idx] = "conflict — couldn't rebase (skipped)"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "task_group_mutating: %d implementer(s)\n", len(results))
	for _, r := range results {
		b.WriteString("\n")
		label := fmt.Sprintf("[%d] implementer", r.idx+1)
		if r.where != "" {
			label += " (" + r.where + ")"
		}
		if r.dur > 0 {
			label += fmt.Sprintf(" · %s", r.dur.Round(time.Second))
		}
		b.WriteString(label + "\n")
		switch {
		case r.err != nil:
			b.WriteString("  error: " + r.err.Error() + "\n")
		case len(r.patch) == 0:
			b.WriteString("  no changes\n")
		default:
			st := applyState[r.idx]
			if st == "" {
				st = "captured"
			}
			b.WriteString("  " + st + "; " + PatchStat(r.patch) + "\n")
		}
		if r.answer != "" {
			for _, line := range strings.Split(strings.TrimRight(oneScreen(r.answer), "\n"), "\n") {
				b.WriteString("    " + line + "\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// PatchStat summarizes a patch as "N files, +A −D" (exported for the apply prompt).
func PatchStat(patch []byte) string {
	s := string(patch)
	files, adds, dels := 0, 0, 0
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			adds++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			dels++
		}
	}
	return fmt.Sprintf("%d file(s), +%d −%d", files, adds, dels)
}

// oneScreen caps a child's answer to a short excerpt for the report. Counts
// runes (not bytes) so a multibyte rune is never split into invalid UTF-8.
func oneScreen(s string) string {
	const max = 600
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
