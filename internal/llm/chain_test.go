package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ResolveChainID must map every entry in the user's canonical chain to either a
// real catalog id or "" (skip) — never panic, never a bogus passthrough. The
// chain is opus,gpt-5.5,glm,sonnet,gpt-5.4,opus-4.7,glm-5.1,composer,glm-5,grok.
func TestResolveChainIDUserChain(t *testing.T) {
	cases := map[string]bool{ // friendly → expect-resolvable
		"opus":     true,
		"gpt-5.5":  true,
		"glm":      true,
		"sonnet":   true,
		"gpt-5.4":  true,
		"opus-4.7": false, // not in this build's catalog → skipped, not errored
		"glm-5.1":  false, // demoted alias not aliased → resolver decides; assert no panic
		"composer": true,
		"glm-5":    false,
		"grok":     true,
	}
	for friendly, wantResolvable := range cases {
		got := ResolveChainID(friendly)
		if wantResolvable && got == "" {
			t.Errorf("ResolveChainID(%q) = \"\" (skipped), want a catalog id", friendly)
		}
		// Unresolvable entries are allowed: "" means the chain skips them.
		_ = got
	}
}

// ResolveChainID passes a real catalog id and an explicit provider:id ref through
// unchanged, and trims whitespace.
func TestResolveChainIDPassthrough(t *testing.T) {
	// A bare alias resolves to a catalog id; re-resolving that id is idempotent.
	id := ResolveChainID("opus")
	if id == "" {
		t.Fatal("opus did not resolve")
	}
	if got := ResolveChainID(id); got != id {
		t.Errorf("ResolveChainID(%q) = %q, want passthrough", id, got)
	}
	if got := ResolveChainID("  opus  "); got != id {
		t.Errorf("ResolveChainID with surrounding space = %q, want %q", got, id)
	}
	if ResolveChainID("") != "" {
		t.Error("empty string should resolve to empty")
	}
	if ResolveChainID("totally-not-a-model-xyz") != "" {
		t.Error("unknown name should resolve to empty (skip), not passthrough")
	}
}

// NewChain drops unresolvable + duplicate ids, preserves order, and returns nil
// when nothing resolves.
func TestNewChainBuild(t *testing.T) {
	if NewChain() != nil {
		t.Error("empty NewChain should be nil")
	}
	if NewChain("totally-not-a-model", "also-bogus") != nil {
		t.Error("all-unresolvable NewChain should be nil")
	}
	// opus + its own alias collapse to one id; bogus dropped.
	p := NewChain("opus", "opus-4.8", "bogus", "grok")
	cp, ok := p.(*chainProvider)
	if !ok {
		t.Fatalf("NewChain returned %T, want *chainProvider", p)
	}
	opus := ResolveChainID("opus")
	grok := ResolveChainID("grok")
	want := []string{opus}
	if grok != "" && grok != opus {
		want = append(want, grok)
	}
	if len(cp.ids) != len(want) {
		t.Fatalf("chain ids = %v, want %v (deduped, bogus dropped)", cp.ids, want)
	}
	for i := range want {
		if cp.ids[i] != want[i] {
			t.Fatalf("chain ids = %v, want %v", cp.ids, want)
		}
	}
}

// ChainBeyond: true only when the provider is a chain with a link other than
// the given id — so a default opus primary still gets the opus→…→grok tail.
func TestChainBeyond(t *testing.T) {
	if ChainBeyond(nil, "anything") {
		t.Error("nil provider is not a chain")
	}
	if ChainBeyond(&stubProvider{name: "x"}, "x") {
		t.Error("non-chain provider should be false")
	}
	opus := ResolveChainID("opus")
	if opus == "" {
		t.Skip("opus did not resolve")
	}
	// A multi-link chain wrapping the same primary still adds failover beyond it.
	full := NewChain("opus", "gpt-5.5", "glm", "grok")
	if !ChainBeyond(full, opus) {
		t.Error("opus-first chain should offer links beyond opus")
	}
	// A single-link chain that IS the primary adds nothing.
	solo := NewChain("opus")
	if ChainBeyond(solo, opus) {
		t.Error("opus-only chain offers nothing beyond opus")
	}
}

// A chain whose links are all quota-frozen exhausts with a clear error rather
// than masking it — and freezing propagates process-wide.
func TestChainExhaustsWhenAllFrozen(t *testing.T) {
	clearFrozenProviders()
	t.Cleanup(clearFrozenProviders)

	// Build a chain over two real catalog ids, then freeze both their providers
	// so every link is skipped before any network call.
	a, b := ResolveChainID("opus"), ResolveChainID("grok")
	if a == "" || b == "" || a == b {
		t.Skip("need two distinct credentialed-resolvable ids for this test")
	}
	p := NewChain("opus", "grok")
	cp, ok := p.(*chainProvider)
	if !ok {
		t.Fatalf("NewChain returned %T", p)
	}
	for _, id := range cp.ids {
		FreezeProvider(canonicalProvider(ResolveProvider("", id)))
	}
	_, err := cp.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("frozen chain should error, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("want exhaustion error, got %v", err)
	}
}

// firstReachableID skips a frozen provider and reports the next reachable one as
// the headline model — what Name()/ModelID() return.
func TestChainHeadlineSkipsFrozen(t *testing.T) {
	clearFrozenProviders()
	t.Cleanup(clearFrozenProviders)

	a, b := ResolveChainID("opus"), ResolveChainID("grok")
	if a == "" || b == "" || a == b {
		t.Skip("need two distinct resolvable ids")
	}
	// Only meaningful if BOTH resolve to credentialed providers in this env;
	// otherwise modelCredentialed already filters them and the test is vacuous.
	if !modelCredentialed(a) || !modelCredentialed(b) {
		t.Skip("both links must be credentialed to exercise the freeze-skip path")
	}
	p := NewChain("opus", "grok").(*chainProvider)
	if p.ModelID() != a {
		t.Fatalf("headline = %q, want first link %q", p.ModelID(), a)
	}
	FreezeProvider(canonicalProvider(ResolveProvider("", a)))
	if p.ModelID() != b {
		t.Fatalf("after freezing first link, headline = %q, want %q", p.ModelID(), b)
	}
}

type streamStubProvider struct {
	name        string
	reply       string
	err         error
	streamCalls int
}

func (s *streamStubProvider) Name() string    { return s.name }
func (s *streamStubProvider) ModelID() string { return s.name }
func (s *streamStubProvider) Complete(context.Context, Request) (*Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &Response{Text: s.reply}, nil
}
func (s *streamStubProvider) Stream(_ context.Context, _ Request, sink StreamSink) (*Response, error) {
	s.streamCalls++
	if s.err != nil {
		return nil, s.err
	}
	if sink != nil && s.reply != "" {
		sink(StreamChunk{Kind: ChunkText, Text: s.reply})
	}
	return &Response{Text: s.reply}, nil
}

func writeNoAuthChainCatalog(t *testing.T) (badID, goodID string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	badID, goodID = "bad-chain-model", "good-chain-model"
	path := CustomProvidersPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cat := `{
  "providers": [
    {"name":"chainbad","type":"openai_chat","base_url":"http://127.0.0.1:9/v1","no_auth":true,"models":[{"name":"bad-chain-model"}]},
    {"name":"chaingood","type":"openai_chat","base_url":"http://127.0.0.1:9/v1","no_auth":true,"models":[{"name":"good-chain-model"}]}
  ]
}`
	if err := os.WriteFile(path, []byte(cat), 0o600); err != nil {
		t.Fatal(err)
	}
	return badID, goodID
}

func TestChainStreamFallsThroughOnQuota(t *testing.T) {
	clearFrozenProviders()
	t.Cleanup(clearFrozenProviders)
	badID, goodID := writeNoAuthChainCatalog(t)

	primary := &streamStubProvider{name: badID, err: errors.New(`HTTP 429: {"code":"1113","message":"Insufficient balance"}`)}
	fallback := &streamStubProvider{name: goodID, reply: "fallback-ok"}
	cp := &chainProvider{ids: []string{badID, goodID}, links: []Provider{primary, fallback}}

	var noticed FallbackNotice
	ctx := WithFallbackNotifier(context.Background(), func(n FallbackNotice) { noticed = n })
	var streamed strings.Builder
	resp, err := cp.Stream(ctx, Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}, func(c StreamChunk) {
		streamed.WriteString(c.Text)
	})
	if err != nil {
		t.Fatalf("chain stream should fall through on quota: %v", err)
	}
	if resp.Text != "fallback-ok" || streamed.String() != "fallback-ok" {
		t.Fatalf("fallback response/stream = %q/%q, want fallback-ok", resp.Text, streamed.String())
	}
	if primary.streamCalls != 1 || fallback.streamCalls != 1 {
		t.Fatalf("stream calls primary=%d fallback=%d, want 1/1", primary.streamCalls, fallback.streamCalls)
	}
	if noticed.PrimaryID != badID || noticed.FallbackID != goodID || noticed.Cause == nil {
		t.Fatalf("fallback notice = %+v, want %s -> %s with cause", noticed, badID, goodID)
	}
	if !providerFrozen(canonicalProvider(ResolveProvider("", badID))) {
		t.Fatal("quota-hit provider should be frozen process-wide")
	}
	if providerFrozen(canonicalProvider(ResolveProvider("", goodID))) {
		t.Fatal("fallback provider should not be frozen")
	}
}

func TestCloneProviderPreservesChain(t *testing.T) {
	cp := &chainProvider{ids: []string{"glm-5.2", "us.anthropic.claude-opus-4-8"}, links: make([]Provider, 2)}
	cloned, err := CloneProvider(cp)
	if err != nil {
		t.Fatalf("CloneProvider(chain) error: %v", err)
	}
	cc, ok := cloned.(*chainProvider)
	if !ok {
		t.Fatalf("CloneProvider(chain) = %T, want *chainProvider", cloned)
	}
	if strings.Join(cc.ids, ",") != strings.Join(cp.ids, ",") {
		t.Fatalf("cloned ids = %v, want %v", cc.ids, cp.ids)
	}
	if len(cc.links) != len(cp.links) || (len(cc.links) > 0 && cc.links[0] != nil) {
		t.Fatalf("clone should have a fresh lazy link cache, got %#v", cc.links)
	}
}
