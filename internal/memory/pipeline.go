package memory

import (
	"context"
	"strings"
	"time"
)

// Pipeline orchestrates the memory generation stages over a scope: turn new
// session transcripts into structured rollout summaries (stage1), fold their
// durable content into MEMORY.md, consolidate MEMORY.md when it grows, and
// regenerate the small injected SUMMARY.md. The model-facing steps are injected
// as callbacks so this package needn't import internal/dream (avoids a cycle).
//
// Triggers (idle TUI dream, daemon nightly tick, `eigen dream`) call Run after
// enqueuing the sessions to summarize. The work is idempotent (watermarks) and
// safe (snapshots + git history + shrink guards in the callbacks).
type Pipeline struct {
	Store *Store
	Index *Index

	// Stage1 summarizes one transcript → (markdown body, slug, outcome, ok).
	// ok=false means skip (trivial session). Provided by the dream package.
	Stage1 func(ctx context.Context, transcript string) (body, slug, outcome string, ok bool, err error)
	// Consolidate rewrites the full MEMORY.md into a smaller current one.
	Consolidate func(ctx context.Context, current string) (string, error)
	// Summarize distills MEMORY.md into the small injected SUMMARY.md.
	Summarize func(ctx context.Context, memory string) (string, error)

	// ConsolidateBytes triggers a consolidate when MEMORY.md exceeds this size
	// (0 = a sane default).
	ConsolidateBytes int
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
// summarized at their watermark), writing each non-trivial one to raw/ and
// folding its durable content into MEMORY.md. Returns how many new summaries
// were produced and the last stage1 error (so callers can surface a provider
// problem instead of reporting a misleading "nothing to remember").
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
		if p.Index.Summarized(scope, s.ID, s.Watermark) {
			continue
		}
		body, slug, outcome, ok, err := p.Stage1(ctx, s.Transcript)
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
		raw, err := p.Store.WriteRollout(slug, body, time.Now())
		if err != nil {
			lastErr = err
			continue
		}
		_ = p.Index.RecordSummary(SummaryRow{Scope: scope, SessionID: s.ID, Slug: slug, RawPath: raw, Outcome: outcome, Watermark: s.Watermark, GeneratedAt: time.Now().Unix()})
		// Fold the rollout's durable content into MEMORY.md (the working tier).
		// Consolidation later dedups/structures it; here we just accrue.
		if err := p.Store.appendRollout(body); err == nil {
			n++
		}
	}
	return n, lastErr
}

// MaybeConsolidate rewrites MEMORY.md when it exceeds the size threshold (or
// when force is set), keeping a snapshot + git history. No-op without a
// Consolidate callback.
func (p *Pipeline) MaybeConsolidate(ctx context.Context, force bool) (bool, error) {
	if p.Store == nil || p.Consolidate == nil {
		return false, nil
	}
	cur := p.Store.Read()
	limit := p.ConsolidateBytes
	if limit <= 0 {
		limit = 24_000 // ~ a few hundred bullets; keeps MEMORY.md curatable
	}
	if !force && len(cur) < limit {
		return false, nil
	}
	out, err := p.Consolidate(ctx, cur)
	if err != nil {
		return false, err // the callback's shrink/empty guards refused — keep current
	}
	if err := p.Store.Rewrite(out); err != nil {
		return false, err
	}
	return true, nil
}

// RegenSummary regenerates the small injected SUMMARY.md from MEMORY.md. No-op
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
	if did, _ := p.MaybeConsolidate(ctx, false); did {
		parts = append(parts, "consolidated MEMORY.md")
	}
	if did, _ := p.RegenSummary(ctx); did {
		parts = append(parts, "regenerated SUMMARY.md")
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
