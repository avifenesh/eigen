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

// Memory returns the memory tool: the agent records a durable note about the
// project (build commands, conventions, gotchas) that is injected into future
// sessions' system prompt. It writes only to eigen's own memory store (not the
// user's project), so it is read-only with respect to the project and auto-runs.
func Memory(store MemoryStore) Definition {
	return Definition{
		Name:        "memory",
		Description: "Record a durable note about THIS project for future sessions (e.g. build/test commands, conventions, gotchas, architecture). Use sparingly for facts worth remembering long-term.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "note": { "type": "string", "description": "A concise fact worth remembering across sessions." }
  },
  "required": ["note"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Note string `json:"note"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if err := store.Append(in.Note); err != nil {
				return "", err
			}
			return "noted for future sessions", nil
		},
	}
}
