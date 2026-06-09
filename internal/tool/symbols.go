package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Symbols returns the symbol-finder tool: locate where a function, type, class,
// etc. named X is defined, across languages, powered by ripgrep. Read-only.
func Symbols(policy *Policy) Definition {
	return Definition{
		Name:        "symbols",
		Description: "Find where a symbol (function, type, class, struct, etc.) is defined, across languages. Returns file:line:definition.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": { "type": "string", "description": "Symbol name to find the definition of." },
    "path": { "type": "string", "description": "Directory to search (default: current directory)." }
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Name string `json:"name"`
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Name == "" {
				return "", fmt.Errorf("name is required")
			}
			if in.Path == "" {
				in.Path = "."
			}
			resolved, err := policy.Resolve(in.Path)
			if err != nil {
				return "", err
			}
			name := regexp.QuoteMeta(in.Name)
			// A line that both contains a definition keyword and the symbol, or
			// a JS/TS-style `name = function|(=>|class` binding.
			pattern := `(\b(func|type|def|class|fn|struct|enum|trait|interface|impl|module|package)\b.*\b` + name + `\b)` +
				`|(\b` + name + `\b\s*[:=]\s*(function|async|\(|class))`
			rgArgs := []string{"--line-number", "--no-heading", "--color", "never"}
			rgArgs = append(rgArgs, DenyGlobs()...)
			rgArgs = append(rgArgs, "--", pattern, resolved)
			out, code, err := runRipgrep(ctx, rgArgs...)
			if err != nil {
				return "", err
			}
			out = FilterDeniedLines(out, func(line string) string {
				if i := strings.IndexByte(line, ':'); i >= 0 {
					return line[:i]
				}
				return line
			})
			if code == 1 && strings.TrimSpace(out) == "" {
				return "(no definitions found for " + in.Name + ")", nil
			}
			return out, nil
		},
	}
}
