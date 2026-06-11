package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// TaskOpts shapes one delegation request from the model. The main package
// adapts this to agent.SubtaskOpts to avoid an import cycle (agent imports tool).
type TaskOpts struct {
	Kind       string
	Difficulty string
	Model      string
}

// TaskRun is injected by main/buildSession so tool doesn't need to construct
// agents or providers. It returns either the final result (foreground) or a
// background task handle string.
type TaskRun func(ctx context.Context, task string, opts TaskOpts, background bool) (string, error)

// TaskStatusRun is injected by main/buildSession for querying/collecting
// background task state.
type TaskStatusRun func(ctx context.Context, id string, all bool) (string, error)

// Task returns the sub-agent delegation tool. Foreground mode runs to
// completion and returns the final answer. background=true starts a detached
// task and returns immediately with a task id; later use task_status to collect
// the result. kind/difficulty/model are the orchestrator's authoritative
// routing controls: a specific model override beats route selection; otherwise
// explicit kind/difficulty routes to the best-fit model.
func Task(run TaskRun) Definition {
	return Definition{
		Name:        "task",
		Description: "Delegate a self-contained subtask to a fresh agent context. YOU are the orchestrator: state kind (general|search|vision|social) and difficulty (trivial|easy|medium|hard) when delegating so the subtask runs on the best-fit model. Set model to override routing and force a specific model/ref (e.g. grok-code-fast-1, mantle:openai.gpt-5.5, ant:claude-fable-5). Set background=true to start it asynchronously; it will write jsonl under ~/.eigen/tasks and you can later call task_status to collect the result.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": { "type": "string", "description": "Complete, self-contained instructions for the subtask. The subtask cannot see this conversation." },
    "kind": { "type": "string", "enum": ["general","search","vision","social"], "description": "What the subtask needs: general reasoning/coding, live web search, image understanding, or social (X/Twitter reach). Optional but recommended." },
    "difficulty": { "type": "string", "enum": ["trivial","easy","medium","hard"], "description": "Routing ladder: trivial = mechanical/cheap; easy = well-scoped; medium = reasoning/may run long; hard = strongest available. Stating this routes the subtask; omitting it keeps your model unless heuristic routing is enabled." },
    "model": { "type": "string", "description": "Optional explicit model/ref override. Beats routing. Examples: grok-code-fast-1, glm-5.1, mantle:openai.gpt-5.5, ant:claude-fable-5, us.anthropic.claude-opus-4-8." },
    "background": { "type": "boolean", "description": "If true, start the subtask asynchronously and return a task id immediately; use task_status to check/collect later." }
  },
  "required": ["task"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Task       string `json:"task"`
				Kind       string `json:"kind"`
				Difficulty string `json:"difficulty"`
				Model      string `json:"model"`
				Background bool   `json:"background"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(in.Task) == "" {
				return "", fmt.Errorf("task is required")
			}
			if run == nil {
				return "", fmt.Errorf("subtasks are not available")
			}
			return run(ctx, in.Task, TaskOpts{Kind: in.Kind, Difficulty: in.Difficulty, Model: in.Model}, in.Background)
		},
	}
}

// TaskStatus returns a tool to inspect/collect background task results.
func TaskStatus(run TaskStatusRun) Definition {
	return Definition{
		Name:        "task_status",
		Description: "Check background tasks started with task(background=true). With id, returns running/done/error plus result when ready; without id or with all=true, lists known background tasks.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string", "description": "Background task id, e.g. bg-142233-1. Omit to list tasks." },
    "all": { "type": "boolean", "description": "List all known background tasks instead of one id." }
  },
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				ID  string `json:"id"`
				All bool   `json:"all"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if run == nil {
				return "", fmt.Errorf("background tasks are not available")
			}
			return run(ctx, in.ID, in.All)
		},
	}
}
