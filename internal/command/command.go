// Package command implements custom slash commands (Tier 31): user- or
// plugin-authored prompts saved as markdown, surfaced as `/<name>` in the TUI.
// It reads the SAME format as Claude Code's slash commands — a markdown file
// with optional YAML-ish frontmatter (description / argument-hint / allowed-tools
// / model) and a body that becomes the prompt, with $ARGUMENTS and $1..$9
// substitution — so a plugin's commands/*.md files run unchanged.
//
// A command is "run" by expanding its body with the user's arguments and
// submitting that as a normal user turn (the model then does the work with the
// regular toolset). This composes with the live session, approvals, and
// steering, exactly like /workflow.
package command

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Command is one custom slash command parsed from a markdown file.
type Command struct {
	Name         string   // slash name (file basename sans .md), e.g. "review"
	Description  string   // frontmatter description (shown in the menu)
	ArgHint      string   // frontmatter argument-hint (shown after the name)
	Model        string   // frontmatter model: run this command on a specific model
	AllowedTools []string // frontmatter allowed-tools: restrict the turn to these tools
	Body         string   // the prompt template (frontmatter stripped)
	Path         string   // source file
	Scope        string   // "project" or "user"
}

// Dirs returns the command directories in precedence order: project first
// (./.eigen/commands), then user (~/.eigen/commands). A project command shadows
// a user one of the same name.
func Dirs() []string {
	var dirs []string
	dirs = append(dirs, filepath.Join(".eigen", "commands"))
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".eigen", "commands"))
	}
	return dirs
}

// UserDir is where plugin-installed and hand-authored commands live globally.
func UserDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "commands")
}

// Set is the loaded set of custom commands, keyed by name (first scope wins).
type Set struct {
	byName map[string]Command
	order  []string
}

// Load discovers commands from the given dirs (precedence: earlier wins). A
// missing dir is skipped. Names are the file basename without .md.
func Load(dirs ...string) *Set {
	s := &Set{byName: map[string]Command{}}
	for i, dir := range dirs {
		if dir == "" {
			continue
		}
		scope := "user"
		if i == 0 {
			scope = "project"
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		sort.Strings(matches)
		for _, path := range matches {
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			if name == "" || s.byName[name].Name != "" {
				continue // earlier scope wins
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			c := parse(name, string(b))
			c.Path = path
			c.Scope = scope
			s.byName[name] = c
			s.order = append(s.order, name)
		}
	}
	sort.Strings(s.order)
	return s
}

// Get returns a command by name.
func (s *Set) Get(name string) (Command, bool) {
	c, ok := s.byName[strings.TrimPrefix(name, "/")]
	return c, ok
}

// Names returns the command names in sorted order.
func (s *Set) Names() []string { return append([]string(nil), s.order...) }

// All returns the commands in sorted order.
func (s *Set) All() []Command {
	out := make([]Command, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.byName[n])
	}
	return out
}

// Len reports how many commands are loaded.
func (s *Set) Len() int { return len(s.order) }

var fmKey = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*):[ \t]*(.*)$`)

// parse splits optional leading "--- … ---" frontmatter and returns the command.
// description, argument-hint, model, and allowed-tools are read; other keys
// (codex-description, …) are tolerated and ignored.
func parse(name, content string) Command {
	c := Command{Name: name}
	body := content
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end > 0 {
			for _, ln := range lines[1:end] {
				m := fmKey.FindStringSubmatch(strings.TrimSpace(ln))
				if m == nil {
					continue
				}
				key, val := strings.ToLower(m[1]), strings.TrimSpace(m[2])
				val = strings.Trim(val, `"'`)
				switch key {
				case "description":
					c.Description = val
				case "argument-hint", "argument_hint", "arg-hint":
					c.ArgHint = val
				case "model":
					c.Model = val
				case "allowed-tools", "allowed_tools", "allowedtools":
					c.AllowedTools = splitToolList(val)
				}
			}
			body = strings.Join(lines[end+1:], "\n")
		}
	}
	c.Body = strings.TrimSpace(body)
	if c.Description == "" {
		c.Description = "custom command"
	}
	return c
}

// splitToolList parses a frontmatter allowed-tools value: a comma-separated
// list of tool names (Claude form, e.g. "Read, Write, Bash(git:*)"). Whitespace
// is trimmed and empty entries dropped; argument scopes like "(git:*)" are kept
// verbatim here and normalized at enforcement time.
func splitToolList(val string) []string {
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// argTokens splits an argument string into whitespace-separated tokens, honoring
// simple double-quoted groups so `"a b"` is one token.
func argTokens(args string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range args {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

var posArg = regexp.MustCompile(`\$([1-9])`)

// Expand fills a command body with the user's argument string: $ARGUMENTS →
// the whole string, $1..$9 → positional tokens (empty when absent). Matches
// Claude's command substitution so plugin commands behave identically.
func Expand(body, args string) string {
	args = strings.TrimSpace(args)
	out := strings.ReplaceAll(body, "$ARGUMENTS", args)
	if strings.Contains(out, "$") {
		toks := argTokens(args)
		out = posArg.ReplaceAllStringFunc(out, func(m string) string {
			n, _ := strconv.Atoi(m[1:])
			if n >= 1 && n <= len(toks) {
				return toks[n-1]
			}
			return ""
		})
	}
	// A command with NO $ARGUMENTS placeholder but given args: append them so a
	// bare `/cmd extra context` still reaches the model (Claude appends too).
	if args != "" && !strings.Contains(body, "$ARGUMENTS") && !posArg.MatchString(body) {
		out = strings.TrimRight(out, "\n") + "\n\n" + args
	}
	return out
}
