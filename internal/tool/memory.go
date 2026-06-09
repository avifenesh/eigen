package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// MemoryStore is the minimal view of a project memory the memory tool needs.
// (Satisfied by *memory.Store; an interface here avoids an import cycle.)
type MemoryStore interface {
	Append(note string) error
}

// Memory returns the memory tool. The agent records a durable note for future
// sessions, choosing the scope: "project" (default — facts about THIS repo:
// build/test commands, architecture, gotchas) or "global" (cross-project facts:
// the user's working style, durable preferences, and rules that apply
// everywhere). It writes only to eigen's own memory store (not the user's
// project), so it is read-only with respect to the project and auto-runs.
// global may be nil when no global store is available (then any scope writes to
// project).
func Memory(project, global MemoryStore) Definition {
	return Definition{
		Name: "memory",
		Description: "Record a durable note for future sessions. scope=\"project\" (default) for facts about THIS repo (build/test commands, conventions, architecture, gotchas); scope=\"global\" for cross-project facts that apply everywhere (the user's working style, durable preferences, global rules). Use sparingly, for facts worth remembering long-term.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "note": { "type": "string", "description": "A concise fact worth remembering across sessions." },
    "scope": { "type": "string", "enum": ["project", "global"], "description": "Where to store it: \"project\" (this repo, default) or \"global\" (applies to every project)." }
  },
  "required": ["note"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Note  string `json:"note"`
				Scope string `json:"scope"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			store := project
			where := "project"
			if in.Scope == "global" && global != nil {
				store = global
				where = "global"
			}
			if store == nil {
				return "", fmt.Errorf("no memory store available")
			}
			if err := store.Append(in.Note); err != nil {
				return "", err
			}
			return "noted for future sessions (" + where + " memory)", nil
		},
	}
}
