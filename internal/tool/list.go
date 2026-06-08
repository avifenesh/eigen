package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const maxListEntries = 1000

// List returns the directory-listing tool.
func List(policy *Policy) Definition {
	return Definition{
		Name:        "list",
		Description: "List the entries of a directory. Directories are suffixed with '/'.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Directory to list (default: current directory)."
    }
  },
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				in.Path = "."
			}
			resolved, err := policy.Resolve(in.Path)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(resolved)
			if err != nil {
				return "", err
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				names = append(names, name)
			}
			sort.Strings(names)
			truncated := false
			if len(names) > maxListEntries {
				names = names[:maxListEntries]
				truncated = true
			}
			out := strings.Join(names, "\n")
			if truncated {
				out += "\n[truncated]"
			}
			if out == "" {
				return "(empty directory)", nil
			}
			return out, nil
		},
	}
}
