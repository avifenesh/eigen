package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Grep returns the content-search tool, powered by ripgrep.
func Grep(policy *Policy) Definition {
	return Definition{
		Name:        "grep",
		Description: "Search file contents for a regular expression, powered by ripgrep. Returns file:line:match.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": { "type": "string", "description": "Regular expression to search for." },
    "path": { "type": "string", "description": "File or directory to search (default: current directory)." }
  },
  "required": ["pattern"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Pattern == "" {
				return "", fmt.Errorf("pattern is required")
			}
			if in.Path == "" {
				in.Path = "."
			}
			resolved, err := policy.Resolve(in.Path)
			if err != nil {
				return "", err
			}
			out, code, err := runRipgrep(ctx, "--line-number", "--no-heading", "--color", "never", "--", in.Pattern, resolved)
			if err != nil {
				return "", err
			}
			if code == 1 && strings.TrimSpace(out) == "" {
				return "(no matches)", nil
			}
			return out, nil
		},
	}
}
