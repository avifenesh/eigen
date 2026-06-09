package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Diff returns the git-diff tool: show the working-tree diff (optionally for a
// single path). Read-only, so it auto-runs. Useful for the agent to review the
// changes it has made before summarizing.
func Diff(policy *Policy) Definition {
	return Definition{
		Name:        "diff",
		Description: "Show the git diff of the working tree (uncommitted changes). Optionally limit to a path. Read-only.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "Limit the diff to this path (optional)." },
    "staged": { "type": "boolean", "description": "Show staged changes (git diff --staged) instead of unstaged." }
  },
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path   string `json:"path"`
				Staged bool   `json:"staged"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			cmdArgs := []string{"diff"}
			if in.Staged {
				cmdArgs = append(cmdArgs, "--staged")
			}
			if in.Path != "" {
				resolved, err := policy.Resolve(in.Path)
				if err != nil {
					return "", err
				}
				cmdArgs = append(cmdArgs, "--", resolved)
			}
			cmd := exec.CommandContext(ctx, "git", cmdArgs...)
			var out, errb bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errb
			if err := cmd.Run(); err != nil {
				if errb.Len() > 0 {
					return "", fmt.Errorf("git diff: %s", bytes.TrimSpace(errb.Bytes()))
				}
				return "", fmt.Errorf("git diff: %w", err)
			}
			if out.Len() == 0 {
				return "(no changes)", nil
			}
			return out.String(), nil
		},
	}
}
