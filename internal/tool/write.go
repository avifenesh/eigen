package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// Write returns the file-writing tool (creates or overwrites a file).
func Write(policy *Policy) Definition {
	return Definition{
		Name:        "write",
		Description: "Create or overwrite a file with the given content. Parent directories are created as needed.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "Path of the file to write." },
    "content": { "type": "string", "description": "Full file content to write." }
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			resolved, err := policy.Resolve(in.Path)
			if err != nil {
				return "", err
			}
			if err := atomicWrite(resolved, []byte(in.Content)); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path), nil
		},
	}
}
