// Package workflow parses and represents authored, replayable multi-step
// processes (Tier 17). A workflow is a markdown file with YAML-ish frontmatter
// (name/description) and one "## <step-id>" section per step. Each section's
// body is the step's prompt; optional directive lines at the TOP of a section
// (key: value, before any prose) configure the step:
//
//	model:       run this step on a specific model/ref (else inherit)
//	check:       a goal-judge condition on the step's output (opt-in)
//	on_failure:  stop (default) | continue | retry
//	retries:     N (with on_failure: retry; default 1)
//
// Prompts may reference {{var.NAME}} placeholders filled from `eigen run --var`.
// This is the skills grain (human-authored prose), with a small hand-rolled
// parser — no new YAML dependency, matching internal/skill.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// OnFailure is what to do when a step's check fails.
type OnFailure string

const (
	FailStop     OnFailure = "stop"     // abort the workflow (nonzero exit)
	FailContinue OnFailure = "continue" // record + move on
	FailRetry    OnFailure = "retry"    // re-run the step up to Retries times
)

// Step is one workflow step.
type Step struct {
	ID        string
	Prompt    string
	Model     string    // optional explicit model/ref
	Check     string    // optional goal-judge condition ("" = no check)
	OnFailure OnFailure // default stop
	Retries   int       // for OnFailure==retry (default 1)
}

// Workflow is a parsed, validated authored process.
type Workflow struct {
	Name        string
	Description string
	Steps       []Step
}

var stepHeader = regexp.MustCompile(`(?m)^##[ \t]+(.+?)[ \t]*$`)
var directiveLine = regexp.MustCompile(`^([a-z_]+):[ \t]*(.*)$`)

// Dir is where workflows live (~/.eigen/workflows).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "workflows")
}

// Load reads and parses a workflow by name from Dir() (with or without .md).
func Load(name string) (*Workflow, error) {
	dir := Dir()
	if dir == "" {
		return nil, fmt.Errorf("no home directory")
	}
	path := filepath.Join(dir, name)
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflow %q: %w", name, err)
	}
	wf, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("workflow %q: %w", name, err)
	}
	if wf.Name == "" {
		wf.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return wf, nil
}

// List returns the names of available workflows (sans .md).
func List() []string {
	dir := Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names
}

// Parse parses a workflow's markdown content.
func Parse(content string) (*Workflow, error) {
	wf := &Workflow{}
	body := content
	// Optional frontmatter (--- name/description ---).
	if name, desc, rest, ok := splitFrontmatter(content); ok {
		wf.Name, wf.Description, body = name, desc, rest
	}
	// Split into "## id" sections.
	locs := stepHeader.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		return nil, fmt.Errorf("no steps (expected one or more \"## <step-id>\" sections)")
	}
	for i, loc := range locs {
		id := strings.TrimSpace(body[loc[2]:loc[3]])
		secStart := loc[1]
		secEnd := len(body)
		if i+1 < len(locs) {
			secEnd = locs[i+1][0]
		}
		step, err := parseStep(id, body[secStart:secEnd])
		if err != nil {
			return nil, err
		}
		wf.Steps = append(wf.Steps, step)
	}
	if err := wf.validate(); err != nil {
		return nil, err
	}
	return wf, nil
}

// parseStep reads a section: optional leading directive lines, then the prompt.
func parseStep(id, section string) (Step, error) {
	s := Step{ID: strings.TrimSpace(id), OnFailure: FailStop, Retries: 1}
	lines := strings.Split(strings.TrimLeft(section, "\n"), "\n")
	body := lines
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" && i == 0 {
			continue
		}
		m := directiveLine.FindStringSubmatch(t)
		if m == nil {
			body = lines[i:]
			break
		}
		key, val := m[1], strings.TrimSpace(m[2])
		val = strings.Trim(val, `"'`)
		switch key {
		case "model":
			s.Model = val
		case "check":
			s.Check = val
		case "on_failure":
			switch OnFailure(val) {
			case FailStop, FailContinue, FailRetry:
				s.OnFailure = OnFailure(val)
			default:
				return s, fmt.Errorf("step %q: on_failure must be stop|continue|retry, got %q", id, val)
			}
		case "retries":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return s, fmt.Errorf("step %q: retries must be a non-negative integer, got %q", id, val)
			}
			s.Retries = n
		default:
			// Unknown key: treat as the start of the prompt body (not a typo
			// trap — a prose line that happens to look like "word: ...").
			body = lines[i:]
			s.Prompt = strings.TrimSpace(strings.Join(body, "\n"))
			return s, nil
		}
	}
	s.Prompt = strings.TrimSpace(strings.Join(body, "\n"))
	return s, nil
}

func (wf *Workflow) validate() error {
	if len(wf.Steps) == 0 {
		return fmt.Errorf("workflow has no steps")
	}
	seen := map[string]bool{}
	for _, s := range wf.Steps {
		if s.ID == "" {
			return fmt.Errorf("a step has an empty id")
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate step id %q", s.ID)
		}
		seen[s.ID] = true
		if strings.TrimSpace(s.Prompt) == "" {
			return fmt.Errorf("step %q has an empty prompt", s.ID)
		}
		if s.OnFailure == FailRetry && s.Retries < 1 {
			s.Retries = 1
		}
	}
	return nil
}

// splitFrontmatter extracts name/description from a leading "--- … ---" block,
// returning the remaining body. ok=false when there's no frontmatter.
func splitFrontmatter(content string) (name, desc, rest string, ok bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", content, false
	}
	for i, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			return name, desc, strings.Join(lines[i+2:], "\n"), true
		}
		key, val, found := strings.Cut(ln, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	return name, desc, content, false // unterminated frontmatter → treat as body
}

var varRe = regexp.MustCompile(`\{\{var\.([a-zA-Z0-9_]+)\}\}`)

// Interpolate replaces {{var.NAME}} placeholders with values from vars. An
// unset variable becomes empty (and is reported) so a missing --var is visible,
// not silently the literal placeholder.
func Interpolate(s string, vars map[string]string) (string, []string) {
	var missing []string
	out := varRe.ReplaceAllStringFunc(s, func(m string) string {
		name := varRe.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		missing = append(missing, name)
		return ""
	})
	return out, missing
}
