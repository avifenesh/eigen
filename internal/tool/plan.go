package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// Planner runs an adversarial cross-vendor planning council and returns the
// hardened plan. Injected from main (it needs provider construction + the
// cross-vendor rule, same as Reviewer).
type Planner func(ctx context.Context, task, context string) (string, error)

// Plan returns the adversarial planning tool. The active model AUTHORS a plan
// and a model from the OTHER vendor adversarially critiques it (GPT×Claude);
// the author revises until the adversary approves or the round budget runs out.
// Use for HARD/ambiguous/expensive tasks before writing code — two vendors
// rarely share a blind spot, so the converged plan is materially harder than a
// solo plan. Returns the hardened plan + any unresolved objections.
func Plan(run Planner) Definition {
	return Definition{
		Name:        "plan",
		Description: "Get an adversarial cross-vendor plan for a hard/ambiguous task BEFORE implementing. The active model drafts a step-by-step plan and an independent model from the OTHER vendor (GPT⇄Claude) critiques it hard; the draft is revised until the adversary approves or the round budget runs out. Returns the hardened plan plus any unresolved objections. Use it when the approach is non-obvious, the change is expensive to get wrong, or you want a second vendor's eyes before committing to a design.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": { "type": "string", "description": "The task to plan: what needs to be built/changed and any goals or constraints. Be specific." },
    "context": { "type": "string", "description": "Optional grounding the planners should assume: relevant code facts, file paths, prior decisions, constraints." }
  },
  "required": ["task"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Task    string `json:"task"`
				Context string `json:"context"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Task == "" {
				return "", fmt.Errorf("task is required")
			}
			if run == nil {
				return "", fmt.Errorf("adversarial planning is not available")
			}
			return run(ctx, in.Task, in.Context)
		},
	}
}
