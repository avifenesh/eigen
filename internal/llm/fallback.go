package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// IsQuotaError reports whether err is a provider quota/billing/rate rejection
// that won't clear by retrying soon — HTTP 429, "insufficient balance", a
// drained resource package (z.ai code 1113), or generic quota/billing wording.
// Distinct from a transient network blip: the caller should STOP hitting this
// provider for a while, not retry it. (Errors surface as `HTTP <code>: <body>`
// from internal/llm/http.go, so the raw provider message is in the string.)
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, sig := range []string{
		"http 429",
		"too many requests",
		"insufficient balance",
		"no resource package",
		"1113", // z.ai: "Insufficient balance or no resource package"
		"quota",
		"out of credit",
		"billing",
	} {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

// fallbackProvider tries a primary provider, then a fallback. When the primary
// fails with a quota/billing error (IsQuotaError), the primary is FROZEN until
// the next local midnight — so a drained account (e.g. GLM out of balance) is
// not re-hit on every call for the rest of the day; the fallback carries the
// load until then. ANY primary error (quota or not) routes to the fallback so a
// single bad call still produces output — except a cancelled/expired context,
// which short-circuits (the caller's deadline is already gone).
//
// Name/ModelID report the PRIMARY (the headline model); the fallback is an
// invisible safety net, not a model switch the user chose.
type fallbackProvider struct {
	primary  Provider
	fallback Provider

	mu          sync.Mutex
	frozenUntil time.Time // primary is skipped until this instant (zero = live)
}

// FallbackNotice describes a primary→fallback failover for a single turn.
type FallbackNotice struct {
	PrimaryID, FallbackID string
	Cause                 error
}

// fallbackNotifierKey carries a per-CALL failover callback through the context,
// rather than storing it on the (process-shared) provider. Providers are shared
// across concurrent sessions/subagents, so a notifier set on the provider would
// be overwritten between sessions and route a notice to the wrong one. A context
// value is per-call and thread-safe — each turn's ctx carries its own sink.
type fallbackNotifierKey struct{}

// WithFallbackNotifier returns a ctx that, when a fallbackProvider fails over
// during a call made with it, invokes fn with the failover detail. The agent
// wraps each turn's ctx so a fallback is SURFACED (with the cause), never a
// silent bypass.
func WithFallbackNotifier(ctx context.Context, fn func(FallbackNotice)) context.Context {
	return context.WithValue(ctx, fallbackNotifierKey{}, fn)
}

// notifyFallback fires the context's failover callback (if any). Per-call, so no
// shared state and no lock.
func notifyFallback(ctx context.Context, n FallbackNotice) {
	if fn, ok := ctx.Value(fallbackNotifierKey{}).(func(FallbackNotice)); ok && fn != nil {
		fn(n)
	}
}

func (f *fallbackProvider) notifyFallback(ctx context.Context, cause error) {
	notifyFallback(ctx, FallbackNotice{PrimaryID: f.primary.ModelID(), FallbackID: f.fallback.ModelID(), Cause: cause})
}

// NewFallback wraps a primary provider with a fallback. A nil side collapses to
// the other (so callers don't special-case "only one available"); nil/nil → nil.
func NewFallback(primary, fallback Provider) Provider {
	switch {
	case primary == nil && fallback == nil:
		return nil
	case primary == nil:
		return fallback
	case fallback == nil:
		return primary
	default:
		return &fallbackProvider{primary: primary, fallback: fallback}
	}
}

func (f *fallbackProvider) Name() string    { return f.primary.Name() }
func (f *fallbackProvider) ModelID() string { return f.primary.ModelID() }

func (f *fallbackProvider) frozen() bool {
	// Lock the read: frozenUntil is written under f.mu in freezeForToday, and
	// frozen() is called concurrently from Complete/Stream across sessions.
	f.mu.Lock()
	until := f.frozenUntil
	f.mu.Unlock()
	if !until.IsZero() && time.Now().Before(until) {
		return true
	}
	// Also honor the PROCESS-WIDE freeze: a quota 429 on this model's provider
	// from ANY instance (a sibling subagent, a prior call) parks the whole
	// provider, so we don't re-probe a known-drained account per fresh provider.
	return providerFrozen(canonicalProvider(ResolveProvider("", f.primary.ModelID())))
}

// freezeForToday parks the primary until the next local midnight — both on this
// instance AND process-wide by provider, so every other provider built for the
// same drained backend (subagent picks, fresh dream providers) skips it too.
func (f *fallbackProvider) freezeForToday() {
	f.mu.Lock()
	f.frozenUntil = nextMidnight()
	f.mu.Unlock()
	FreezeProvider(canonicalProvider(ResolveProvider("", f.primary.ModelID())))
}

func (f *fallbackProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	if !f.frozen() {
		resp, err := f.primary.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		// On a quota/billing rejection, freeze the primary for the rest of the
		// day so the next scan goes straight to the fallback (no wasted 429).
		if IsQuotaError(err) {
			f.freezeForToday()
		}
		// Don't burn the fallback on an already-dead context (the caller's
		// timeout/cancel applies to both); surface the primary's error.
		if ctx.Err() != nil {
			return nil, err
		}
		if f.fallback == nil {
			return nil, err // nothing to fall to — surface the real cause
		}
		f.notifyFallback(ctx, err) // SURFACE the failover + cause; never silent
	}
	if f.fallback == nil {
		return nil, errors.New("fallback provider unavailable")
	}
	return f.fallback.Complete(ctx, req)
}

// Stream forwards to the primary's streaming path (falling back on a quota
// freeze / error), so a wrapped reasoning model still streams its text +
// reasoning deltas. Without this method the wrapper is not a Streamer, so the
// agent silently took the non-streaming Complete path: no live text, and no
// reasoning deltas at all — the model's "thoughts" never reached the UI. A side
// that can't stream (only Complete) is driven via streamViaComplete so the sink
// still fires once with the final text.
func (f *fallbackProvider) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	if !f.frozen() {
		resp, err := streamAny(ctx, f.primary, req, sink)
		if err == nil {
			return resp, nil
		}
		if IsQuotaError(err) {
			f.freezeForToday()
		}
		if ctx.Err() != nil {
			return nil, err
		}
		if f.fallback == nil {
			return nil, err
		}
		f.notifyFallback(ctx, err) // SURFACE the failover + cause; never silent
	}
	if f.fallback == nil {
		return nil, errors.New("fallback provider unavailable")
	}
	return streamAny(ctx, f.fallback, req, sink)
}

// streamAny streams through p when it implements Streamer, else runs Complete
// and emits the final text as a single chunk so callers relying on the sink
// still see output.
func streamAny(ctx context.Context, p Provider, req Request, sink StreamSink) (*Response, error) {
	if sm, ok := p.(Streamer); ok {
		return sm.Stream(ctx, req, sink)
	}
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if sink != nil && resp != nil && resp.Text != "" {
		sink(StreamChunk{Kind: ChunkText, Text: resp.Text})
	}
	return resp, nil
}

// activeProvider returns the side capability calls should target: the primary
// unless it is frozen (quota), then the fallback. Keeps runtime toggles
// (effort/search/fast) pointed at whichever provider actually serves turns.
func (f *fallbackProvider) activeProvider() Provider {
	if f.frozen() && f.fallback != nil {
		return f.fallback
	}
	return f.primary
}

// SetEffort/Effort/SetSearch/SearchMode/SetFast/FastMode forward the optional
// runtime-capability interfaces to the active provider, so a wrapped reasoning
// model still exposes its effort/search/fast controls (the daemon type-asserts
// the live provider for these; an unwrapped assertion on the bare wrapper failed
// → the GUI showed no effort dropdown and SetEffort was a no-op).
func (f *fallbackProvider) SetEffort(level string) bool {
	if es, ok := f.activeProvider().(EffortSetter); ok {
		return es.SetEffort(level)
	}
	return false
}

func (f *fallbackProvider) Effort() string {
	if es, ok := f.activeProvider().(EffortSetter); ok {
		return es.Effort()
	}
	return ""
}

func (f *fallbackProvider) SetSearch(mode string) bool {
	if sr, ok := f.activeProvider().(Searcher); ok {
		return sr.SetSearch(mode)
	}
	return false
}

func (f *fallbackProvider) SearchMode() string {
	if sr, ok := f.activeProvider().(Searcher); ok {
		return sr.SearchMode()
	}
	return ""
}

func (f *fallbackProvider) SetFast(on bool) bool {
	if fm, ok := f.activeProvider().(FastModer); ok {
		return fm.SetFast(on)
	}
	return false
}

func (f *fallbackProvider) FastMode() bool {
	if fm, ok := f.activeProvider().(FastModer); ok {
		return fm.FastMode()
	}
	return false
}

func nextMidnight() time.Time {
	now := time.Now()
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
}

// ── process-wide provider quota freeze ──────────────────────────────────────
// A quota/billing 429 (e.g. a drained GLM account whose KEY is still present)
// must take that provider out of selection EVERYWHERE until it likely refills —
// not just on the one wrapped instance that saw the error. Subagent model picks
// (SubagentModel) and fresh dream/judge providers all consult this, so a frozen
// provider drops out of every capability ladder for the rest of the day instead
// of being re-tried (and re-429'd) per fresh provider. Keyed by canonical
// provider name; auto-expires at the next local midnight.
var (
	frozenMu        sync.Mutex
	frozenProviders = map[string]time.Time{}
)

// FreezeProvider parks a provider (by canonical name) until next local midnight.
func FreezeProvider(provider string) {
	if provider == "" {
		return
	}
	frozenMu.Lock()
	frozenProviders[provider] = nextMidnight()
	frozenMu.Unlock()
}

// clearFrozenProviders resets the process-wide freeze registry (tests only).
func clearFrozenProviders() {
	frozenMu.Lock()
	frozenProviders = map[string]time.Time{}
	frozenMu.Unlock()
}

// providerFrozen reports whether a provider is currently quota-frozen.
func providerFrozen(provider string) bool {
	if provider == "" {
		return false
	}
	frozenMu.Lock()
	defer frozenMu.Unlock()
	until, ok := frozenProviders[provider]
	if !ok {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	delete(frozenProviders, provider) // expired — clear it
	return false
}
