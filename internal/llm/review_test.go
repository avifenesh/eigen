package llm

import (
	"context"
	"strings"
	"testing"
)

type capProvider struct{ sys, user string }

func (p *capProvider) Name() string { return "cap" }
func (p *capProvider) Complete(_ context.Context, req Request) (*Response, error) {
	p.sys = req.System
	if len(req.Messages) > 0 {
		p.user = req.Messages[0].Text
	}
	return &Response{Text: "looks correct but missing an edge case"}, nil
}

func TestReviewArtifact(t *testing.T) {
	rev := &capProvider{}
	out, err := ReviewArtifact(context.Background(), rev, "openai.gpt-5.5", "us.anthropic.claude-opus-4-8", "func add(a,b int) int { return a-b }", "correctness")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "edge case") {
		t.Fatalf("review text not returned: %q", out)
	}
	// The prompt must frame the author's vendor and the focus.
	if !strings.Contains(rev.user, "Anthropic Claude") {
		t.Errorf("review prompt should name the author vendor:\n%s", rev.user)
	}
	if !strings.Contains(rev.user, "correctness") {
		t.Errorf("review prompt should carry the focus:\n%s", rev.user)
	}
	if !strings.Contains(rev.user, "a-b") {
		t.Errorf("review prompt should include the artifact:\n%s", rev.user)
	}
}

func TestReviewArtifactNilReviewer(t *testing.T) {
	if _, err := ReviewArtifact(context.Background(), nil, "x", "y", "z", ""); err == nil {
		t.Fatal("nil reviewer should error")
	}
}
