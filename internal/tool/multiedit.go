package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// MultiEdit returns the multi-edit tool: apply a sequence of string
// replacements to a single file atomically. Edits are applied in order, each
// against the result of the previous one, and the file is written once at the
// end — so either all edits land or none do.
func MultiEdit(policy *Policy) Definition {
	return Definition{
		Name:        "multiedit",
		Description: "Apply multiple ordered string replacements to one file atomically. Each edit's old_string must match (and be unique unless replace_all). All edits succeed or none are written.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "Path of the file to edit." },
    "edits": {
      "type": "array",
      "description": "Ordered list of replacements to apply.",
      "items": {
        "type": "object",
        "properties": {
          "old_string": { "type": "string", "description": "Exact text to replace." },
          "new_string": { "type": "string", "description": "Replacement text." },
          "replace_all": { "type": "boolean", "description": "Replace every occurrence (default false)." }
        },
        "required": ["old_string", "new_string"],
        "additionalProperties": false
      }
    }
  },
  "required": ["path", "edits"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path  string `json:"path"`
				Edits []struct {
					OldString  string `json:"old_string"`
					NewString  string `json:"new_string"`
					ReplaceAll bool   `json:"replace_all"`
				} `json:"edits"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			if len(in.Edits) == 0 {
				return "", fmt.Errorf("edits is required and must be non-empty")
			}
			resolved, err := policy.Resolve(in.Path)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return "", err
			}
			content := string(data)
			total := 0
			for i, e := range in.Edits {
				if e.OldString == "" {
					return "", fmt.Errorf("edit %d: old_string is required", i+1)
				}
				n := strings.Count(content, e.OldString)
				if n == 0 {
					return "", fmt.Errorf("edit %d: old_string not found in %s", i+1, in.Path)
				}
				if n > 1 && !e.ReplaceAll {
					return "", fmt.Errorf("edit %d: old_string is not unique in %s (%d matches); add context or set replace_all", i+1, in.Path, n)
				}
				if e.ReplaceAll {
					content = strings.ReplaceAll(content, e.OldString, e.NewString)
					total += n
				} else {
					content = strings.Replace(content, e.OldString, e.NewString, 1)
					total++
				}
			}
			if err := atomicWrite(resolved, []byte(content)); err != nil {
				return "", err
			}
			return fmt.Sprintf("applied %d edits (%d replacement(s)) to %s", len(in.Edits), total, in.Path), nil
		},
	}
}
