package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// Multi-agent fan-out (Tier 16 v1): run several READ-ONLY sub-agents in
// parallel and join their results into one report. Read-only is enforced
// mechanically (every child's toolset must pass Registry.AllReadOnly), which is
// what makes parallelism safe: a read-only child never calls Approve (no
// racing the single approval prompt) and never writes the workspace (no
// concurrent-edit corruption). Mutating/parallel-write fan-out is deferred
// until isolated per-child workspaces exist.

const (
	maxGroupChildren    = 8                // hard cap on subtasks per group
	defaultGroupWorkers = 3                // default concurrency
	maxGroupWorkers     = 6                // cap on concurrency
	groupChildTimeout   = 5 * time.Minute  // per-child wall-clock bound
	groupTotalTimeout   = 12 * time.Minute // whole-group bound
	maxGroupResultBytes = 8000             // per-child result cap in the report
)

// GroupSubtask is one child of a fan-out: a task plus optional role/routing.
type GroupSubtask struct {
	Task       string
	Role       string
	Kind       string
	Difficulty string
	Model      string
}

// childResult is one child's outcome, kept in input order for a stable report.
type childResult struct {
	idx    int
	role   string
	where  string
	result string
	err    error
	dur    time.Duration
}

// TaskGroup runs subs in parallel (bounded) and returns a combined, stable
// report. Infrastructure failures (no subtasks, too many, a child that demands
// a mutating toolset, parent cancellation) are returned as an error — those
// are the orchestrator's mistakes to fix. An individual child's failure is NOT
// an error: it's recorded in the report so the others still land.
func (a *Agent) TaskGroup(ctx context.Context, subs []GroupSubtask, workers int, synthesize string) (string, error) {
	if len(subs) == 0 {
		return "", fmt.Errorf("task_group needs at least one subtask")
	}
	if len(subs) > maxGroupChildren {
		return "", fmt.Errorf("task_group: too many subtasks (%d > %d)", len(subs), maxGroupChildren)
	}
	depth, _ := ctx.Value(subtaskDepthKey{}).(int)
	if depth >= maxSubtaskDepth {
		return "", fmt.Errorf("subtask depth limit (%d) reached", maxSubtaskDepth)
	}
	// Validate every child up front (fail closed before launching anything):
	// a role must be known and read-only, and the resulting toolset must be
	// entirely read-only.
	for i, s := range subs {
		if strings.TrimSpace(s.Task) == "" {
			return "", fmt.Errorf("task_group: subtask %d has an empty task", i+1)
		}
		if s.Role == "" {
			return "", fmt.Errorf("task_group: subtask %d needs a role (%s) — parallel children must be read-only", i+1, strings.Join(taskGroupRoleNames(), "/"))
		}
		role, ok := LookupRole(s.Role)
		if !ok {
			return "", fmt.Errorf("task_group: subtask %d has unknown role %q (available: %s)", i+1, s.Role, strings.Join(taskGroupRoleNames(), "/"))
		}
		if !role.ReadOnly || !a.Tools.Subset(role.Tools...).AllReadOnly() {
			return "", fmt.Errorf("task_group: role %q is not read-only — parallel mutating subtasks are not supported yet", s.Role)
		}
	}

	if workers <= 0 {
		workers = defaultGroupWorkers
	}
	if workers > maxGroupWorkers {
		workers = maxGroupWorkers
	}
	if workers > len(subs) {
		workers = len(subs)
	}

	// Group-level deadline + child depth bump, derived from the PARENT ctx so
	// cancelling the parent turn stops the fan-out.
	gctx, cancel := context.WithTimeout(ctx, groupTotalTimeout)
	defer cancel()
	gctx = context.WithValue(gctx, subtaskDepthKey{}, depth+1)

	sem := make(chan struct{}, workers)
	results := make([]childResult, len(subs))
	var wg sync.WaitGroup

	for i, s := range subs {
		select {
		case <-gctx.Done():
			// Parent cancelled / group timed out before launching the rest.
			results[i] = childResult{idx: i, role: s.Role, err: gctx.Err()}
			continue
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int, s GroupSubtask) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[i] = childResult{idx: i, role: s.Role, err: fmt.Errorf("panic: %v", r)}
				}
			}()
			start := time.Now()
			sub, where := a.subAgent(gctx, s.Task, SubtaskOpts{
				Kind: s.Kind, Difficulty: s.Difficulty, Model: s.Model, Role: s.Role,
			})
			// Run with idle-stall detection + front-window→background promotion
			// (a child still working past frontWindow is moved to the bg so the
			// group doesn't block on it; a child idle past stallIdle is killed).
			r := a.runChild(gctx, childRun{task: s.Task, sub: sub, where: where, opts: SubtaskOpts{
				Kind: s.Kind, Difficulty: s.Difficulty, Model: s.Model, Role: s.Role,
			}, depth: depth})
			out, err, esc := r.out, r.err, where
			if r.promoted != "" {
				results[i] = childResult{idx: i, role: s.Role, where: where, result: "still working past " + frontWindow.String() + " → moved to background " + r.promoted + " (task_status " + r.promoted + " to collect)", dur: time.Since(start)}
				return
			}
			// Escalation (bounded, one step): a HARD error (not cancellation,
			// not a stated model override — the orchestrator chose that) gets
			// ONE retry on the next difficulty tier up, which the router maps
			// to a stronger model. No text heuristics (fragile/injectable);
			// only a real error triggers it.
			if err != nil && s.Model == "" && gctx.Err() == nil && !isCanceled(err) {
				if up := escalateDifficulty(s.Difficulty); up != s.Difficulty {
					retry := s
					retry.Difficulty = up
					rsub, rwhere := a.subAgent(gctx, retry.Task, SubtaskOpts{
						Kind: retry.Kind, Difficulty: retry.Difficulty, Model: retry.Model, Role: retry.Role,
					})
					rr := a.runChild(gctx, childRun{task: retry.Task, sub: rsub, where: rwhere, opts: SubtaskOpts{
						Kind: retry.Kind, Difficulty: retry.Difficulty, Model: retry.Model, Role: retry.Role,
					}, depth: depth})
					if rr.promoted != "" {
						out, err, esc = "still working past "+frontWindow.String()+" → moved to background "+rr.promoted, nil, "escalated → "+rwhere
					} else if rr.err == nil {
						out, err, esc = rr.out, nil, "escalated → "+rwhere
					}
				}
			}
			results[i] = childResult{idx: i, role: s.Role, where: esc, result: out, err: err, dur: time.Since(start)}
		}(i, s)
	}
	wg.Wait()

	report := formatGroupReport(results)
	if strings.TrimSpace(synthesize) == "" {
		return report, nil
	}
	// Optional merge step: one more sub-agent (no tools — pure reasoning) reads
	// the children's reports and produces a single coherent answer to the
	// synthesis question. Best-effort: on failure, return the raw report so the
	// orchestrator still has the children's findings.
	merged, err := a.synthesizeReports(ctx, synthesize, report)
	if err != nil || strings.TrimSpace(merged) == "" {
		return report, nil
	}
	return report + "\n\n--- synthesis ---\n" + merged, nil
}

func taskGroupRoleNames() []string {
	names := append([]string{}, RoleNames()...)
	names = append(names, PluginRoleNames()...)
	sort.Strings(names)
	return names
}

// synthesizeReports runs a tool-less sub-agent that reads the combined fan-out
// report and answers the synthesis question — the "merge step" that turns N
// child reports into one coherent result. No tools (it only reasons over the
// text it's given), cross-vendor where the router sends it.
func (a *Agent) synthesizeReports(ctx context.Context, question, report string) (string, error) {
	emptyTools, _ := tool.NewRegistry()
	prov := a.provider()
	compactor := a.compactor()
	if a.Router != nil {
		if rp, _, _ := a.Router(ctx, question, "general", "medium", false); rp != nil {
			prov = rp
			compactor = llm.NewCompactor(rp)
		}
	}
	sub := &Agent{
		Provider:         prov,
		Tools:            emptyTools,
		Perm:             PermAuto,
		MaxContextTokens: a.maxContextTokens(),
		Compactor:        compactor,
		ExtraSystem:      "You are a SYNTHESIZER. Read the parallel sub-agents' reports below and produce ONE coherent answer to the user's question. Reconcile overlaps, note disagreements, and cite which sub-agent found what. Do not invent findings beyond the reports.",
	}
	prompt := question + "\n\n=== sub-agent reports ===\n" + report
	return sub.NewSession().Send(ctx, prompt)
}

// formatGroupReport renders the children's outcomes in stable input order.
func formatGroupReport(results []childResult) string {
	sort.SliceStable(results, func(i, j int) bool { return results[i].idx < results[j].idx })
	var b strings.Builder
	ok := 0
	for _, r := range results {
		if r.err == nil {
			ok++
		}
	}
	fmt.Fprintf(&b, "task_group: %d subtasks, %d succeeded\n", len(results), ok)
	for _, r := range results {
		b.WriteString("\n")
		label := fmt.Sprintf("[%d] %s", r.idx+1, r.role)
		if r.where != "" {
			label += " (" + r.where + ")"
		}
		if r.dur > 0 {
			label += fmt.Sprintf(" · %s", r.dur.Round(time.Second))
		}
		b.WriteString(label + "\n")
		if r.err != nil {
			b.WriteString("  error: " + r.err.Error() + "\n")
			continue
		}
		res := r.result
		if len(res) > maxGroupResultBytes {
			res = tool.TruncateUTF8(res, maxGroupResultBytes) + "\n  …[truncated]"
		}
		// Indent each result line so the report stays scannable.
		for _, line := range strings.Split(strings.TrimRight(res, "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// escalateDifficulty returns the next difficulty tier up (the router maps it to
// a stronger model), or the same value when already at the top. Used for the
// one-step escalation retry of a failed read-only child.
func escalateDifficulty(d string) string {
	switch d {
	case "", "trivial":
		return "easy"
	case "easy":
		return "medium"
	case "medium":
		return "hard"
	default:
		return d // hard / unknown — already top
	}
}

// isCanceled reports whether err is a context cancellation/deadline (never
// retry those — the user/turn stopped, or the budget ran out).
func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
