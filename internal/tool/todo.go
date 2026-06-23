package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// validTodoStatus is the allowed lifecycle for a task.
var validTodoStatus = map[string]bool{
	"pending": true, "in_progress": true, "completed": true, "cancelled": true,
}

// validTodoPriority is the allowed (optional) priority band for a task.
var validTodoPriority = map[string]bool{
	"high": true, "medium": true, "low": true,
}

// todoPriorityRank orders todos for rendering: lower rank sorts first. An unset
// or unrecognized priority sorts last (after the explicit high/medium/low band).
func todoPriorityRank(priority string) int {
	switch priority {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

// Todo returns the plan-tracking tool. The model passes the FULL task list on
// every call (an idempotent set, not a delta); a front-end can render it as a
// live checklist. It is read-only (no filesystem side effects) so it auto-runs.
func Todo() Definition {
	return Definition{
		Name:        "todo",
		Description: "Record or update the task plan for the current work. Pass the COMPLETE list every call (it replaces the previous one). Use it to break work into steps and track progress; keep exactly one task in_progress.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "todos": {
      "type": "array",
      "description": "The complete, current task list.",
      "items": {
        "type": "object",
        "properties": {
          "content": { "type": "string", "description": "What the task is." },
          "status": { "type": "string", "enum": ["pending","in_progress","completed","cancelled"] },
          "priority": { "type": "string", "enum": ["high","medium","low"] }
        },
        "required": ["content","status"],
        "additionalProperties": false
      }
    }
  },
  "required": ["todos"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Todos []struct {
					Content  string `json:"content"`
					Status   string `json:"status"`
					Priority string `json:"priority"`
				} `json:"todos"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			inProgress := 0
			done := 0
			for i, t := range in.Todos {
				if t.Content == "" {
					return "", fmt.Errorf("todo %d: content is required", i+1)
				}
				if !validTodoStatus[t.Status] {
					return "", fmt.Errorf("todo %d: invalid status %q", i+1, t.Status)
				}
				if t.Priority != "" && !validTodoPriority[t.Priority] {
					return "", fmt.Errorf("todo %d: invalid priority %q", i+1, t.Priority)
				}
				if t.Status == "in_progress" {
					inProgress++
				}
				if t.Status == "completed" {
					done++
				}
			}
			if inProgress > 1 {
				return "", fmt.Errorf("only one task may be in_progress at a time (%d were)", inProgress)
			}
			// Render highest-priority work first; SliceStable keeps the model's
			// order within a priority band (and across unset priorities).
			order := make([]int, len(in.Todos))
			for i := range order {
				order[i] = i
			}
			sort.SliceStable(order, func(a, b int) bool {
				return todoPriorityRank(in.Todos[order[a]].Priority) < todoPriorityRank(in.Todos[order[b]].Priority)
			})
			var b strings.Builder
			for _, i := range order {
				t := in.Todos[i]
				b.WriteString(todoGlyph(t.Status) + " " + t.Content + todoPriorityTag(t.Priority) + "\n")
			}
			return fmt.Sprintf("plan updated (%d/%d done):\n%s", done, len(in.Todos), strings.TrimRight(b.String(), "\n")), nil
		},
	}
}

// todoPriorityTag renders an optional priority suffix for a checklist line. An
// unset priority yields no tag so the output stays clean when it's omitted.
func todoPriorityTag(priority string) string {
	if priority == "" {
		return ""
	}
	return " (" + priority + ")"
}

// todoGlyph maps a status to a plain-text marker for the tool's text result.
func todoGlyph(status string) string {
	switch status {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	case "cancelled":
		return "[-]"
	default:
		return "[ ]"
	}
}
