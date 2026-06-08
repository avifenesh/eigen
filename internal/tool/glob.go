package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Glob returns the file-finding tool, powered by ripgrep (gitignore-aware).
func Glob(policy *Policy) Definition {
	return Definition{
		Name:        "glob",
		Description: "Find files matching a glob pattern (e.g. **/*.go), powered by ripgrep; respects .gitignore.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": { "type": "string", "description": "Glob pattern, e.g. **/*.go" },
    "path": { "type": "string", "description": "Directory to search (default: current directory)." }
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
			rgArgs := []string{"--files", "-g", in.Pattern}
			rgArgs = append(rgArgs, DenyGlobs()...)
			rgArgs = append(rgArgs, "--", resolved)
			out, _, err := runRipgrep(ctx, rgArgs...)
			if err != nil {
				return "", err
			}
			out = FilterDeniedLines(out, func(line string) string { return line })
			if strings.TrimSpace(out) == "" {
				return "(no files matched)", nil
			}
			return out, nil
		},
	}
}
