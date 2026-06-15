package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// BashOutput returns the bash_output tool: poll a backgrounded shell's new
// output + status. Incremental — repeated calls return only what's new since
// the last poll, so the agent can watch a server's log without re-reading it.
func BashOutput(shells *ShellRegistry) Definition {
	return Definition{
		Name:        "bash_output",
		Description: "Read new output from a backgrounded shell (started with bash background=true, or a command you backgrounded). Returns output appended since your last poll plus its status (running/exited/killed). Use the shell id from bash.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string", "description": "Shell id, e.g. shell-1." },
    "full": { "type": "boolean", "description": "Return the entire retained buffer instead of only new output." }
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				ID   string `json:"id"`
				Full bool   `json:"full"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			sh := shells.Get(strings.TrimSpace(in.ID))
			if sh == nil {
				return "", fmt.Errorf("no such shell %q (list with the shells panel or bash background=true)", in.ID)
			}
			var out, status string
			var code int
			if in.Full {
				out, status, code = sh.snapshot()
			} else {
				out, status, code = sh.readNew()
			}
			head := fmt.Sprintf("[%s %s", sh.ID, status)
			if status != "running" {
				head += fmt.Sprintf(" exit=%d", code)
			}
			head += "]"
			if strings.TrimSpace(out) == "" {
				if status == "running" {
					return head + " (no new output)", nil
				}
				return head + " (no output)", nil
			}
			return head + "\n" + out, nil
		},
	}
}

// KillShell returns the kill_shell tool: stop a backgrounded shell (its whole
// process group). Mutating — gated in gated mode.
func KillShell(shells *ShellRegistry) Definition {
	return Definition{
		Name:        "kill_shell",
		Description: "Stop a backgrounded shell (and the whole process tree it started). Use the shell id from bash.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": { "type": "string", "description": "Shell id, e.g. shell-1." }
  },
  "required": ["id"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			sh := shells.Get(strings.TrimSpace(in.ID))
			if sh == nil {
				return "", fmt.Errorf("no such shell %q", in.ID)
			}
			if !sh.running() {
				_, status, code := sh.snapshot()
				return fmt.Sprintf("%s already %s (exit=%d)", sh.ID, status, code), nil
			}
			sh.kill()
			return fmt.Sprintf("killing %s (%s)", sh.ID, truncShellCmd(sh.Command)), nil
		},
	}
}
