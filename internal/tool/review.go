package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// Reviewer runs a cross-vendor review of an artifact and returns the critique.
// Injected from main (it needs provider construction + the vendor rule: GPT
// reviews Claude, Claude reviews GPT — never self-review).
type Reviewer func(ctx context.Context, artifact, focus string) (string, error)

// Review returns the cross-vendor review tool. The working model submits an
// artifact (plan, diff, code, design) and an INDEPENDENT model from the other
// vendor critiques it: Claude-authored work is reviewed by GPT (strict
// correctness), GPT-authored work by Claude (design/clarity). Same-family
// models share blind spots, so the reviewer is always the other vendor.
func Review(run Reviewer) Definition {
	return Definition{
		Name:        "review",
		Description: "Request a cross-vendor review: an independent model from the OTHER vendor critiques the artifact (plan, diff, code, approach). Use before committing significant work or when confidence matters. Returns a critique — issues found, risks, and concrete improvement suggestions.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "artifact": { "type": "string", "description": "The work to review: a plan, diff, code, or design. Include enough context to judge it." },
    "focus": { "type": "string", "description": "What the review should focus on (e.g. correctness, security, API design, edge cases). Optional." }
  },
  "required": ["artifact"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Artifact string `json:"artifact"`
				Focus    string `json:"focus"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Artifact == "" {
				return "", fmt.Errorf("artifact is required")
			}
			if run == nil {
				return "", fmt.Errorf("cross-vendor review is not available")
			}
			return run(ctx, in.Artifact, in.Focus)
		},
	}
}
