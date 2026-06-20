package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GoalJudge verifies a goal-achievement claim and clears the goal when
// confirmed. It is injected from main (it needs the agent + judge provider).
type GoalJudge func(ctx context.Context, evidence string) (achieved bool, reason string, err error)

// GoalAchieved returns the tool the model calls when it believes the current
// goal is achieved. A judge — a strong model in a fresh context that sees only
// the goal and the evidence, none of the working conversation — verifies the
// claim; only a confirmed verdict clears the goal (and stops the idle goal
// nag). The tool is read-only: it never mutates the project, only the goal
// state, and a rejected claim simply tells the model what is missing.
func GoalAchieved(judge GoalJudge) Definition {
	return Definition{
		Name:        "goal_achieved",
		Description: "Claim the CURRENT GOAL is achieved. A judge model reviews your evidence against the goal in a fresh context; if confirmed, the goal is cleared. Call this when you believe the goal is fully done — include concrete evidence (what was built/changed/verified, test results, observed behavior).",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "evidence": { "type": "string", "description": "Concrete evidence the goal is fully achieved: what was done, how it was verified (tests, output, behavior)." }
  },
  "required": ["evidence"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Evidence string `json:"evidence"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Evidence == "" {
				return "", fmt.Errorf("evidence is required")
			}
			if judge == nil {
				return "", fmt.Errorf("goal judging is not available")
			}
			achieved, reason, err := judge(ctx, in.Evidence)
			if err != nil {
				return "", err
			}
			reason = strings.TrimSpace(reason)
			if reason == "" {
				reason = "the judge did not provide a specific reason"
			}
			if achieved {
				return "Goal CONFIRMED achieved and cleared.\n" + reason, nil
			}
			return "Goal NOT confirmed.\n" + reason + "\nRetry goal_achieved only after closing every listed gap and including concrete evidence.", nil
		},
	}
}
