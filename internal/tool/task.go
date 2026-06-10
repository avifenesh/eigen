package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskRunner runs a delegated subtask to completion and returns its final
// answer. kind ("general"|"search"|"vision") and difficulty
// ("trivial"|"easy"|"medium"|"hard") are the orchestrator's authoritative
// routing hints; both may be empty (the runner falls back to heuristics). It is
// injected (by main) so this package need not import agent or llm.
type TaskRunner func(ctx context.Context, task, kind, difficulty string) (string, error)

// Task returns the sub-agent delegation tool: it hands a self-contained subtask
// to a fresh agent context and returns only the final result, keeping the main
// conversation focused. Read-only with respect to gating (any mutating tools the
// subtask uses are themselves gated). The optional kind/difficulty let the
// orchestrator route each subtask to the most appropriate model (auto-router).
func Task(run TaskRunner) Definition {
	return Definition{
		Name:        "task",
		Description: "Delegate a self-contained subtask to a fresh agent context and get back only its final result. Use for large, separable chunks of work to keep the main context focused. Give complete instructions; the subtask cannot see this conversation. Optionally set kind (general|search|vision) and difficulty (trivial|easy|medium|hard) to route the subtask to the best-fit model.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": { "type": "string", "description": "Complete, self-contained instructions for the subtask." },
    "kind": { "type": "string", "enum": ["general","search","vision"], "description": "What the subtask needs: general reasoning/coding, live web search, or image understanding. Optional." },
    "difficulty": { "type": "string", "enum": ["trivial","easy","medium","hard"], "description": "How hard the subtask is, so it routes to a strong-enough but not wastefully expensive model. Optional." }
  },
  "required": ["task"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Task       string `json:"task"`
				Kind       string `json:"kind"`
				Difficulty string `json:"difficulty"`
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
			return run(ctx, in.Task, in.Kind, in.Difficulty)
		},
	}
}
