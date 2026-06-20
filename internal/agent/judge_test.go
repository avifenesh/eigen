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
	j := &scriptedJudge{reply: "ACHIEVED: tests pass and the parser handles all cases"}
	ok, reason, err := a.JudgeGoal(context.Background(), j, "rewrote parser, go test green")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("verdict should be achieved")
	}
	if !strings.Contains(reason, "tests pass") {
		t.Fatalf("reason should carry the judge's words: %q", reason)
	}
	if a.CurrentGoal() != "" {
		t.Fatal("confirmed verdict must clear the goal")
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "judge-confirmed") {
		t.Fatalf("confirmation should emit a note, got %v", notes)
	}
	// The judge must have seen both goal and evidence.
	if !strings.Contains(j.asked, "ship the parser") || !strings.Contains(j.asked, "go test green") {
		t.Fatalf("judge prompt missing goal/evidence:\n%s", j.asked)
	}
}

func TestJudgeGoalRejectedKeepsGoal(t *testing.T) {
	a := &Agent{}
	a.SetGoal("ship the parser")
	j := &scriptedJudge{reply: "NOT ACHIEVED: no test results were provided"}
	ok, reason, err := a.JudgeGoal(context.Background(), j, "I think it works")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("verdict should be not-achieved")
	}
	if !strings.Contains(reason, "no test results") {
		t.Fatalf("rejection reason should be surfaced: %q", reason)
	}
	if a.CurrentGoal() != "ship the parser" {
		t.Fatal("rejected verdict must keep the goal")
	}
}

func TestJudgePromptAsksForActionableRejection(t *testing.T) {
	a := &Agent{}
	a.SetGoal("ship the parser")
	j := &scriptedJudge{reply: "NOT ACHIEVED: add a failing parser fixture for comments"}
	_, reason, err := a.JudgeGoal(context.Background(), j, "go test green")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "parser fixture") {
		t.Fatalf("rejection reason should be surfaced unchanged: %q", reason)
	}
	for _, want := range []string{"actionable", "missing/broken evidence", "feature", "test", "artifact", "quality bar"} {
		if !strings.Contains(j.asked, want) {
			t.Fatalf("judge prompt should demand actionable rejection detail %q; prompt:\n%s", want, j.asked)
		}
	}
}

func TestJudgeGoalFailsClosed(t *testing.T) {
	a := &Agent{}
	a.SetGoal("g")
	// Unparseable verdict → not achieved.
	j := &scriptedJudge{reply: "hmm, it looks pretty good I guess?"}
	ok, _, err := a.JudgeGoal(context.Background(), j, "stuff")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unparseable verdict must fail closed")
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

func TestParseVerdictOrdering(t *testing.T) {
	// NOT ACHIEVED must not be misread as ACHIEVED (prefix check ordering).
	ok, _ := parseVerdict("NOT ACHIEVED: nope")
	if ok {
		t.Fatal("NOT ACHIEVED misparsed as achieved")
	}
	ok, reason := parseVerdict("some preamble\nACHIEVED: solid evidence")
	if !ok || reason != "solid evidence" {
		t.Fatalf("multi-line verdict misparsed: %v %q", ok, reason)
	}
}
