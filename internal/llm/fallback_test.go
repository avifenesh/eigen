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
	clearFrozenProviders() // isolate from the process-wide freeze registry
	t.Cleanup(clearFrozenProviders)
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

// A quota 429 must freeze the provider PROCESS-WIDE so SubagentModel /
// modelCredentialed drop it from every ladder (not just the one wrapped
// instance) — the GLM-drained-account case.
func TestProcessWideProviderFreeze(t *testing.T) {
	clearFrozenProviders()
	t.Cleanup(clearFrozenProviders)
	if providerFrozen("glm") {
		t.Fatal("glm should not be frozen initially")
	}
	FreezeProvider("glm")
	if !providerFrozen("glm") {
		t.Fatal("glm should be frozen after FreezeProvider")
	}
	if providerFrozen("converse") {
		t.Fatal("freezing glm must not freeze other providers")
	}
}

func TestFallbackNonQuotaErrorDoesNotFreeze(t *testing.T) {
	clearFrozenProviders() // isolate from the process-wide freeze registry
	t.Cleanup(clearFrozenProviders)
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

// effortStub is a provider with the EffortSetter/Searcher/FastModer capabilities,
// to verify the fallback wrapper forwards them to the active side.
type effortStub struct {
	stubProvider
	effort string
	search string
	fast   bool
}

func (e *effortStub) SetEffort(l string) bool { e.effort = l; return true }
func (e *effortStub) Effort() string          { return e.effort }
func (e *effortStub) SetSearch(m string) bool { e.search = m; return true }
func (e *effortStub) SearchMode() string      { return e.search }
func (e *effortStub) SetFast(on bool) bool    { e.fast = on; return true }
func (e *effortStub) FastMode() bool          { return e.fast }

// TestFallbackForwardsCapabilities is the GLM "no effort dropdown" fix: a
// reasoning provider wrapped in a fallback must still expose EffortSetter etc.,
// or the daemon's type-assertion fails and the GUI shows no effort control.
func TestFallbackForwardsCapabilities(t *testing.T) {
	clearFrozenProviders()
	primary := &effortStub{stubProvider: stubProvider{name: "glm-5.2", reply: "ok"}, effort: "max"}
	fb := &stubProvider{name: "small", reply: "fb"}
	f := NewFallback(primary, fb)

	es, ok := f.(EffortSetter)
	if !ok {
		t.Fatal("wrapped provider must implement EffortSetter (else no effort dropdown)")
	}
	if es.Effort() != "max" {
		t.Fatalf("Effort() forwarded = %q, want max", es.Effort())
	}
	if !es.SetEffort("high") || primary.effort != "high" {
		t.Fatalf("SetEffort not forwarded to primary: %q", primary.effort)
	}
	if sr, ok := f.(Searcher); !ok || !sr.SetSearch("on") || primary.search != "on" {
		t.Fatal("Searcher not forwarded to primary")
	}
	if fm, ok := f.(FastModer); !ok || !fm.SetFast(true) || !primary.fast {
		t.Fatal("FastModer not forwarded to primary")
	}
}

// TestFallbackNotifiesOnFailover is the "don't bypass silently" requirement: a
// failover fires onFallback with the primary's cause.
func TestFallbackNotifiesOnFailover(t *testing.T) {
	clearFrozenProviders()
	primary := &stubProvider{name: "glm-5.2", err: errors.New("HTTP 500: boom")}
	fb := &stubProvider{name: "small", reply: "served"}
	f := NewFallback(primary, fb)

	var gotPrimary, gotFallback string
	var gotCause error
	SetFallbackNotifier(f, func(p, fbid string, cause error) {
		gotPrimary, gotFallback, gotCause = p, fbid, cause
	})
	resp, err := f.Complete(context.Background(), Request{})
	if err != nil || resp.Text != "served" {
		t.Fatalf("expected fallback to serve, got %v / %v", resp, err)
	}
	if gotPrimary != "glm-5.2" || gotFallback != "small" || gotCause == nil {
		t.Fatalf("failover not surfaced: primary=%q fallback=%q cause=%v", gotPrimary, gotFallback, gotCause)
	}
}

// TestFallbackStreamsThroughPrimary: the wrapper is a Streamer and forwards
// reasoning/text chunks (else the agent took the non-streaming path → no
// thoughts).
func TestFallbackStreamsThroughPrimary(t *testing.T) {
	clearFrozenProviders()
	primary := &streamStub{name: "glm-5.2"}
	fb := &stubProvider{name: "small", reply: "fb"}
	f := NewFallback(primary, fb)
	sm, ok := f.(Streamer)
	if !ok {
		t.Fatal("wrapped provider must implement Streamer")
	}
	var kinds []ChunkKind
	_, err := sm.Stream(context.Background(), Request{}, func(c StreamChunk) { kinds = append(kinds, c.Kind) })
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != ChunkReasoning || kinds[1] != ChunkText {
		t.Fatalf("expected reasoning then text chunks forwarded, got %v", kinds)
	}
}

// streamStub emits one reasoning + one text chunk.
type streamStub struct{ name string }

func (s *streamStub) Name() string    { return s.name }
func (s *streamStub) ModelID() string { return s.name }
func (s *streamStub) Complete(ctx context.Context, req Request) (*Response, error) {
	return &Response{Text: "done"}, nil
}
func (s *streamStub) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	sink(StreamChunk{Kind: ChunkReasoning, Text: "thinking"})
	sink(StreamChunk{Kind: ChunkText, Text: "answer"})
	return &Response{Text: "answer", Reasoning: "thinking"}, nil
}
