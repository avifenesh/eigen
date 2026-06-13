package agent

// Goal judging: the model claims the goal is achieved, an independent judge
// (a separate, typically small provider) verifies the claim against the goal,
// and only a confirmed verdict clears the goal. Fail closed: an unavailable
// judge or an unparseable verdict means NOT achieved.

import (
	"context"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// judgePrompt asks for a strict, parseable verdict.
const judgePrompt = `You are a strict completion judge. A coding agent claims it has achieved the goal below. Decide whether the EVIDENCE actually demonstrates the goal is fully achieved.

GOAL:
%s

CLAIMED EVIDENCE:
%s

Rules:
- Only count what the evidence demonstrates; do not assume unstated work.
- Partial progress, plans, or "almost done" are NOT achieved.
- Reply with EXACTLY one line: "ACHIEVED: <one-sentence reason>" or "NOT ACHIEVED: <one-sentence reason>".`

// JudgeGoal asks judge whether evidence demonstrates the agent's current goal
// is achieved. On a confirmed verdict it clears the goal and returns
// (true, reason). On rejection it returns (false, reason) so the model knows
// what is missing. Fail closed on judge errors.
func (a *Agent) JudgeGoal(ctx context.Context, judge llm.Provider, evidence string) (bool, string, error) {
	goal := a.CurrentGoal()
	if goal == "" {
		return false, "", fmt.Errorf("no goal is set")
	}
	if judge == nil {
		// Default: the agent's own provider (race-safe read) in a FRESH
		// context. Judging completion is a hard reasoning task, so it gets
		// the strongest available model; independence comes from the clean
		// context and the strict verdict prompt — the judge sees only the
		// goal and the claimed evidence, none of the working conversation's
		// momentum or self-justification.
		judge = a.provider()
	}
	if judge == nil {
		return false, "", fmt.Errorf("no judge model available")
	}
	resp, err := judge.Complete(ctx, llm.Request{
		System: "You judge task completion claims. Be strict and literal.",
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Text: fmt.Sprintf(judgePrompt, goal, evidence),
		}},
	})
	if err != nil {
		return false, "", fmt.Errorf("judge: %w", err)
	}
	verdict, reason := parseVerdict(resp.Text)
	if verdict {
		a.SetGoal("")
		a.emit(Event{Kind: EventNote, Text: "Goal achieved (judge-confirmed): " + reason})
		return true, reason, nil
	}
	return false, reason, nil
}

// parseVerdict extracts the ACHIEVED / NOT ACHIEVED line. Anything that does
// not clearly say ACHIEVED fails closed.
func parseVerdict(s string) (achieved bool, reason string) {
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		upper := strings.ToUpper(ln)
		switch {
		case strings.HasPrefix(upper, "NOT ACHIEVED"):
			return false, trimVerdictReason(ln)
		case strings.HasPrefix(upper, "ACHIEVED"):
			return true, trimVerdictReason(ln)
		}
	}
	return false, strings.TrimSpace(s)
}

// trimVerdictReason strips the verdict label, returning just the reason text.
func trimVerdictReason(ln string) string {
	if i := strings.Index(ln, ":"); i >= 0 {
		return strings.TrimSpace(ln[i+1:])
	}
	return ln
}

// JudgeClaim verifies a free-standing condition against evidence — like
// JudgeGoal but for an arbitrary claim (workflow step checks), with no
// dependency on the agent's Goal. Independence comes from the fresh context +
// strict prompt; judge may be nil (falls back to the agent's provider).
func (a *Agent) JudgeClaim(ctx context.Context, judge llm.Provider, condition, evidence string) (bool, string, error) {
	if judge == nil {
		judge = a.provider()
	}
	if judge == nil {
		return false, "", fmt.Errorf("no judge model available")
	}
	resp, err := judge.Complete(ctx, llm.Request{
		System: "You judge task completion claims. Be strict and literal.",
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Text: fmt.Sprintf(judgePrompt, condition, evidence),
		}},
	})
	if err != nil {
		return false, "", fmt.Errorf("judge: %w", err)
	}
	verdict, reason := parseVerdict(resp.Text)
	return verdict, reason, nil
}
