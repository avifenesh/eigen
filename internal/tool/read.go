package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

const maxReadBytes = 256 * 1024

// Read returns the read-file tool: a read-only tool that returns a file's
// UTF-8 contents, truncated to a safe size.
func Read() Definition {
	return Definition{
		Name:        "read",
		Description: "Read the contents of a UTF-8 text file at the given path.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Path to the file to read (absolute, or relative to the working directory)."
    }
  },
  "required": ["path"],
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
				return "", fmt.Errorf("path is required")
			}
			data, err := os.ReadFile(in.Path)
			if err != nil {
				return "", err
			}
			if len(data) > maxReadBytes {
				return string(data[:maxReadBytes]) + "\n[truncated]", nil
			}
			return string(data), nil
		},
	}
}
