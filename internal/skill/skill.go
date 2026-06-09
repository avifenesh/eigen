// Package skill discovers and loads SKILL.md skills — markdown files with a
// YAML-ish frontmatter (name, description) and an instruction body. The agent
// is told which skills exist (a catalog injected into the system prompt) and
// loads a skill's full body on demand via the skill tool.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill is one discovered skill (frontmatter only; the body is read on demand).
type Skill struct {
	Name        string
	Description string
	Path        string // path to the SKILL.md file
}

// Set is an ordered, name-keyed collection of discovered skills.
type Set struct {
	order  []string
	byName map[string]Skill
}

// Discover scans each directory for `*/SKILL.md` files, parsing the frontmatter
// of each. Later directories do not override earlier ones (first wins on name).
// Missing directories are skipped silently.
func Discover(dirs ...string) *Set {
	s := &Set{byName: map[string]Skill{}}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
		sort.Strings(matches)
		for _, path := range matches {
			sk, err := parse(path)
			if err != nil || sk.Name == "" {
				continue
			}
			if _, dup := s.byName[sk.Name]; dup {
				continue
			}
			s.order = append(s.order, sk.Name)
			s.byName[sk.Name] = sk
		}
	}
	return s
}

// List returns the skills in discovery order.
func (s *Set) List() []Skill {
	out := make([]Skill, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.byName[n])
	}
	return out
}

// Len reports how many skills were discovered.
func (s *Set) Len() int { return len(s.order) }

// Get returns a skill by name.
func (s *Set) Get(name string) (Skill, bool) {
	sk, ok := s.byName[name]
	return sk, ok
}

// Names returns the discovered skill names in order.
func (s *Set) Names() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Body returns the instruction body of a skill (everything after the
// frontmatter), read from disk on demand.
func (s *Set) Body(name string) (string, error) {
	sk, ok := s.byName[name]
	if !ok {
		return "", fmt.Errorf("unknown skill %q (available: %s)", name, strings.Join(s.order, ", "))
	}
	data, err := os.ReadFile(sk.Path)
	if err != nil {
		return "", err
	}
	return stripFrontmatter(string(data)), nil
}

// Catalog renders the skill list for injection into the system prompt. Empty
// when no skills are present.
func (s *Set) Catalog() string {
	if len(s.order) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available skills — invoke the `skill` tool with the name to load full instructions when a task matches:\n")
	for _, n := range s.order {
		sk := s.byName[n]
		b.WriteString("- " + sk.Name + ": " + firstSentence(sk.Description) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// parse reads a SKILL.md's frontmatter into a Skill. If no name is present the
// parent directory name is used.
func parse(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	name, desc := parseFrontmatter(string(data))
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	return Skill{Name: name, Description: desc, Path: path}, nil
}

// parseFrontmatter extracts name and description from a leading `---` block.
func parseFrontmatter(content string) (name, desc string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			break
		}
		key, val, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	return name, desc
}

// stripFrontmatter returns the body after a leading `---...---` block.
func stripFrontmatter(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return content
}

// firstSentence trims a long description to its first sentence/period for the
// catalog, keeping the system prompt compact.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '.'); i >= 0 && i < 200 {
		return s[:i+1]
	}
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// Save writes a new skill as dir/<name>/SKILL.md with the given frontmatter and
// body. It refuses to overwrite an existing skill (returns an error), so a
// synthesized skill never clobbers a hand-written one. Returns the file path.
func Save(dir, name, description, body string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	// Keep the name filesystem-safe (it is also the directory).
	if strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("invalid skill name %q", name)
	}
	sd := filepath.Join(dir, name)
	path := filepath.Join(sd, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("skill %q already exists at %s", name, path)
	}
	if err := os.MkdirAll(sd, 0o755); err != nil {
		return "", err
	}
	desc := strings.ReplaceAll(strings.TrimSpace(description), "\n", " ")
	content := fmt.Sprintf("---\nname: %s\ndescription: \"%s\"\n---\n\n%s\n", name, desc, strings.TrimSpace(body))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
