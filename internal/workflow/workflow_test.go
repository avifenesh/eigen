package workflow

import (
	"context"
	"strings"
	"testing"
)

func TestParseBasic(t *testing.T) {
	wf, err := Parse(`---
name: review-pr
description: review a pull request
---

## summarize
Read the diff and summarize the change.

## review
model: us.anthropic.claude-opus-4-8
check: the review names concrete issues with file locations
on_failure: retry
retries: 2
Critique {{var.pr}} for correctness and edge cases.
`)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Name != "review-pr" || wf.Description != "review a pull request" {
		t.Fatalf("frontmatter: %+v", wf)
	}
	if len(wf.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(wf.Steps))
	}
	s0, s1 := wf.Steps[0], wf.Steps[1]
	if s0.ID != "summarize" || !strings.Contains(s0.Prompt, "summarize the change") {
		t.Fatalf("step0: %+v", s0)
	}
	if s0.OnFailure != FailStop {
		t.Fatalf("default on_failure should be stop, got %q", s0.OnFailure)
	}
	if s1.ID != "review" || s1.Model != "us.anthropic.claude-opus-4-8" {
		t.Fatalf("step1 model: %+v", s1)
	}
	if s1.Check == "" || s1.OnFailure != FailRetry || s1.Retries != 2 {
		t.Fatalf("step1 directives: %+v", s1)
	}
	if !strings.Contains(s1.Prompt, "{{var.pr}}") {
		t.Fatalf("step1 prompt should keep the placeholder: %q", s1.Prompt)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	wf, err := Parse("## one\nDo a thing.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(wf.Steps) != 1 || wf.Steps[0].ID != "one" {
		t.Fatalf("steps: %+v", wf.Steps)
	}
}

func TestParseErrors(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"no steps", "---\nname: x\n---\njust prose, no sections"},
		{"empty prompt", "## a\non_failure: stop\n## b\nreal prompt"},
		{"dup id", "## a\nfirst\n## a\nsecond"},
		{"bad on_failure", "## a\non_failure: explode\nbody"},
	} {
		if _, err := Parse(c.src); err == nil {
			t.Errorf("%s: expected a parse error", c.name)
		}
	}
}

func TestInterpolate(t *testing.T) {
	out, missing := Interpolate("review {{var.pr}} on {{var.branch}}", map[string]string{"pr": "#42"})
	if out != "review #42 on " {
		t.Fatalf("interp = %q", out)
	}
	if len(missing) != 1 || missing[0] != "branch" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestPromptLineLookingLikeDirective(t *testing.T) {
	// A prose prompt starting with "word:" must not be eaten as a directive.
	wf, err := Parse("## a\nNote: do the thing carefully.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wf.Steps[0].Prompt, "Note: do the thing") {
		t.Fatalf("prose 'Note:' should stay in the prompt: %q", wf.Steps[0].Prompt)
	}
}

func TestRunCarriesAndChecks(t *testing.T) {
	wf, err := Parse(`## a
First step.
## b
check: output mentions done
on_failure: stop
Second step.`)
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	runner := func(_ context.Context, prompt, model string) (string, error) {
		prompts = append(prompts, prompt)
		return "step output: done", nil
	}
	judge := func(_ context.Context, cond, out string) (bool, string, error) {
		return strings.Contains(out, "done"), "checked", nil
	}
	res, err := wf.Run(context.Background(), RunOpts{Run: runner, Judge: judge})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Completed) != 2 {
		t.Fatalf("both steps should complete: %+v", res)
	}
	if len(prompts) != 2 || prompts[0] != "First step." {
		t.Fatalf("steps run in order: %v", prompts)
	}
}

func TestRunStopsOnFailedCheck(t *testing.T) {
	wf, _ := Parse("## a\ncheck: impossible condition\nonly step.")
	runner := func(_ context.Context, p, m string) (string, error) { return "nope", nil }
	judge := func(_ context.Context, c, o string) (bool, string, error) { return false, "did not meet", nil }
	res, err := wf.Run(context.Background(), RunOpts{Run: runner, Judge: judge})
	if err == nil {
		t.Fatal("a failed stop-check should error (nonzero exit)")
	}
	if res.FailedAt != "a" {
		t.Fatalf("FailedAt = %q", res.FailedAt)
	}
}

func TestRunRetryThenSucceed(t *testing.T) {
	wf, _ := Parse("## a\ncheck: ok\non_failure: retry\nretries: 2\ndo it.")
	n := 0
	runner := func(_ context.Context, p, m string) (string, error) { n++; return "attempt", nil }
	judge := func(_ context.Context, c, o string) (bool, string, error) { return n >= 2, "n", nil }
	res, err := wf.Run(context.Background(), RunOpts{Run: runner, Judge: judge})
	if err != nil {
		t.Fatalf("retry should recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("should have retried once (n=%d)", n)
	}
	if len(res.Completed) != 1 {
		t.Fatal("step should complete after retry")
	}
}

func TestRunContinueOnFailure(t *testing.T) {
	wf, _ := Parse("## a\ncheck: ok\non_failure: continue\nstep one.\n## b\nstep two.")
	runner := func(_ context.Context, p, m string) (string, error) { return "x", nil }
	judge := func(_ context.Context, c, o string) (bool, string, error) { return false, "no", nil }
	res, err := wf.Run(context.Background(), RunOpts{Run: runner, Judge: judge})
	if err != nil {
		t.Fatalf("continue should not error: %v", err)
	}
	if len(res.Completed) != 2 {
		t.Fatalf("both steps should be recorded with continue: %+v", res)
	}
}
