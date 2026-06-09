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
}

// Skill returns the skill-loading tool: the model invokes it with a skill name
// (advertised in the system-prompt catalog) to pull that skill's full
// instructions into the conversation. Read-only, so it auto-runs.
func Skill(set SkillSet) Definition {
	return Definition{
		Name:        "skill",
		Description: "Load a skill's full instructions by name. Available skills are listed in the system prompt; invoke this when the current task matches one.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": { "type": "string", "description": "The skill name to load." }
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
			return body, nil
		},
	}
}
