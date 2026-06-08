package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const (
	defaultBashTimeout = 30 * time.Second
	maxBashTimeout     = 10 * time.Minute
)

// Bash returns the command-execution tool. It is mutating (Exec): in gated mode
// it requires approval. The path fence does not constrain arbitrary commands —
// that is what the approval gate is for.
func Bash() Definition {
	return Definition{
		Name:        "bash",
		Description: "Run a shell command with bash -c and return its combined stdout+stderr. Mutating: requires approval in gated mode.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": { "type": "string", "description": "Shell command to run." },
    "timeout_seconds": { "type": "integer", "description": "Max seconds to run (default 30, max 600)." }
  },
  "required": ["command"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Command        string `json:"command"`
				TimeoutSeconds int    `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Command == "" {
				return "", fmt.Errorf("command is required")
			}
			timeout := defaultBashTimeout
			if in.TimeoutSeconds > 0 {
				timeout = time.Duration(in.TimeoutSeconds) * time.Second
				if timeout > maxBashTimeout {
					timeout = maxBashTimeout
				}
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			out, err := exec.CommandContext(ctx, "bash", "-c", in.Command).CombinedOutput()
			result := string(out)
			if ctx.Err() == context.DeadlineExceeded {
				return result + fmt.Sprintf("\n[timed out after %s]", timeout), nil
			}
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return result + fmt.Sprintf("\n[exit status %d]", ee.ExitCode()), nil
			}
			if err != nil {
				return "", err
			}
			return result, nil
		},
	}
}
