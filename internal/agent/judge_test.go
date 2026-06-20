package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// scriptedJudge returns a fixed verdict text.
type scriptedJudge struct {
	reply string
	asked string
}

func (j *scriptedJudge) Name() string    { return "judge" }
func (j *scriptedJudge) ModelID() string { return "judge" }
func (j *scriptedJudge) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	j.asked = req.Messages[0].Text
	return &llm.Response{Text: j.reply}, nil
}

func TestJudgeGoalConfirmedClearsGoal(t *testing.T) {
	var notes []string
	a := &Agent{OnEvent: func(e Event) {
		if e.Kind == EventNote {
			notes = append(notes, e.Text)
		}
	}}
	a.SetGoal("ship the parser")
	j := &scriptedJudge{reply: `{"verdict":"ACHIEVED","summary":"Tests pass and the parser handles all cases.","gaps":[]}`}
	ok, reason, err := a.JudgeGoal(context.Background(), j, "rewrote parser, go test green")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("verdict should be achieved")
	}
	if !strings.Contains(reason, "Tests pass") || !strings.Contains(reason, "Judge summary") {
		t.Fatalf("reason should carry the judge's summary: %q", reason)
	}
	if a.CurrentGoal() != "" {
		t.Fatal("confirmed verdict must clear the goal")
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "judge-confirmed") {
		t.Fatalf("confirmation should emit a note, got %v", notes)
	}
	// The judge must have seen both goal and evidence as JSON data strings.
	if !strings.Contains(j.asked, `"ship the parser"`) || !strings.Contains(j.asked, `"rewrote parser, go test green"`) {
		t.Fatalf("judge prompt missing JSON-quoted goal/evidence:\n%s", j.asked)
	}
}

func TestJudgeGoalRejectedKeepsGoalAndSurfacesGaps(t *testing.T) {
	a := &Agent{}
	a.SetGoal("ship the parser")
	j := &scriptedJudge{reply: `{
		"verdict":"NOT_ACHIEVED",
		"summary":"The evidence lacks verification.",
		"gaps":[{"category":"missing_evidence","requirement":"Parser test suite passes","observed":"No test output was provided","needed":"A passing parser test run","next_step":"Run go test ./internal/parser and include the output"}]
	}`}
	ok, reason, err := a.JudgeGoal(context.Background(), j, "I think it works")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("verdict should be not-achieved")
	}
	for _, want := range []string{"Judge summary", "Gaps:", "missing_evidence", "Parser test suite", "No test output", "Run go test"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("rejection reason missing %q:\n%s", want, reason)
		}
	}
	if a.CurrentGoal() != "ship the parser" {
		t.Fatal("rejected verdict must keep the goal")
	}
}

func TestJudgePromptAsksForStructuredActionableGaps(t *testing.T) {
	a := &Agent{}
	a.SetGoal("ship the parser")
	j := &scriptedJudge{reply: `{"verdict":"NOT_ACHIEVED","summary":"Needs a fixture.","gaps":[{"category":"untested","requirement":"comment fixture","observed":"missing","needed":"fixture output","next_step":"add the fixture"}]}`}
	_, reason, err := a.JudgeGoal(context.Background(), j, "go test green")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "comment fixture") {
		t.Fatalf("rejection gap should be surfaced: %q", reason)
	}
	for _, want := range []string{"JSON object", "gaps", "requirement", "observed", "needed", "next_step", "quality_bar"} {
		if !strings.Contains(j.asked, want) {
			t.Fatalf("judge prompt should demand structured gap detail %q; prompt:\n%s", want, j.asked)
		}
	}
}

func TestJudgeGoalFailsClosed(t *testing.T) {
	a := &Agent{}
	a.SetGoal("g")
	// Unparseable verdict → not achieved with a contract gap.
	j := &scriptedJudge{reply: "hmm, it looks pretty good I guess?"}
	ok, reason, err := a.JudgeGoal(context.Background(), j, "stuff")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unparseable verdict must fail closed")
	}
	if !strings.Contains(reason, "judge_contract") || !strings.Contains(reason, "valid structured gap report") {
		t.Fatalf("unparseable verdict should surface a contract gap: %q", reason)
	}
	if a.CurrentGoal() != "g" {
		t.Fatal("goal must survive an unparseable verdict")
	}
	// No goal set → error.
	a.SetGoal("")
	if _, _, err := a.JudgeGoal(context.Background(), j, "x"); err == nil {
		t.Fatal("no goal should error")
	}
}

func TestParseJudgeReportStrictness(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantOK     bool
		wantReason string
	}{
		{"fenced achieved", "```json\n{\"verdict\":\"ACHIEVED\",\"summary\":\"tests pass\",\"gaps\":[]}\n```", true, "tests pass"},
		{"not achieved with gap", `{"verdict":"NOT_ACHIEVED","summary":"missing tests","gaps":[{"category":"untested","requirement":"coverage","next_step":"add tests"}]}`, false, "add tests"},
		{"achieved with gap fails closed", `{"verdict":"ACHIEVED","summary":"mostly done","gaps":[{"category":"missing_work","requirement":"docs","next_step":"write docs"}]}`, false, "write docs"},
		{"legacy line fails closed", "ACHIEVED: tests pass", false, "judge_contract"},
		{"bare rejection fallback", `{"verdict":"NOT_ACHIEVED","summary":"no","gaps":[]}`, false, "judge_contract"},
		{"duplicate verdict fails closed", `{"verdict":"ACHIEVED","verdict":"NOT_ACHIEVED","summary":"x","gaps":[]}`, false, "judge_contract"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := parseJudgeReport(tc.text)
			if report.achieved() != tc.wantOK || !strings.Contains(report.format(), tc.wantReason) {
				t.Fatalf("parseJudgeReport(%q) = achieved %v, reason %q; want achieved %v containing %q", tc.text, report.achieved(), report.format(), tc.wantOK, tc.wantReason)
			}
		})
	}
}
