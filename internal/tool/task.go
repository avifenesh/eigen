package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskRunner runs a delegated subtask to completion and returns its final
// answer. It is injected (by main) so this package need not import agent.
type TaskRunner func(ctx context.Context, task string) (string, error)

// Task returns the sub-agent delegation tool: it hands a self-contained subtask
// to a fresh agent context and returns only the final result, keeping the main
// conversation focused. Read-only with respect to gating (any mutating tools the
// subtask uses are themselves gated).
func Task(run TaskRunner) Definition {
	return Definition{
		Name:        "task",
		Description: "Delegate a self-contained subtask to a fresh agent context and get back only its final result. Use for large, separable chunks of work to keep the main context focused. Give complete instructions; the subtask cannot see this conversation.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": { "type": "string", "description": "Complete, self-contained instructions for the subtask." }
  },
  "required": ["task"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Task string `json:"task"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Task == "" {
				return "", fmt.Errorf("task is required")
			}
			if run == nil {
				return "", fmt.Errorf("subtasks are not available")
			}
			return run(ctx, in.Task)
		},
	}
}
