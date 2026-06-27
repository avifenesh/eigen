package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// moaStub is an in-package Provider stub: it records the request it received and
// returns a canned response (or error), optionally after a delay.
type moaStub struct {
	id    string
	reply string
	err   error
	delay time.Duration

	calls   int32
	gotReq  Request
	gotTool int // number of tool specs seen on the last call
}

func (s *moaStub) Name() string    { return s.id }
func (s *moaStub) ModelID() string { return s.id }
func (s *moaStub) Complete(ctx context.Context, req Request) (*Response, error) {
	atomic.AddInt32(&s.calls, 1)
	s.gotReq = req
	s.gotTool = len(req.Tools)
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &Response{Text: s.reply}, nil
}

// ── storage / validation ────────────────────────────────────────────────────

func TestMoAPresetSaveLoadAndModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := MoAPreset{
		Name:       "review",
		References: []string{"openai.gpt-5.5"},
		Aggregator: "us.anthropic.claude-opus-4-8",
	}
	if err := UpsertMoAPreset(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMoAPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "review" {
		t.Fatalf("presets wrong: %+v", got)
	}
	// Surfaced as a pickable model under provider "moa", inheriting the
	// aggregator's window (opus = 200k) and capabilities.
	mi, ok := Lookup("review")
	if !ok || mi.Provider != "moa" {
		t.Fatalf("preset lookup wrong: %+v ok=%v", mi, ok)
	}
	if mi.ContextWindow != 200000 {
		t.Fatalf("preset should inherit aggregator window 200000, got %d", mi.ContextWindow)
	}
	if !mi.Vision || !mi.Reasoning {
		t.Fatalf("preset should inherit aggregator caps (vision+reasoning): %+v", mi)
	}
	if got := DefaultModel("moa"); got != "review" {
		t.Fatalf("DefaultModel(moa) = %q, want review", got)
	}
	if got := ResolveProvider("", "review"); got != "moa" {
		t.Fatalf("ResolveProvider(review) = %q, want moa", got)
	}
}

func TestMoAValidationRejectsBadPresets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Seed a sibling preset so the recursion guard can see it.
	if err := UpsertMoAPreset(MoAPreset{Name: "base", References: []string{"openai.gpt-5.5"}, Aggregator: "us.anthropic.claude-opus-4-8"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		preset MoAPreset
		want   string
	}{
		{"no refs", MoAPreset{Name: "x", Aggregator: "openai.gpt-5.5"}, "at least one reference"},
		{"no aggregator", MoAPreset{Name: "x", References: []string{"openai.gpt-5.5"}}, "aggregator model is required"},
		{"reserved name", MoAPreset{Name: "moa", References: []string{"openai.gpt-5.5"}, Aggregator: "openai.gpt-5.5"}, "reserved provider name"},
		{"collides with catalog", MoAPreset{Name: "glm-5.2", References: []string{"openai.gpt-5.5"}, Aggregator: "openai.gpt-5.5"}, "collides"},
		{"aggregator is moa tag", MoAPreset{Name: "x", References: []string{"openai.gpt-5.5"}, Aggregator: "moa:base"}, "aggregator"},
		{"reference is a preset", MoAPreset{Name: "x", References: []string{"base"}, Aggregator: "openai.gpt-5.5"}, "reference"},
		{"name has colon", MoAPreset{Name: "a:b", References: []string{"openai.gpt-5.5"}, Aggregator: "openai.gpt-5.5"}, "whitespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMoAPreset(tc.preset)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

// ── reference message trimming ───────────────────────────────────────────────

func TestMoAReferenceMessagesStripsSystemAndToolHistory(t *testing.T) {
	req := Request{
		System: "huge eigen system prompt",
		Messages: []Message{
			{Role: RoleUser, Text: "do the thing"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "f"}}}, // tool-call-only
			{Role: RoleTool, ToolCallID: "c1", Text: "tool result"},
			{Role: RoleAssistant, Text: "here is my answer"},
		},
	}
	got := moaReferenceMessages(req)
	if len(got) != 2 {
		t.Fatalf("want 2 trimmed messages, got %d: %+v", len(got), got)
	}
	for _, m := range got {
		if m.Role != RoleUser && m.Role != RoleAssistant {
			t.Fatalf("unexpected role %q survived trimming", m.Role)
		}
		if len(m.ToolCalls) != 0 {
			t.Fatalf("tool calls should be stripped: %+v", m)
		}
	}
	if got[0].Text != "do the thing" || got[1].Text != "here is my answer" {
		t.Fatalf("trimmed text wrong: %+v", got)
	}
}

func TestMoAReferenceMessagesFallsBackToLastUser(t *testing.T) {
	req := Request{Messages: []Message{
		{Role: RoleTool, Text: "leftover"},
		{Role: RoleUser, Text: "the question"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "z"}}},
	}}
	// The assistant turn is tool-call-only (dropped); the tool turn is dropped;
	// the single user turn survives.
	got := moaReferenceMessages(req)
	if len(got) != 1 || got[0].Text != "the question" {
		t.Fatalf("fallback wrong: %+v", got)
	}
}

// ── tail-append (cache stability) ────────────────────────────────────────────

func TestMoAAppendToLastUserKeepsPrefix(t *testing.T) {
	req := Request{
		System: "stable system",
		Messages: []Message{
			{Role: RoleUser, Text: "first"},
			{Role: RoleAssistant, Text: "answer"},
			{Role: RoleUser, Text: "second"},
		},
	}
	out := appendToLastUser(req, "GUIDANCE")
	if out.System != "stable system" {
		t.Fatal("system prompt must be untouched")
	}
	if out.Messages[0].Text != "first" || out.Messages[1].Text != "answer" {
		t.Fatal("prior messages must be byte-stable (prompt cache)")
	}
	if !strings.HasSuffix(out.Messages[2].Text, "GUIDANCE") || !strings.HasPrefix(out.Messages[2].Text, "second") {
		t.Fatalf("guidance must append to the tail of the last user msg, got %q", out.Messages[2].Text)
	}
	// The input request must not be mutated (copy semantics).
	if req.Messages[2].Text != "second" {
		t.Fatal("appendToLastUser must not mutate the input request")
	}
}

// ── runtime: aggregator is the actor ─────────────────────────────────────────

func TestMoAAggregatorIsActorWithTools(t *testing.T) {
	ref := &moaStub{id: "ref", reply: "reference advice"}
	agg := &moaStub{id: "agg", reply: "aggregator acted"}
	m := &moaProvider{
		preset:     "review",
		references: []Provider{ref},
		refIDs:     []string{"ref"},
		aggregator: agg,
		aggID:      "agg",
		enabled:    true,
	}
	req := Request{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Text: "solve this"}},
		Tools:    []ToolSpec{{Name: "read"}, {Name: "edit"}},
	}
	resp, err := m.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "aggregator acted" {
		t.Fatalf("MoA response must be the aggregator's, got %q", resp.Text)
	}
	if ref.calls != 1 || agg.calls != 1 {
		t.Fatalf("calls: ref=%d agg=%d (want 1,1)", ref.calls, agg.calls)
	}
	// Reference saw NO tools and NO system prompt.
	if ref.gotTool != 0 {
		t.Fatalf("reference must get no tool schema, got %d", ref.gotTool)
	}
	if ref.gotReq.System != "" {
		t.Fatalf("reference must get no system prompt, got %q", ref.gotReq.System)
	}
	// Aggregator kept the full tool schema and system prompt.
	if agg.gotTool != 2 {
		t.Fatalf("aggregator must keep the tool schema, got %d", agg.gotTool)
	}
	if agg.gotReq.System != "sys" {
		t.Fatalf("aggregator must keep the system prompt, got %q", agg.gotReq.System)
	}
	// Aggregator's last user message carries the reference context at the tail,
	// matching hermes-agent's "[Mixture of Agents reference context]" block.
	last := agg.gotReq.Messages[len(agg.gotReq.Messages)-1]
	if !strings.Contains(last.Text, "[Mixture of Agents reference context]") || !strings.Contains(last.Text, "reference advice") {
		t.Fatalf("aggregator must receive reference context, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "Reference 1 — ref:") {
		t.Fatalf("reference must be labelled, got %q", last.Text)
	}
	if !strings.HasPrefix(last.Text, "solve this") {
		t.Fatalf("guidance must be appended after the user's prompt, got %q", last.Text)
	}
}

// TestMoAFailedReferenceKeptAsNote mirrors hermes-agent: a failed reference is
// NOT dropped — it is included as a labelled "[failed: …]" note so the
// aggregator still sees that a perspective was attempted, and still acts.
func TestMoAFailedReferenceKeptAsNote(t *testing.T) {
	r1 := &moaStub{id: "r1", err: errors.New("boom-1")}
	r2 := &moaStub{id: "r2", reply: "real advice"}
	agg := &moaStub{id: "agg", reply: "acted"}
	m := &moaProvider{
		preset:     "p",
		references: []Provider{r1, r2},
		refIDs:     []string{"r1", "r2"},
		aggregator: agg,
		aggID:      "agg",
		enabled:    true,
	}
	req := Request{Messages: []Message{{Role: RoleUser, Text: "solve"}}}
	resp, err := m.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "acted" {
		t.Fatalf("aggregator must still act, got %q", resp.Text)
	}
	last := agg.gotReq.Messages[len(agg.gotReq.Messages)-1].Text
	if !strings.Contains(last, "Reference 1 — r1:\n[failed:") {
		t.Fatalf("failed reference must be kept as a labelled note, got %q", last)
	}
	if !strings.Contains(last, "Reference 2 — r2:\nreal advice") {
		t.Fatalf("usable reference must appear, got %q", last)
	}
}

func TestMoADisabledPresetSkipsReferences(t *testing.T) {
	ref := &moaStub{id: "ref", reply: "advice"}
	agg := &moaStub{id: "agg", reply: "alone"}
	m := &moaProvider{
		preset:     "solo",
		references: []Provider{ref}, // present but must be ignored when disabled
		refIDs:     []string{"ref"},
		aggregator: agg,
		aggID:      "agg",
		enabled:    false,
	}
	req := Request{Messages: []Message{{Role: RoleUser, Text: "q"}}}
	resp, err := m.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "alone" {
		t.Fatalf("want aggregator-alone response, got %q", resp.Text)
	}
	if ref.calls != 0 {
		t.Fatalf("disabled preset must NOT call references, got %d", ref.calls)
	}
	// Aggregator got the unmodified user message (no guidance appended).
	if agg.gotReq.Messages[len(agg.gotReq.Messages)-1].Text != "q" {
		t.Fatalf("disabled preset must not append guidance, got %q", agg.gotReq.Messages[len(agg.gotReq.Messages)-1].Text)
	}
}

// ── runtime: parallel references with isolated failure ───────────────────────

func TestMoAReferencesRunInParallelAndIsolateFailure(t *testing.T) {
	// Three references each sleep; wall-time must approximate one call, not three.
	r1 := &moaStub{id: "r1", reply: "ok-1", delay: 200 * time.Millisecond}
	r2 := &moaStub{id: "r2", err: errors.New("kaboom"), delay: 10 * time.Millisecond}
	r3 := &moaStub{id: "r3", reply: "ok-3", delay: 200 * time.Millisecond}
	agg := &moaStub{id: "agg", reply: "done"}
	m := &moaProvider{
		preset:     "p",
		references: []Provider{r1, r2, r3},
		refIDs:     []string{"r1", "r2", "r3"},
		aggregator: agg,
		aggID:      "agg",
		enabled:    true,
	}
	req := Request{Messages: []Message{{Role: RoleUser, Text: "go"}}}

	start := time.Now()
	if _, err := m.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > 380*time.Millisecond {
		t.Fatalf("references did not run in parallel (took %v)", elapsed)
	}
	guidance := agg.gotReq.Messages[len(agg.gotReq.Messages)-1].Text
	// All references appear in preset order with stable "Reference N" labels;
	// a failure is kept as a labelled "[failed: …]" note (hermes-agent behavior).
	if !strings.Contains(guidance, "Reference 1 — r1:\nok-1") {
		t.Fatalf("missing/ordered r1 output: %q", guidance)
	}
	if !strings.Contains(guidance, "Reference 2 — r2:\n[failed:") {
		t.Fatalf("failed r2 must be kept as a labelled note: %q", guidance)
	}
	if !strings.Contains(guidance, "Reference 3 — r3:\nok-3") {
		t.Fatalf("missing/ordered r3 output: %q", guidance)
	}
}

// ── runtime: capability forwarding to the aggregator ─────────────────────────

type moaEffortStub struct {
	moaStub
	effort string
}

func (s *moaEffortStub) SetEffort(level string) bool { s.effort = level; return true }
func (s *moaEffortStub) Effort() string              { return s.effort }

func TestMoAForwardsEffortToAggregator(t *testing.T) {
	agg := &moaEffortStub{moaStub: moaStub{id: "agg", reply: "x"}, effort: "low"}
	m := &moaProvider{preset: "p", aggregator: agg, aggID: "agg", enabled: false}
	es, ok := Provider(m).(EffortSetter)
	if !ok {
		t.Fatal("moaProvider must implement EffortSetter")
	}
	if !es.SetEffort("high") {
		t.Fatal("SetEffort should forward to the aggregator")
	}
	if es.Effort() != "high" {
		t.Fatalf("effort not forwarded, got %q", es.Effort())
	}
}

// ── runtime recursion guard ──────────────────────────────────────────────────

func TestMoANewProviderRejectsRecursiveSlots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// moaRefTargetsMoA must catch both a bare preset name and a moa: tag, and
	// must NOT flag a normal model.
	if err := SaveMoAPresets([]MoAPreset{
		{Name: "base", References: []string{"openai.gpt-5.5"}, Aggregator: "us.anthropic.claude-opus-4-8"},
	}); err != nil {
		t.Fatal(err)
	}
	if !moaRefTargetsMoA("base") {
		t.Fatal("moaRefTargetsMoA should detect a bare preset name")
	}
	if !moaRefTargetsMoA("moa:base") {
		t.Fatal("moaRefTargetsMoA should detect an explicit moa: tag")
	}
	if moaRefTargetsMoA("openai.gpt-5.5") {
		t.Fatal("moaRefTargetsMoA must not flag a normal model")
	}

	// Write a recursive preset DIRECTLY to disk (bypassing SaveMoAPresets'
	// validation) so the *runtime* guard in newMoAProvider is what's tested: it
	// must error at construction, never recurse through New into itself.
	path := MoAPresetsPath()
	raw := `{"presets":[
		{"name":"base","references":["openai.gpt-5.5"],"aggregator":"us.anthropic.claude-opus-4-8"},
		{"name":"loop","references":["openai.gpt-5.5"],"aggregator":"base"}
	]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	// LoadMoAPresets validates, so the recursive "loop" is dropped on load — but
	// newMoAProvider also reads the file. Guard against a hang with a deadline.
	done := make(chan error, 1)
	go func() {
		_, err := newMoAProvider("loop")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("recursive aggregator must be rejected, not constructed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("newMoAProvider hung on a recursive preset (recursion guard failed)")
	}
}
