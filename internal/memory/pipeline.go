package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	JobStage1      = "mem_stage1"
	JobConsolidate = "mem_consolidate"
	JobSummary     = "mem_summary"

	scopeJobKey = "scope"
)

// Pipeline orchestrates the memory generation stages over a scope: turn new
// session transcripts into DB-first Stage1 outputs, materialize rollout summaries
// into the memory workspace, consolidate Stage1/ad-hoc material into MEMORY.md,
// and regenerate the small injected memory_summary.md. The model-facing steps are
// injected as callbacks so this package needn't import internal/dream.
//
// Triggers (idle TUI dream, daemon nightly tick, `eigen dream`) call Run after
// enqueuing the sessions to summarize. The work is idempotent (watermarks) and
// safe (snapshots + git history + shrink guards in the callbacks).
type Pipeline struct {
	Store *Store
	Index *Index

	// Stage1 summarizes one transcript. ok=false means skip (trivial session).
	Stage1 func(ctx context.Context, sessionID, transcript string) (Stage1Result, bool, error)
	// Consolidate rewrites the full MEMORY.md into a smaller current one.
	Consolidate func(ctx context.Context, current string) (string, error)
	// Summarize distills MEMORY.md into the small injected memory_summary.md.
	Summarize func(ctx context.Context, memory string) (string, error)

	// ConsolidateBytes triggers a consolidate when MEMORY.md exceeds this size
	// (0 = a sane default).
	ConsolidateBytes int
}

// Stage1Result is the model output for one transcript. RawMemory is the compact
// durable memory candidate stored in SQLite; RolloutSummary is the human-readable
// evidence markdown materialized under rollout_summaries/.
type Stage1Result struct {
	RawMemory      string
	RolloutSummary string
	RolloutSlug    string
	Outcome        string
}

// Session is one transcript to summarize.
type Session struct {
	ID         string
	Transcript string
	Watermark  int64 // mtime/size signature; skip if already summarized at this value
}

// scopeKey is the index scope for this pipeline's store.
func (p *Pipeline) scopeKey() string {
	if p.Store == nil {
		return ""
	}
	if p.Store.IsGlobal() {
		return "global"
	}
	// the scope dir's base name is the readable key
	return baseName(p.Store.Dir())
}

func baseName(dir string) string {
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

// Stage1Sessions summarizes the given sessions (skipping ones already
// summarized at their watermark), writes each non-trivial output to SQLite first,
// then materializes rollout markdown for humans and Phase 2 evidence. Returns
// how many new summaries were produced and the last stage1 error.
func (p *Pipeline) Stage1Sessions(ctx context.Context, sessions []Session) (int, error) {
	if p.Store == nil || p.Stage1 == nil {
		return 0, nil
	}
	scope := p.scopeKey()
	n := 0
	var lastErr error
	for _, s := range sessions {
		if ctx.Err() != nil {
			break
		}
		if p.Index != nil && p.Index.Stage1Summarized(scope, s.ID, s.Watermark) {
			continue
		}
		result, ok, err := p.Stage1(ctx, s.ID, s.Transcript)
		if err != nil {
			lastErr = err // one bad session must not stall the rest; remember it
			continue
		}
		if !ok {
			// Trivial OR a flaky small-model "skip": do NOT permanently mark it
			// summarized. A single skip from a non-deterministic small model
			// must not bury a session that's actually worth remembering — let
			// the next run (possibly a better model / different sampling)
			// re-evaluate it. (Truly trivial sessions just keep returning skip,
			// cheaply.)
			continue
		}
		when := time.Now()
		if strings.TrimSpace(result.RawMemory) == "" {
			result.RawMemory = result.RolloutSummary
		}
		if strings.TrimSpace(result.RolloutSummary) == "" {
			result.RolloutSummary = result.RawMemory
		}
		if result.RolloutSlug == "" {
			result.RolloutSlug = "session"
		}
		if p.Index != nil {
			if err := p.Index.RecordStage1Output(Stage1Output{
				Scope:           scope,
				ThreadID:        s.ID,
				SourceUpdatedAt: s.Watermark,
				RawMemory:       Redact(result.RawMemory),
				RolloutSummary:  Redact(result.RolloutSummary),
				RolloutSlug:     result.RolloutSlug,
				Outcome:         result.Outcome,
				GeneratedAt:     when.Unix(),
			}); err != nil {
				lastErr = err
				continue
			}
		}
		raw, err := p.Store.WriteRollout(result.RolloutSlug, result.RolloutSummary, when)
		if err != nil {
			lastErr = err
		} else if p.Index != nil {
			if err := p.Index.UpdateStage1RolloutPath(scope, s.ID, raw); err != nil {
				lastErr = err
			}
		}
		if p.Index != nil {
			if err := p.Index.EnqueueWatermark(JobConsolidate, scope, scopeJobKey, s.Watermark); err != nil {
				lastErr = err
			}
			if err := p.Index.EnqueueWatermark(JobSummary, scope, scopeJobKey, s.Watermark); err != nil {
				lastErr = err
			}
		}
		n++
	}
	return n, lastErr
}

// RunQueued drains queued downstream memory jobs for this pipeline's scope.
// Stage1 jobs need caller-supplied transcripts, so this worker handles the
// per-scope jobs that operate from the Store itself: consolidate and summary.
func (p *Pipeline) RunQueued(ctx context.Context, maxJobs int) (string, error) {
	if p.Store == nil || p.Index == nil {
		return "", nil
	}
	if maxJobs <= 0 {
		maxJobs = 16
	}
	scope := p.scopeKey()
	var parts []string
	var lastErr error
	for n := 0; n < maxJobs; n++ {
		if ctx.Err() != nil {
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			break
		}
		j, ok, err := p.Index.ClaimScope(scope, 5*60)
		if err != nil {
			return strings.Join(parts, ", "), err
		}
		if !ok {
			break
		}
		var jobErr error
		switch j.Kind {
		case JobConsolidate:
			if did, err := p.MaybeConsolidate(ctx, true); err != nil {
				jobErr = err
			} else if did {
				parts = append(parts, "consolidated MEMORY.md")
			}
		case JobSummary:
			if did, err := p.RegenSummary(ctx); err != nil {
				jobErr = err
			} else if did {
				parts = append(parts, "regenerated memory_summary.md")
			}
		default:
			jobErr = fmt.Errorf("unsupported memory job %q for scope %q", j.Kind, j.Scope)
		}
		if err := p.Index.Finish(j, jobErr); err != nil && jobErr == nil {
			jobErr = err
		}
		if jobErr != nil {
			lastErr = jobErr
		}
	}
	return strings.Join(parts, ", "), lastErr
}

// MaybeConsolidate rewrites MEMORY.md when it exceeds the size threshold (or
// when force is set), keeping a snapshot + git history. No-op without a
// Consolidate callback.
func (p *Pipeline) MaybeConsolidate(ctx context.Context, force bool) (bool, error) {
	if p.Store == nil || p.Consolidate == nil {
		return false, nil
	}
	cur := p.phase2Input()
	limit := p.ConsolidateBytes
	if limit <= 0 {
		limit = 24_000 // ~ a few hundred bullets; keeps MEMORY.md curatable
	}
	if !force && len(p.Store.Read()) < limit {
		return false, nil
	}
	if strings.TrimSpace(cur) == "" {
		return false, nil
	}
	_ = p.Store.WriteRawMemories(cur)
	out, err := p.Consolidate(ctx, cur)
	if err != nil {
		return false, err // the callback's shrink/empty guards refused — keep current
	}
	if err := p.Store.Rewrite(out); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Pipeline) phase2Input() string {
	if p == nil || p.Store == nil {
		return ""
	}
	var b strings.Builder
	if cur := strings.TrimSpace(p.Store.Read()); cur != "" {
		b.WriteString("## Current MEMORY.md\n\n")
		b.WriteString(cur)
		b.WriteString("\n\n")
	}
	var selected []Stage1Output
	if p.Index != nil {
		rows, err := p.Index.Phase2Inputs(p.scopeKey(), 64)
		if err == nil {
			selected = rows
		}
	}
	if len(selected) > 0 {
		b.WriteString("## Stage1 raw memories\n\n")
		for _, r := range selected {
			fmt.Fprintf(&b, "### %s (%s)\n\n%s\n\n", r.ThreadID, r.RolloutSlug, strings.TrimSpace(r.RawMemory))
		}
	}
	if notes := p.Store.AdHocNotes(64); len(notes) > 0 {
		b.WriteString("## Ad-hoc notes\n\n")
		for _, n := range notes {
			b.WriteString(strings.TrimSpace(n))
			b.WriteString("\n\n")
		}
	}
	if p.Index != nil {
		p.Index.MarkSelectedForPhase2(selected)
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// RegenSummary regenerates the small injected memory_summary.md from MEMORY.md. No-op
// without a Summarize callback or when memory is empty.
func (p *Pipeline) RegenSummary(ctx context.Context) (bool, error) {
	if p.Store == nil || p.Summarize == nil {
		return false, nil
	}
	mem := p.Store.Read()
	if strings.TrimSpace(mem) == "" {
		return false, nil
	}
	sum, err := p.Summarize(ctx, mem)
	if err != nil || strings.TrimSpace(sum) == "" {
		return false, err
	}
	if err := p.Store.writeSummary(sum); err != nil {
		return false, err
	}
	return true, nil
}

// Run is the full per-scope pipeline: stage1 the given sessions → consolidate if
// large → regenerate the injected summary → commit to git. Each step is
// best-effort; a failing step doesn't abort the others. Returns a short report
// and the last error encountered (so a provider outage is surfaced, not hidden
// behind an empty report).
func (p *Pipeline) Run(ctx context.Context, sessions []Session) (string, error) {
	var parts []string
	n, stageErr := p.Stage1Sessions(ctx, sessions)
	if n > 0 {
		parts = append(parts, itoa(n)+" new session summaries")
	}
	queued, queuedErr := p.RunQueued(ctx, 16)
	if queued != "" {
		parts = append(parts, queued)
	}
	if stageErr == nil {
		stageErr = queuedErr
	}
	// Keep `eigen dream` useful for existing MEMORY.md files that have no
	// queued work yet (for example after migrating a flat v1 memory).
	if queued == "" {
		if did, _ := p.MaybeConsolidate(ctx, false); did {
			parts = append(parts, "consolidated MEMORY.md")
		}
		if did, _ := p.RegenSummary(ctx); did {
			parts = append(parts, "regenerated memory_summary.md")
		}
	}
	if len(parts) > 0 {
		CommitMemory("dream: " + p.scopeKey() + " — " + strings.Join(parts, ", "))
		return strings.Join(parts, ", "), stageErr
	}
	return "", stageErr
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
