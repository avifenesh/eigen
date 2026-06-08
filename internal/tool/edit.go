package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Edit returns the string-replacement edit tool. It requires old_string to be
// present, and unique unless replace_all is set, so an edit can never silently
// hit the wrong location.
func Edit(policy *Policy) Definition {
	return Definition{
		Name:        "edit",
		Description: "Replace old_string with new_string in a file. old_string must match exactly and be unique unless replace_all is true.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "Path of the file to edit." },
    "old_string": { "type": "string", "description": "Exact text to replace." },
    "new_string": { "type": "string", "description": "Replacement text." },
    "replace_all": { "type": "boolean", "description": "Replace every occurrence (default false)." }
  },
  "required": ["path", "old_string", "new_string"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path       string `json:"path"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			if in.OldString == "" {
				return "", fmt.Errorf("old_string is required")
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
			n := strings.Count(content, in.OldString)
			if n == 0 {
				return "", fmt.Errorf("old_string not found in %s", in.Path)
			}
			if n > 1 && !in.ReplaceAll {
				return "", fmt.Errorf("old_string is not unique in %s (%d matches); add surrounding context or set replace_all", in.Path, n)
			}
			var updated string
			if in.ReplaceAll {
				updated = strings.ReplaceAll(content, in.OldString, in.NewString)
			} else {
				updated = strings.Replace(content, in.OldString, in.NewString, 1)
			}
			if err := atomicWrite(resolved, []byte(updated)); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s (%d replacement(s))", in.Path, n), nil
		},
	}
}
