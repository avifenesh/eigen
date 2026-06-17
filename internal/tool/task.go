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
	Role       string
}

// TaskRun is injected by main/buildSession so tool doesn't need to construct
// agents or providers. It returns either the final result (foreground) or a
// background task handle string.
type TaskRun func(ctx context.Context, task string, opts TaskOpts, background bool) (string, error)

// TaskStatusRun is injected by main/buildSession for querying/collecting
// background task state.
type TaskStatusRun func(ctx context.Context, id string, all, verbose bool) (string, error)

// GroupSubtaskArg is one child in a task_group fan-out, as the model supplies it.
type GroupSubtaskArg struct {
	Task       string
	Role       string
	Kind       string
	Difficulty string
	Model      string
}

// TaskGroupRun is injected by main/buildSession: run several read-only
// sub-agents in parallel and return a joined report. synthesize, when non-empty,
// runs a final merge step answering that question over the children's reports.
type TaskGroupRun func(ctx context.Context, subs []GroupSubtaskArg, workers int, synthesize string) (string, error)

// Task returns the sub-agent delegation tool. Foreground mode runs to
// completion and returns the final answer. background=true starts a detached
// task and returns immediately with a task id; later use task_status to collect
// the result. kind/difficulty/model are the orchestrator's authoritative
// routing controls: a specific model override beats route selection; otherwise
// explicit kind/difficulty routes to the best-fit model.
func Task(run TaskRun) Definition {
	return Definition{
		Name:        "task",
		Description: "Delegate a self-contained subtask to a fresh agent context. YOU are the orchestrator: state kind (general|search|vision|social) and difficulty (trivial|easy|medium|hard) when delegating so the subtask runs on the best-fit model. Set model to override routing and force a specific model/ref (e.g. grok-code-fast-1, mantle:openai.gpt-5.5, glm-5.2). Optionally set role to a built-in role or an installed plugin-agent role. Set background=true to start it asynchronously; it will write jsonl under ~/.eigen/tasks and you can later call task_status to collect the result.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": { "type": "string", "description": "Complete, self-contained instructions for the subtask. The subtask cannot see this conversation." },
    "kind": { "type": "string", "enum": ["general","search","vision","social"], "description": "What the subtask needs: general reasoning/coding, live web search, image understanding, or social (X/Twitter reach). Optional but recommended." },
    "difficulty": { "type": "string", "enum": ["trivial","easy","medium","hard"], "description": "Routing ladder: trivial = mechanical/cheap; easy = well-scoped; medium = reasoning/may run long; hard = strongest available. Stating this routes the subtask; omitting it keeps your model unless heuristic routing is enabled." },
    "model": { "type": "string", "description": "Optional explicit model/ref override. Beats routing. Examples: grok-code-fast-1, glm-5.2, mantle:openai.gpt-5.5, us.anthropic.claude-opus-4-8." },
    "role": { "type": "string", "description": "Optional named sub-agent role. Built-ins: researcher, reviewer, summarizer. Installed plugin agents are also valid for task (they inherit normal tools and approval gates)." },
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
				Role       string `json:"role"`
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
			return run(ctx, in.Task, TaskOpts{Kind: in.Kind, Difficulty: in.Difficulty, Model: in.Model, Role: in.Role}, in.Background)
		},
	}
}

// TaskStatus returns a tool to inspect/collect background task results.
func TaskStatus(run TaskStatusRun) Definition {
	return Definition{
		Name:        "task_status",
		Description: "Check background tasks started with task(background=true). With id, returns running/done/error plus result when ready; set verbose=true with an id to include attempt history and transcript paths. Without id or with all=true, lists known background tasks.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string", "description": "Background task id, e.g. bg-142233-1. Omit to list tasks." },
    "all": { "type": "boolean", "description": "List all known background tasks instead of one id." },
    "verbose": { "type": "boolean", "description": "With id, include attempt history plus state/transcript file paths. With all=true, include transcript paths in the listing." }
  },
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				ID      string `json:"id"`
				All     bool   `json:"all"`
				Verbose bool   `json:"verbose"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if run == nil {
				return "", fmt.Errorf("background tasks are not available")
			}
			return run(ctx, in.ID, in.All, in.Verbose)
		},
	}
}

// TaskGroup returns the parallel fan-out tool: run several READ-ONLY
// sub-agents at once (each with a role) and get one joined report. Use it to
// investigate or review several things concurrently — e.g. three researchers
// on three files, or a researcher + a reviewer. Children are read-only by
// design (they can't edit/write/run commands), which is what makes running
// them in parallel safe.
func TaskGroup(run TaskGroupRun) Definition {
	return Definition{
		Name:        "task_group",
		Description: "Run several READ-ONLY sub-agents in PARALLEL and get one combined report. Each subtask needs a role: researcher (read+search the codebase), reviewer (critique + cross-vendor review), or summarizer (read + condense). Use this to fan out investigation/review across files or angles at once. Children cannot modify files or run commands — for that, use the `task` tool (one at a time). Optional workers caps concurrency (default 3).",
		ReadOnly:    true, // children are read-only, so the fan-out itself is safe to auto-run
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "subtasks": {
      "type": "array",
      "minItems": 1,
      "maxItems": 8,
      "description": "The parallel subtasks. Each runs in its own fresh read-only agent.",
      "items": {
        "type": "object",
        "properties": {
          "task": { "type": "string", "description": "Complete, self-contained instructions. The child cannot see this conversation." },
          "role": { "type": "string", "enum": ["researcher","reviewer","summarizer"], "description": "researcher = read+search code; reviewer = critique+cross-review; summarizer = read+condense." },
          "kind": { "type": "string", "enum": ["general","search","vision","social"] },
          "difficulty": { "type": "string", "enum": ["trivial","easy","medium","hard"] },
          "model": { "type": "string", "description": "Optional explicit model/ref override (beats routing)." }
        },
        "required": ["task","role"],
        "additionalProperties": false
      }
    },
    "workers": { "type": "integer", "description": "Max concurrent subtasks (default 3, max 6)." },
    "synthesize": { "type": "string", "description": "Optional: a question to answer by MERGING the children's reports into one coherent result (a final reasoning pass over all their findings). Omit to just get the raw per-child reports." }
  },
  "required": ["subtasks"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Subtasks   []GroupSubtaskArg `json:"subtasks"`
				Workers    int               `json:"workers"`
				Synthesize string            `json:"synthesize"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if run == nil {
				return "", fmt.Errorf("task_group is not available")
			}
			return run(ctx, in.Subtasks, in.Workers, in.Synthesize)
		},
	}
}

// TaskGroupMutatingRun is injected by main/buildSession: run implementer
// children in parallel (isolated worktrees) and apply the merged result.
type TaskGroupMutatingRun func(ctx context.Context, subs []GroupSubtaskArg, workers int) (string, error)

// TaskGroupMutating returns the parallel WRITE fan-out tool: several
// implementer sub-agents edit code at once, each in an isolated copy of the
// repo, and their changes are merged back behind one approval. NOT read-only
// (the merge mutates the workspace), so it needs approval in gated mode.
func TaskGroupMutating(run TaskGroupMutatingRun) Definition {
	return Definition{
		Name:        "task_group_mutating",
		Description: "Run several IMPLEMENTER sub-agents in PARALLEL to make code changes at once — each works in its own isolated git worktree, then their diffs are merged back to your workspace behind ONE approval (conflicting patches are reported, not forced). Requires: a git repo, the session rooted at the repo root, and a CLEAN working tree (commit/stash first). Children have read/write/edit but NO shell/git/network. Use for independent edits across files; for one focused change use the `task` tool. Optional workers caps concurrency (default 3).",
		ReadOnly:    false, // merging applies real changes — gate the delegation
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "subtasks": {
      "type": "array",
      "minItems": 1,
      "maxItems": 8,
      "description": "Independent implementation subtasks, each run in its own isolated repo copy.",
      "items": {
        "type": "object",
        "properties": {
          "task": { "type": "string", "description": "Complete, self-contained change instructions. Keep edits tightly scoped so they merge cleanly." },
          "kind": { "type": "string", "enum": ["general","search","vision","social"] },
          "difficulty": { "type": "string", "enum": ["trivial","easy","medium","hard"] },
          "model": { "type": "string", "description": "Optional explicit model/ref override." }
        },
        "required": ["task"],
        "additionalProperties": false
      }
    },
    "workers": { "type": "integer", "description": "Max concurrent implementers (default 3, max 6)." }
  },
  "required": ["subtasks"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Subtasks []GroupSubtaskArg `json:"subtasks"`
				Workers  int               `json:"workers"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if run == nil {
				return "", fmt.Errorf("task_group_mutating is not available")
			}
			return run(ctx, in.Subtasks, in.Workers)
		},
	}
}
