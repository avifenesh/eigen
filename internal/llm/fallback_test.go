package llm

import (
	"context"
	"errors"
	"testing"
)

func TestIsQuotaError(t *testing.T) {
	quota := []error{
		errors.New("HTTP 429: rate limited"),
		errors.New(`HTTP 429: {"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`),
		errors.New("too many requests"),
		errors.New("account out of credit"),
		errors.New("billing required"),
	}
	for _, e := range quota {
		if !IsQuotaError(e) {
			t.Errorf("IsQuotaError(%q) = false, want true", e)
		}
	}
	notQuota := []error{
		nil,
		errors.New("HTTP 500: internal error"),
		errors.New("connection refused"),
		errors.New("context deadline exceeded"),
	}
	for _, e := range notQuota {
		if IsQuotaError(e) {
			t.Errorf("IsQuotaError(%v) = true, want false", e)
		}
	}
}

// stubProvider counts Complete calls and returns a fixed result/error.
type stubProvider struct {
	name  string
	reply string
	err   error
	calls int
}

func (s *stubProvider) Name() string    { return s.name }
func (s *stubProvider) ModelID() string { return s.name }
func (s *stubProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &Response{Text: s.reply}, nil
}

func TestNewFallbackCollapsesNils(t *testing.T) {
	if NewFallback(nil, nil) != nil {
		t.Fatal("nil/nil should collapse to nil")
	}
	p := &stubProvider{name: "p"}
	if got := NewFallback(p, nil); got != p {
		t.Fatal("fallback nil should collapse to primary")
	}
	if got := NewFallback(nil, p); got != p {
		t.Fatal("primary nil should collapse to fallback")
	}
}

func TestFallbackUsesPrimaryWhenHealthy(t *testing.T) {
	primary := &stubProvider{name: "glm", reply: "PRIMARY"}
	fallback := &stubProvider{name: "small", reply: "FALLBACK"}
	f := NewFallback(primary, fallback)

	resp, err := f.Complete(context.Background(), Request{})
	if err != nil || resp.Text != "PRIMARY" {
		t.Fatalf("got (%v, %v), want PRIMARY", resp, err)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback should not be called when primary healthy, got %d", fallback.calls)
	}
	// Name/ModelID report the primary (headline model).
	if f.Name() != "glm" || f.ModelID() != "glm" {
		t.Fatalf("Name/ModelID should be the primary's, got %q/%q", f.Name(), f.ModelID())
	}
}

func TestFallbackRoutesAndFreezesOnQuota(t *testing.T) {
	primary := &stubProvider{name: "glm", err: errors.New(`HTTP 429: {"code":"1113","message":"Insufficient balance"}`)}
	fallback := &stubProvider{name: "small", reply: "FALLBACK"}
	f := NewFallback(primary, fallback)

	// First call: primary fails on quota → routes to fallback.
	resp, err := f.Complete(context.Background(), Request{})
	if err != nil || resp.Text != "FALLBACK" {
		t.Fatalf("got (%v, %v), want FALLBACK", resp, err)
	}
	if primary.calls != 1 {
		t.Fatalf("primary should be tried once, got %d", primary.calls)
	}

	// Second call: primary is frozen for the day → NOT retried, fallback again.
	resp, err = f.Complete(context.Background(), Request{})
	if err != nil || resp.Text != "FALLBACK" {
		t.Fatalf("got (%v, %v), want FALLBACK", resp, err)
	}
	if primary.calls != 1 {
		t.Fatalf("frozen primary must not be re-hit, got %d calls", primary.calls)
	}
	if fallback.calls != 2 {
		t.Fatalf("fallback should carry both calls, got %d", fallback.calls)
	}
}

func TestFallbackNonQuotaErrorDoesNotFreeze(t *testing.T) {
	primary := &stubProvider{name: "glm", err: errors.New("HTTP 500: transient")}
	fallback := &stubProvider{name: "small", reply: "FALLBACK"}
	f := NewFallback(primary, fallback)

	// A non-quota error still routes to the fallback...
	if _, err := f.Complete(context.Background(), Request{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ...but does NOT freeze the primary, so it's retried next call.
	_, _ = f.Complete(context.Background(), Request{})
	if primary.calls != 2 {
		t.Fatalf("non-quota failure should not freeze primary; want 2 tries, got %d", primary.calls)
	}
}
