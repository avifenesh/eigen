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
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.frozenUntil.IsZero() && time.Now().Before(f.frozenUntil)
}

// freezeForToday parks the primary until the next local midnight.
func (f *fallbackProvider) freezeForToday() {
	now := time.Now()
	y, m, d := now.Date()
	f.mu.Lock()
	f.frozenUntil = time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
	f.mu.Unlock()
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
	}
	if f.fallback == nil {
		return nil, errors.New("fallback provider unavailable")
	}
	return f.fallback.Complete(ctx, req)
}
