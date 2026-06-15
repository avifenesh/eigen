package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SkillSet is the minimal view of a skill collection the skill tool needs.
// (Defined here to avoid a hard dependency cycle; satisfied by *skill.Set.)
type SkillSet interface {
	Body(name string) (string, error)
	Names() []string
	// Resolve maps a loose hint to the registered skill name it will load,
	// so the tool can tell the model which skill a fuzzy hint resolved to.
	Resolve(hint string) (string, bool)
}

// Skill returns the skill-loading tool: the model invokes it with a skill name
// (advertised in the system-prompt catalog) to pull that skill's full
// instructions into the conversation. Read-only, so it auto-runs.
func Skill(set SkillSet) Definition {
	return Definition{
		Name:        "skill",
		Description: "Load a skill's full instructions by name. Available skills are listed in the system prompt; invoke this when the current task matches one. The name is matched loosely — a close hint (\"skill curator\", \"curator\") resolves to the right skill, so you do not need the exact registered key.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": { "type": "string", "description": "The skill name or a close hint (e.g. \"skill-curator\", \"skill curator\", or \"curator\")." }
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Name == "" {
				return "", fmt.Errorf("name is required (available: %s)", strings.Join(set.Names(), ", "))
			}
			body, err := set.Body(in.Name)
			if err != nil {
				return "", err
			}
			// When the hint wasn't the exact registered name, tell the model
			// which skill actually loaded so a fuzzy resolve is never silent.
			if resolved, ok := set.Resolve(in.Name); ok && resolved != in.Name {
				return fmt.Sprintf("(loaded skill %q for hint %q)\n\n%s", resolved, in.Name, body), nil
			}
			return body, nil
		},
	}
}
