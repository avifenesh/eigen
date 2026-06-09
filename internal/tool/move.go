package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Move returns the file move/rename tool. Both paths are confined by the policy.
// Mutating: requires approval in gated mode.
func Move(policy *Policy) Definition {
	return Definition{
		Name:        "move",
		Description: "Move or rename a file or directory. Both source and destination must be within the allowed roots.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "from": { "type": "string", "description": "Existing path to move." },
    "to": { "type": "string", "description": "Destination path." }
  },
  "required": ["from", "to"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.From == "" || in.To == "" {
				return "", fmt.Errorf("both from and to are required")
			}
			from, err := policy.Resolve(in.From)
			if err != nil {
				return "", err
			}
			to, err := policy.Resolve(in.To)
			if err != nil {
				return "", err
			}
			if _, err := os.Stat(from); err != nil {
				return "", fmt.Errorf("source does not exist: %s", in.From)
			}
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return "", err
			}
			if err := os.Rename(from, to); err != nil {
				return "", err
			}
			return fmt.Sprintf("moved %s -> %s", in.From, in.To), nil
		},
	}
}
