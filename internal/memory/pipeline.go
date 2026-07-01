package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	JobStage1      = "mem_stage1"
	JobConsolidate = "mem_consolidate"
	JobSummary     = "mem_summary"
	JobForget      = "mem_forget"

	scopeJobKey = "scope"

	defaultPhase2ChunkBytes = 80_000
	maxPhase2ChunkBytes     = 100_000
	maxPhase2ChunkDepth     = 4
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

	// Batch path (optional, off-hot-path only): when Batcher + Stage1Req +
	// Stage1Parse are all set AND the provider supports batches, Stage1 work can
	// be SUBMITTED as one provider batch (~50% input discount) and reconciled on
	// a later wake, instead of N synchronous Complete calls. Stage1Req builds the
	// per-session request (same as the sync Stage1 sends); Stage1Parse turns a
	// batched reply's text back into a Stage1Result. Injected by main so this
	// package needn't import internal/dream or internal/llm's provider plumbing.
	Batcher interface {
		SubmitBatch(ctx context.Context, items []BatchRequest) (string, error)
		PollBatch(ctx context.Context, id string) (state string, done bool, err error)
		CollectBatch(ctx context.Context, id string) (map[string]string, error) // key → reply text
	}
	Stage1Req   func(transcript string) (system, user string)
	Stage1Parse func(text string) (Stage1Result, bool, error)

	// ConsolidateBytes triggers a consolidate when MEMORY.md exceeds this size
	// (0 = a sane default).
	ConsolidateBytes int

	// Phase2ChunkBytes bounds each input sent to Consolidate when a legacy
	// MEMORY.md/raw_memories.md payload is too large for one safe rewrite
	// (0 = a sane default). This keeps the callback's shrink guard meaningful
	// per chunk instead of comparing one compact summary against a huge input.
	Phase2ChunkBytes int
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
		if err := p.recordStage1(scope, s.ID, s.Watermark, result); err != nil {
			lastErr = err
			continue
		}
		n++
	}
	return n, lastErr
}

// recordStage1 persists one Stage1 result for a session: the SQLite row, the
// human rollout markdown, and the downstream consolidate/summary watermarks.
// Shared by the sync loop (Stage1Sessions) and the batch reconciler so a batched
// summary lands byte-identically to a live one. Returns the first error.
func (p *Pipeline) recordStage1(scope, threadID string, watermark int64, result Stage1Result) error {
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
	var firstErr error
	if p.Index != nil {
		if err := p.Index.RecordStage1Output(Stage1Output{
			Scope:           scope,
			ThreadID:        threadID,
			SourceUpdatedAt: watermark,
			RawMemory:       Redact(result.RawMemory),
			RolloutSummary:  Redact(result.RolloutSummary),
			RolloutSlug:     result.RolloutSlug,
			Outcome:         result.Outcome,
			GeneratedAt:     when.Unix(),
		}); err != nil {
			return err // can't record the row → don't enqueue downstream against it
		}
	}
	raw, err := p.Store.WriteRollout(result.RolloutSlug, result.RolloutSummary, when)
	if err != nil {
		firstErr = err
	} else if p.Index != nil {
		if err := p.Index.UpdateStage1RolloutPath(scope, threadID, raw); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if p.Index != nil {
		if err := p.Index.EnqueueWatermark(JobConsolidate, scope, scopeJobKey, watermark); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := p.Index.EnqueueWatermark(JobSummary, scope, scopeJobKey, watermark); err != nil && firstErr == nil {
			firstErr = err
		}
		// Forget runs off the same watermark: after new evidence lands, sweep the
		// scope's append-only tiers back under their retention caps. Cheap + purely
		// local (no model), so it rides along with every reflection.
		if err := p.Index.EnqueueWatermark(JobForget, scope, scopeJobKey, watermark); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// BatchRequest is one Stage1 request handed to the injected Batcher: a caller
// key (the session/thread id) + the system+user prompt. Decoupled from
// internal/llm so this package stays provider-agnostic; main adapts it.
type BatchRequest struct {
	Key    string
	System string
	User   string
}

// batchEnabled reports whether the batch Stage1 path is fully wired.
func (p *Pipeline) batchEnabled() bool {
	return p.Batcher != nil && p.Stage1Req != nil && p.Stage1Parse != nil && p.Index != nil && p.Store != nil
}

// SubmitStage1Batch gathers the not-yet-summarized sessions, submits them as ONE
// provider batch, and persists the batch record (crash-safe) — returning the
// batch id and how many items it carried. Results are applied later by
// ReconcileStage1Batches. Returns ("",0,nil) when nothing needs summarizing or
// the batch path isn't enabled (caller falls back to the sync Stage1Sessions).
func (p *Pipeline) SubmitStage1Batch(ctx context.Context, sessions []Session) (string, int, error) {
	if !p.batchEnabled() {
		return "", 0, nil
	}
	scope := p.scopeKey()
	var reqs []BatchRequest
	items := map[string]int64{}
	for _, s := range sessions {
		if p.Index.Stage1Summarized(scope, s.ID, s.Watermark) {
			continue
		}
		if strings.TrimSpace(s.Transcript) == "" {
			continue
		}
		if _, dup := items[s.ID]; dup {
			continue
		}
		sys, usr := p.Stage1Req(s.Transcript)
		reqs = append(reqs, BatchRequest{Key: s.ID, System: sys, User: usr})
		items[s.ID] = s.Watermark
	}
	if len(reqs) == 0 {
		return "", 0, nil
	}
	id, err := p.Batcher.SubmitBatch(ctx, reqs)
	if err != nil || id == "" {
		return "", 0, err // caller falls back to sync
	}
	if err := p.Index.RecordStage1Batch(Stage1Batch{
		BatchID:     id,
		Scope:       scope,
		Items:       items,
		SubmittedAt: time.Now().Unix(),
		State:       "pending",
	}); err != nil {
		// Persisted-record failure after a live submit: we'd lose track of the
		// batch. Surface it; the batch still runs provider-side but we can't
		// reconcile it — the watermark gate means those sessions just get
		// re-summarized next run (sync), so no data loss, only wasted spend.
		return id, len(reqs), err
	}
	return id, len(reqs), nil
}

// ReconcileStage1Batches polls every in-flight batch for THIS pipeline's scope.
// A done batch is collected and each result recorded via recordStage1 (identical
// to the sync path). A batch older than maxWait that hasn't finished is ABANDONED
// — its row deleted — so the watermark gate lets those sessions be re-summarized
// synchronously on a later run; memory never stalls on a stuck batch. Returns the
// number of sessions reconciled. (sessionsByID lets a failed/abandoned batch's
// items fall back to sync here when transcripts are still in hand.)
func (p *Pipeline) ReconcileStage1Batches(ctx context.Context, maxWait time.Duration) (int, error) {
	if !p.batchEnabled() {
		return 0, nil
	}
	scope := p.scopeKey()
	batches, err := p.Index.PendingStage1Batches()
	if err != nil {
		return 0, err
	}
	applied := 0
	var lastErr error
	for _, b := range batches {
		if b.Scope != scope {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		state, done, perr := p.Batcher.PollBatch(ctx, b.BatchID)
		if perr != nil {
			lastErr = perr
			continue
		}
		if !done {
			_ = p.Index.SetStage1BatchState(b.BatchID, state)
			// Max-wait fallback: a batch that won't finish must not block memory.
			if maxWait > 0 && time.Since(time.Unix(b.SubmittedAt, 0)) > maxWait {
				_ = p.Index.DeleteStage1Batch(b.BatchID) // un-summarized items re-run sync next wake
			}
			continue
		}
		results, cerr := p.Batcher.CollectBatch(ctx, b.BatchID)
		if cerr != nil {
			lastErr = cerr
			continue // try again next wake (until maxWait abandons it)
		}
		for threadID, watermark := range b.Items {
			text, ok := results[threadID]
			if !ok {
				continue // this item failed in the batch; watermark gate re-runs it sync
			}
			res, keep, perr := p.Stage1Parse(text)
			if perr != nil || !keep {
				continue
			}
			if err := p.recordStage1(scope, threadID, watermark, res); err != nil {
				lastErr = err
				continue
			}
			applied++
		}
		if err := p.Index.DeleteStage1Batch(b.BatchID); err != nil && lastErr == nil {
			lastErr = err
		}
	}
	return applied, lastErr
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
		case JobForget:
			if r := p.Store.PruneEvidence(time.Now()); r.RolloutsArchived > 0 || r.RetiredArchived > 0 {
				parts = append(parts, fmt.Sprintf("archived %d rollouts + %d retired notes", r.RolloutsArchived, r.RetiredArchived))
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
	// RMW guard: two Bridges can race on phase2Input (reads MEMORY.md + ad-hoc
	// + Stage1) → consolidate → Rewrite. Hold the lock across the ENTIRE span
	// so the read snapshot and the final write are atomic together.
	release, err := p.Store.lockStore()
	if err != nil {
		return false, err
	}
	defer release()
	cur, selected, adHocPaths := p.phase2Input()
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
	out, err := p.consolidatePhase2(ctx, cur)
	if err != nil {
		return false, err // the callback's shrink/empty guards refused — keep current
	}
	if err := p.Store.Rewrite(out); err != nil {
		return false, err
	}
	if p.Index != nil {
		p.Index.MarkSelectedForPhase2(selected)
	}
	// Retire the ad-hoc notes this run folded into MEMORY.md: they now live in
	// the curated memory, so leaving the source files live would re-feed them
	// into every future Phase 2 (unbounded growth + repeated reconsideration of
	// already-absorbed notes). Moved to retired/, not deleted — recoverable.
	p.Store.RetireAdHocNotes(adHocPaths)
	return true, nil
}

// phase2Input builds the consolidation input and reports both the Stage1 rows
// selected (to watermark) and the ad-hoc note FILE PATHS included (to retire
// after a successful fold).
func (p *Pipeline) phase2Input() (input string, selected []Stage1Output, adHocPaths []string) {
	if p == nil || p.Store == nil {
		return "", nil, nil
	}
	var b strings.Builder
	if cur := strings.TrimSpace(p.Store.Read()); cur != "" {
		b.WriteString("## Current MEMORY.md\n\n")
		b.WriteString(cur)
		b.WriteString("\n\n")
	}
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
	notes, paths := p.Store.adHocNotesWithPaths(64)
	if len(notes) > 0 {
		adHocPaths = paths
		b.WriteString("## Ad-hoc notes\n\n")
		for _, n := range notes {
			b.WriteString(strings.TrimSpace(n))
			b.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(b.String()) + "\n", selected, adHocPaths
}

func (p *Pipeline) consolidatePhase2(ctx context.Context, input string) (string, error) {
	if p == nil || p.Consolidate == nil {
		return "", fmt.Errorf("memory consolidation unavailable")
	}
	limit := p.Phase2ChunkBytes
	if limit <= 0 {
		limit = defaultPhase2ChunkBytes
	}
	if limit > maxPhase2ChunkBytes {
		limit = maxPhase2ChunkBytes
	}
	if limit < 1024 {
		limit = 1024
	}
	return p.consolidatePhase2Chunked(ctx, strings.TrimSpace(input), limit, 0)
}

func (p *Pipeline) consolidatePhase2Chunked(ctx context.Context, input string, limit, depth int) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("memory is empty; nothing to consolidate")
	}
	if len(input) <= limit {
		return p.Consolidate(ctx, input)
	}
	if depth >= maxPhase2ChunkDepth {
		return "", fmt.Errorf("phase2 input stayed over %d bytes after %d chunking passes", limit, maxPhase2ChunkDepth)
	}

	payloadLimit := limit - 512
	if payloadLimit < 512 {
		payloadLimit = limit
	}
	chunks := splitPhase2Chunks(input, payloadLimit)
	if len(chunks) <= 1 {
		return p.Consolidate(ctx, input)
	}

	var merged []string
	for i, ch := range chunks {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		chunkInput := fmt.Sprintf("## Phase 2 chunk %d/%d\n\n%s", i+1, len(chunks), ch)
		out, err := p.Consolidate(ctx, chunkInput)
		if err != nil {
			return "", fmt.Errorf("phase2 chunk %d/%d: %w", i+1, len(chunks), err)
		}
		out = strings.TrimSpace(out)
		if out == "" {
			return "", fmt.Errorf("phase2 chunk %d/%d produced empty output", i+1, len(chunks))
		}
		merged = append(merged, fmt.Sprintf("## Consolidated chunk %d/%d\n\n%s", i+1, len(chunks), out))
	}
	joined := strings.TrimSpace(strings.Join(merged, "\n\n"))
	if joined == "" {
		return "", fmt.Errorf("phase2 chunking produced empty output")
	}
	mergeInput := "## Phase 2 merge\n\n" + joined
	if len(mergeInput) <= limit {
		return p.Consolidate(ctx, mergeInput)
	}
	return p.consolidatePhase2Chunked(ctx, joined, limit, depth+1)
}

func splitPhase2Chunks(input string, limit int) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	if limit <= 0 || len(input) <= limit {
		return []string{input}
	}
	var chunks []string
	var b strings.Builder
	flush := func() {
		if strings.TrimSpace(b.String()) == "" {
			b.Reset()
			return
		}
		chunks = append(chunks, strings.TrimSpace(b.String()))
		b.Reset()
	}
	for _, line := range strings.SplitAfter(input, "\n") {
		for len(line) > limit {
			if b.Len() > 0 {
				flush()
			}
			head, tail := splitAtRuneBoundary(line, limit)
			chunks = append(chunks, strings.TrimSpace(head))
			line = tail
		}
		if b.Len() > 0 && b.Len()+len(line) > limit {
			flush()
		}
		b.WriteString(line)
	}
	flush()
	return chunks
}

func splitAtRuneBoundary(s string, limit int) (string, string) {
	if limit <= 0 || len(s) <= limit {
		return s, ""
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		cut = limit
	}
	return s[:cut], s[cut:]
}

// RegenSummary regenerates the small injected memory_summary.md from MEMORY.md.
// No-op without a Summarize callback. When MEMORY.md is empty/whitespace there is
// nothing to distill, so the stale distilled summary is REMOVED (rather than left
// behind): Store.Injected() prefers memory_summary.md when present, so keeping it
// would inject a summary of memory the user has cleared — injected memory must not
// diverge from the curated tier. Returns whether it changed anything on disk.
func (p *Pipeline) RegenSummary(ctx context.Context) (bool, error) {
	if p.Store == nil || p.Summarize == nil {
		return false, nil
	}
	mem := p.Store.Read()
	if strings.TrimSpace(mem) == "" {
		return p.removeStaleSummary()
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

// removeStaleSummary deletes the distilled summary tier(s) so Store.Injected()
// falls back to (the now-empty) MEMORY.md instead of serving a summary of memory
// the user cleared. Removes both the current memory_summary.md and the legacy
// SUMMARY.md that Injected() also reads. Returns whether any file was removed
// (so callers only report / commit when something actually changed).
func (p *Pipeline) removeStaleSummary() (bool, error) {
	if p.Store == nil {
		return false, nil
	}
	changed := false
	for _, path := range []string{p.Store.SummaryPath(), p.Store.legacySummaryPath()} {
		if path == "" {
			continue
		}
		err := os.Remove(path)
		switch {
		case err == nil:
			changed = true
		case os.IsNotExist(err):
			// nothing to remove for this tier
		default:
			return changed, err
		}
	}
	return changed, nil
}

// Run is the full per-scope pipeline: stage1 the given sessions → consolidate if
// large → regenerate the injected summary → commit to git. Each step is
// best-effort; a failing step doesn't abort the others. Returns a short report
// and the last error encountered (so a provider outage is surfaced, not hidden
// behind an empty report).
func (p *Pipeline) Run(ctx context.Context, sessions []Session) (string, error) {
	var parts []string
	var stageErr error
	if p.batchEnabled() {
		// Batch path: first apply any batch that finished since the last wake
		// (abandon ones stuck past 12h → they re-run sync), THEN submit this
		// wake's new sessions as one batch. Submitted results are recorded on a
		// LATER wake by the reconcile above, so this wake's downstream
		// (consolidate/summary) runs over whatever already landed.
		const batchMaxWait = 12 * time.Hour
		if applied, rerr := p.ReconcileStage1Batches(ctx, batchMaxWait); applied > 0 || rerr != nil {
			if applied > 0 {
				parts = append(parts, itoa(applied)+" batched summaries applied")
			}
			stageErr = rerr
		}
		if id, count, serr := p.SubmitStage1Batch(ctx, sessions); serr != nil {
			// Submit failed — fall back to a synchronous Stage1 pass this wake so
			// memory still progresses (the whole point of dream).
			if stageErr == nil {
				stageErr = serr
			}
			if n, e := p.Stage1Sessions(ctx, sessions); n > 0 {
				parts = append(parts, itoa(n)+" new session summaries (sync fallback)")
				if stageErr == nil {
					stageErr = e
				}
			}
		} else if id != "" {
			parts = append(parts, itoa(count)+" sessions submitted to batch")
		}
		// Drain downstream + maybe-consolidate over whatever's already recorded.
		return p.runDownstream(ctx, parts, stageErr)
	}
	n, syncErr := p.Stage1Sessions(ctx, sessions)
	if n > 0 {
		parts = append(parts, itoa(n)+" new session summaries")
	}
	return p.runDownstream(ctx, parts, syncErr)
}

// runDownstream drains queued consolidate/summary jobs, maybe-consolidates a
// legacy file with no queued work, and commits — the tail shared by the sync and
// batch Run paths. `parts`/`stageErr` carry whatever Stage1 (sync or batch) did.
func (p *Pipeline) runDownstream(ctx context.Context, parts []string, stageErr error) (string, error) {
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
