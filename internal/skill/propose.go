package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill proposals: dreaming synthesizes a candidate skill but NEVER auto-installs
// it (the agent can't grow its own active skills unsupervised). A proposal is
// staged under ~/.eigen/skills-proposed/<name>/SKILL.md; the user reviews it and
// accepts (`eigen skill accept <name>`) to move it into the active skills dir,
// or rejects it.

// ProposedDir is ~/.eigen/skills-proposed (override base via the home dir).
func ProposedDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "skills-proposed")
}

// activeSkillsDir is ~/.eigen/skills (the per-user active skills).
func activeSkillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "skills")
}

func safeName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid skill name %q", name)
	}
	return nil
}

// Propose stages a synthesized skill under ProposedDir for the user to review.
// Preserves the FIRST proposal of a given name: the dream loop runs repeatedly,
// and a still-unreviewed proposal must not be silently clobbered by a later
// pass. Returns ("", nil) — a no-op — when a proposal already exists (whether
// the body is identical or refined) or when a skill of that name is already
// ACTIVE. Only a brand-new name is written, returning its path.
func Propose(name, description, body string) (string, error) {
	if err := safeName(name); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(activeSkillsDir(), name, "SKILL.md")); err == nil {
		return "", nil // already an active skill
	}
	sd := filepath.Join(ProposedDir(), name)
	path := filepath.Join(sd, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		return "", nil // a proposal is already pending review — don't overwrite it
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

// Proposal is a staged skill proposal.
type Proposal struct {
	Name        string
	Description string
	Path        string
}

// Proposals lists the staged skill proposals.
func Proposals() []Proposal {
	entries, err := os.ReadDir(ProposedDir())
	if err != nil {
		return nil
	}
	var out []Proposal
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(ProposedDir(), e.Name(), "SKILL.md")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, Proposal{Name: e.Name(), Description: frontmatterDesc(string(data)), Path: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Accept moves a proposal into the active skills dir, making it loadable.
// Returns the installed path. Fails if no such proposal, or a skill of that
// name is already active.
func Accept(name string) (string, error) {
	if err := safeName(name); err != nil {
		return "", err
	}
	src := filepath.Join(ProposedDir(), name)
	if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
		return "", fmt.Errorf("no proposed skill %q", name)
	}
	dst := filepath.Join(activeSkillsDir(), name)
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err == nil {
		return "", fmt.Errorf("skill %q is already active", name)
	}
	if err := os.MkdirAll(activeSkillsDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(src, dst); err != nil {
		// Cross-device or other rename failure: copy the SKILL.md then drop src.
		data, rerr := os.ReadFile(filepath.Join(src, "SKILL.md"))
		if rerr != nil {
			return "", err
		}
		if werr := os.MkdirAll(dst, 0o755); werr != nil {
			return "", werr
		}
		if werr := os.WriteFile(filepath.Join(dst, "SKILL.md"), data, 0o644); werr != nil {
			return "", werr
		}
		_ = os.RemoveAll(src)
	}
	return filepath.Join(dst, "SKILL.md"), nil
}

// Reject deletes a staged proposal.
func Reject(name string) error {
	if err := safeName(name); err != nil {
		return err
	}
	src := filepath.Join(ProposedDir(), name)
	if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
		return fmt.Errorf("no proposed skill %q", name)
	}
	return os.RemoveAll(src)
}

// frontmatterDesc pulls the description from a SKILL.md frontmatter (best-effort).
func frontmatterDesc(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "description:") {
			d := strings.TrimSpace(strings.TrimPrefix(ln, "description:"))
			return strings.Trim(d, "\"")
		}
	}
	return ""
}
