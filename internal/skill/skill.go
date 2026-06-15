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
	"sync"

	"github.com/avifenesh/eigen/internal/fuzzy"
)

// Skill is one discovered skill (frontmatter only; the body is read on demand).
type Skill struct {
	Name        string
	Description string
	Path        string // path to the SKILL.md file
}

// Set is an ordered, name-keyed collection of discovered skills. It remembers
// the directories it was discovered from so it can Rescan in place when a skill
// is added mid-session (the catalog is otherwise a start-of-session snapshot).
type Set struct {
	mu     sync.RWMutex
	dirs   []string
	order  []string
	byName map[string]Skill
}

// Discover scans each directory for `*/SKILL.md` files, parsing the frontmatter
// of each. Later directories do not override earlier ones (first wins on name).
// Missing directories are skipped silently.
func Discover(dirs ...string) *Set {
	s := &Set{byName: map[string]Skill{}, dirs: append([]string(nil), dirs...)}
	s.scan()
	return s
}

// scan (re)populates order/byName from s.dirs. Caller holds the write lock, or
// constructs before publishing (Discover).
func (s *Set) scan() {
	order := s.order[:0]
	byName := make(map[string]Skill, len(s.byName))
	for _, dir := range s.dirs {
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
			if _, dup := byName[sk.Name]; dup {
				continue
			}
			order = append(order, sk.Name)
			byName[sk.Name] = sk
		}
	}
	s.order = order
	s.byName = byName
}

// Rescan re-reads the source directories so skills added since construction
// (e.g. `eigen skill add` in another window, or a hand-dropped SKILL.md) become
// discoverable without restarting the session. Safe to call concurrently.
func (s *Set) Rescan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scan()
}

// List returns the skills in discovery order.
func (s *Set) List() []Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Skill, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.byName[n])
	}
	return out
}

// Len reports how many skills were discovered.
func (s *Set) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}

// Get returns a skill by name (exact, then via Resolve's hint matching, so a
// near-miss like "skill curator" or "curator" still finds "skill-curator").
func (s *Set) Get(name string) (Skill, bool) {
	resolved, ok := s.Resolve(name)
	if !ok {
		return Skill{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byName[resolved], true
}

// normalizeName lowercases and collapses separators so "Skill Curator",
// "skill_curator", and "skill-curator" all compare equal.
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := func(r rune) rune {
		switch r {
		case ' ', '_', '-', '.', '/':
			return '-'
		}
		return r
	}
	s = strings.Map(repl, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// Resolve maps a loose hint to a registered skill name. Models rarely echo the
// exact registered key, so a hint ("skill curator", "curator", "curate skill")
// must still land. Resolution is a confidence ladder, each tier only accepted
// when it names exactly ONE skill (an ambiguous hint resolves to nothing rather
// than guessing wrong):
//
//  1. exact name
//  2. separator/case-insensitive normalized name ("Skill Curator")
//  3. unique whole-word containment either direction ("curator", "curate a skill")
//  4. unique fuzzy subsequence match (internal/fuzzy), the same ranker the
//     palette/search use
//
// Returns (name, true) only on a unique winner. On a miss it rescans the source
// directories once and retries, so a skill installed mid-session resolves
// without restarting (the catalog is otherwise a start-of-session snapshot).
func (s *Set) Resolve(hint string) (string, bool) {
	s.mu.RLock()
	name, ok := s.resolveLocked(hint)
	s.mu.RUnlock()
	if ok {
		return name, true
	}
	s.Rescan()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveLocked(hint)
}

// resolveLocked is the pure matching ladder; the caller holds at least a read
// lock. It never rescans (Resolve owns that), so it is reused on both the
// pre-rescan and post-rescan attempts.
func (s *Set) resolveLocked(hint string) (string, bool) {
	if hint == "" {
		return "", false
	}
	if _, ok := s.byName[hint]; ok {
		return hint, true
	}
	nh := normalizeName(hint)
	if nh == "" {
		return "", false
	}

	// Tier 2: normalized exact.
	var norm []string
	for _, n := range s.order {
		if normalizeName(n) == nh {
			norm = append(norm, n)
		}
	}
	if len(norm) == 1 {
		return norm[0], true
	}

	// Tier 3: unique whole-word containment, hint-word ↔ name-word, either way.
	hintWords := strings.Split(nh, "-")
	var contain []string
	for _, n := range s.order {
		nn := normalizeName(n)
		nameWords := strings.Split(nn, "-")
		if sharesWord(hintWords, nameWords) && (strings.Contains(nn, nh) || strings.Contains(nh, nn) || overlapAll(hintWords, nameWords)) {
			contain = append(contain, n)
		}
	}
	if len(contain) == 1 {
		return contain[0], true
	}
	// More than one name shares the hint's words: genuinely ambiguous
	// ("curator" with both skill-curator and system-prompt-curator). Fail
	// closed rather than letting the fuzzy tier silently pick one.
	if len(contain) > 1 {
		return "", false
	}

	// Tier 4: unique best fuzzy subsequence match.
	best, bestScore, ties := "", int(^uint(0)>>1), 0
	for _, n := range s.order {
		sc := fuzzy.Score(normalizeName(n), nh)
		if sc < 0 {
			continue
		}
		if sc < bestScore {
			best, bestScore, ties = n, sc, 1
		} else if sc == bestScore {
			ties++
		}
	}
	if best != "" && ties == 1 {
		return best, true
	}
	return "", false
}

// sharesWord reports whether the two word lists share at least one non-trivial
// token (length ≥ 3 so "a"/"to" don't create spurious matches).
func sharesWord(a, b []string) bool {
	for _, x := range a {
		if len(x) < 3 {
			continue
		}
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// overlapAll reports whether every word of the shorter list appears in the
// longer one, so "curate skill" matches "skill-curator"-style multiword names
// even when neither string contains the other verbatim.
func overlapAll(a, b []string) bool {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	set := map[string]bool{}
	for _, w := range long {
		set[w] = true
	}
	for _, w := range short {
		if len(w) < 3 {
			continue // ignore filler tokens
		}
		if !set[w] {
			return false
		}
	}
	return len(short) > 0
}

// Names returns the discovered skill names in order.
func (s *Set) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Body returns the instruction body of a skill (everything after the
// frontmatter), read from disk on demand. The name is resolved through Resolve,
// so a loose hint ("skill curator", "curator") loads the right skill instead of
// erroring on an inexact match.
func (s *Set) Body(name string) (string, error) {
	resolved, ok := s.Resolve(name)
	if !ok {
		s.mu.RLock()
		avail := strings.Join(s.order, ", ")
		s.mu.RUnlock()
		return "", fmt.Errorf("unknown skill %q (available: %s)", name, avail)
	}
	s.mu.RLock()
	sk, ok := s.byName[resolved]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown skill %q", resolved)
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
	s.mu.RLock()
	defer s.mu.RUnlock()
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
