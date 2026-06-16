package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// scriptProvider returns queued responses in order (one per Complete call),
// recording the prompts it saw.
type scriptProvider struct {
	name      string
	responses []string
	i         int
	prompts   []string
}

func (p *scriptProvider) Name() string    { return p.name }
func (p *scriptProvider) ModelID() string { return p.name }
func (p *scriptProvider) Complete(_ context.Context, req Request) (*Response, error) {
	if len(req.Messages) > 0 {
		p.prompts = append(p.prompts, req.Messages[0].Text)
	}
	r := "ok"
	if p.i < len(p.responses) {
		r = p.responses[p.i]
	}
	p.i++
	return &Response{Text: r}, nil
}

func TestCouncilConverges(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{
		"PLAN v1: do X then Y",
	}}
	adversary := &scriptProvider{name: "gpt", responses: []string{
		"This looks complete and correct.\nVERDICT: APPROVE",
	}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "us.anthropic.claude-opus-4-8",
		Adversary: adversary, AdversaryID: "openai.gpt-5.5",
		MaxRounds: 3,
	}, "build a thing", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Converged {
		t.Fatal("should converge on APPROVE")
	}
	if res.Rounds != 1 {
		t.Fatalf("converged in round 1, got %d", res.Rounds)
	}
	if !res.CrossVendor {
		t.Fatal("claude author + gpt adversary should be cross-vendor")
	}
	if res.Plan != "PLAN v1: do X then Y" {
		t.Fatalf("plan should be the approved draft, got %q", res.Plan)
	}
}

func TestCouncilRevisesThenConverges(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{
		"PLAN v1",
		"PLAN v2 (addressed the race)",
	}}
	adversary := &scriptProvider{name: "gpt", responses: []string{
		"You ignored a concurrency race in step 2.\nVERDICT: REVISE",
		"Now correct.\nVERDICT: APPROVE",
	}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "claude-x", Adversary: adversary, AdversaryID: "gpt-x", MaxRounds: 3,
	}, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Converged || res.Rounds != 2 {
		t.Fatalf("should converge in round 2: converged=%v rounds=%d", res.Converged, res.Rounds)
	}
	if res.Plan != "PLAN v2 (addressed the race)" {
		t.Fatalf("final plan should be the revision, got %q", res.Plan)
	}
	// The revise prompt must carry the critique.
	joined := strings.Join(author.prompts, "\n")
	if !strings.Contains(joined, "concurrency race") {
		t.Fatal("author's revise prompt should include the adversary's critique")
	}
}

func TestCouncilHitsRoundCapWithDissent(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{"v1", "v2", "v3"}}
	adversary := &scriptProvider{name: "gpt", responses: []string{
		"problem A\nVERDICT: REVISE",
		"problem B\nVERDICT: REVISE",
		"still problem B\nVERDICT: REVISE",
	}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "claude-x", Adversary: adversary, AdversaryID: "gpt-x", MaxRounds: 2,
	}, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Converged {
		t.Fatal("should NOT converge (always REVISE)")
	}
	if res.Rounds != 2 {
		t.Fatalf("should run the 2-round cap, got %d", res.Rounds)
	}
	if !strings.Contains(res.Dissent, "problem B") {
		t.Fatalf("dissent should carry the last critique, got %q", res.Dissent)
	}
	if strings.Contains(res.Dissent, "VERDICT") {
		t.Fatal("dissent should have the VERDICT line stripped")
	}
}

func TestCouncilSoloWhenNoAdversary(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{"solo plan"}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "claude-x", MaxRounds: 3,
	}, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan != "solo plan" || res.CrossVendor || res.Converged {
		t.Fatalf("solo plan: %+v", res)
	}
}

func TestCouncilAdversaryFailsGraceful(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{"v1"}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "claude-x", Adversary: alwaysErr{}, AdversaryID: "gpt-x", MaxRounds: 3,
	}, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Plan != "v1" {
		t.Fatalf("should return the author draft when adversary fails, got %q", res.Plan)
	}
	if !strings.Contains(res.Dissent, "unavailable") {
		t.Fatalf("dissent should note adversary unavailable, got %q", res.Dissent)
	}
}

type alwaysErr struct{}

func (alwaysErr) Name() string    { return "err" }
func (alwaysErr) ModelID() string { return "err" }
func (alwaysErr) Complete(context.Context, Request) (*Response, error) {
	return nil, fmt.Errorf("boom")
}

func TestLastVerdict(t *testing.T) {
	cases := map[string]string{
		"foo\nVERDICT: APPROVE":             "APPROVE",
		"a\nVERDICT: REVISE\nmore":          "REVISE",
		"VERDICT: APPROVE.":                 "APPROVE",
		"no verdict here":                   "",
		"VERDICT: REVISE\nVERDICT: APPROVE": "APPROVE", // last wins
	}
	for in, want := range cases {
		if got := lastVerdict(in); got != want {
			t.Errorf("lastVerdict(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCouncilFallsToWorkingAdversary(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{"v1"}}
	// primary adversary always errors; fallback approves on first critique
	working := &scriptProvider{name: "grok", responses: []string{"solid plan.\nVERDICT: APPROVE"}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "us.anthropic.claude-opus-4-8",
		Adversary: alwaysErr{}, AdversaryID: "openai.gpt-5.5",
		Fallbacks: []AdversaryOption{{Provider: working, ID: "grok-4"}},
		MaxRounds: 3,
	}, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.AdversaryFailed {
		t.Fatal("should have fallen back to the working adversary, not failed")
	}
	if res.AdversaryID != "grok-4" {
		t.Fatalf("adversary should be the fallback grok-4, got %q", res.AdversaryID)
	}
	if !res.Converged || !res.CrossVendor {
		t.Fatalf("should converge cross-vendor: %+v", res)
	}
}

func TestCouncilAllAdversariesFail(t *testing.T) {
	author := &scriptProvider{name: "claude", responses: []string{"v1"}}
	res, err := Council(context.Background(), CouncilConfig{
		Author: author, AuthorID: "claude-x",
		Adversary: alwaysErr{}, AdversaryID: "gpt-x",
		Fallbacks: []AdversaryOption{{Provider: alwaysErr{}, ID: "grok-x"}},
		MaxRounds: 3,
	}, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.AdversaryFailed || res.Plan != "v1" {
		t.Fatalf("all-fail should mark AdversaryFailed + return draft: %+v", res)
	}
}
