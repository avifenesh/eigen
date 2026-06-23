package gui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/skill"
)

// Skills bridge layer. Skills are SKILL.md files on the local filesystem
// (~/.eigen/skills, the project's .eigen/skills, and any EIGEN_SKILLS_DIRS), so
// the GUI process reads them directly. Proposals are dream-synthesized drafts
// awaiting accept/reject.

// SkillDTO is one discovered skill for the gallery.
type SkillDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Source      string `json:"source"` // "user" | "project" | "extra"
}

// SkillProposalDTO is a dream-proposed skill awaiting the user's accept/reject.
type SkillProposalDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

// SkillsDTO is the full skills snapshot for the gallery.
type SkillsDTO struct {
	Skills    []SkillDTO         `json:"skills"`
	Proposals []SkillProposalDTO `json:"proposals"`
}

// skillDirs mirrors main.skillDirs: per-user store, project store, and any
// colon-separated EIGEN_SKILLS_DIRS.
func skillDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".eigen", "skills"),
		filepath.Join(".eigen", "skills"),
	}
	if extra := os.Getenv("EIGEN_SKILLS_DIRS"); extra != "" {
		for _, d := range strings.Split(extra, ":") {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// sourceOf classifies a skill path into user/project/extra by its directory.
// Discover yields paths as-joined from skillDirs (the project dir is RELATIVE:
// ".eigen/skills"), so resolve the candidate to absolute before comparing —
// otherwise a relative project path never matches the absolute project dir and
// every project skill falls through to "extra".
func sourceOf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	home, _ := os.UserHomeDir()
	userDir := filepath.Join(home, ".eigen", "skills")
	if strings.HasPrefix(abs, userDir) {
		return "user"
	}
	if projDir, err := filepath.Abs(filepath.Join(".eigen", "skills")); err == nil && strings.HasPrefix(abs, projDir) {
		return "project"
	}
	return "extra"
}

// Skills returns discovered skills + dream proposals.
func (b *Bridge) Skills() (*SkillsDTO, error) {
	set := skill.Discover(skillDirs()...)
	list := set.List()
	skills := make([]SkillDTO, 0, len(list))
	for _, s := range list {
		skills = append(skills, SkillDTO{
			Name:        s.Name,
			Description: s.Description,
			Path:        s.Path,
			Source:      sourceOf(s.Path),
		})
	}
	props := skill.Proposals()
	proposals := make([]SkillProposalDTO, 0, len(props))
	for _, p := range props {
		proposals = append(proposals, SkillProposalDTO{Name: p.Name, Description: p.Description, Path: p.Path})
	}
	return &SkillsDTO{Skills: skills, Proposals: proposals}, nil
}

// SkillBody returns a skill's Markdown body (frontmatter stripped) for preview.
func (b *Bridge) SkillBody(name string) (string, error) {
	set := skill.Discover(skillDirs()...)
	return set.Body(name)
}

// AcceptSkill promotes a dream-proposed skill to active; returns its new path.
func (b *Bridge) AcceptSkill(name string) (string, error) {
	return skill.Accept(name)
}

// RejectSkill discards a dream-proposed skill.
func (b *Bridge) RejectSkill(name string) error {
	return skill.Reject(name)
}
