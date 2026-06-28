package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
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

// RemoveSkill deletes an installed skill from ~/.eigen/skills by name.
func (b *Bridge) RemoveSkill(name string) error {
	return skill.Remove(userSkillsDir(), name)
}

// SkillInstallDTO is the result of an install: the resolved skill name and the
// path its SKILL.md was written to.
type SkillInstallDTO struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// userSkillsDir is the per-user skills store (~/.eigen/skills) — installs land
// here, the same target the CLI uses for `eigen skill add`.
func userSkillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "skills")
}

// installScanner builds the security scanner for skill installs using a small/
// cheap model (EIGEN_SMALL_MODEL → grok composer → Haiku), mirroring main's
// smallProvider precedence. It vets the SKILL.md content before it is written;
// a RISKY verdict aborts the install (the bridge never Forces). Returns nil
// when no provider can be credentialed, in which case the caller fails closed.
func installScanner() skill.Scanner {
	if sm := os.Getenv("EIGEN_SMALL_MODEL"); sm != "" {
		if p, err := llm.New("", sm); err == nil {
			return skill.ProviderScanner{P: p}
		}
	}
	if llm.ProviderAvailable("grok") {
		if p, err := llm.New("grok", "grok-composer-2.5-fast"); err == nil {
			return skill.ProviderScanner{P: p}
		}
	}
	if p, err := llm.New("converse", "us.anthropic.claude-haiku-4-5-20251001-v1:0"); err == nil {
		return skill.ProviderScanner{P: p}
	}
	return nil
}

// installOptions builds the InstallOptions shared by both install paths: write
// into the per-user store, scan on, never Force. A nil scanner (no credentialed
// small model) is surfaced as an error rather than silently installing unvetted
// content.
func (b *Bridge) installOptions() (skill.InstallOptions, error) {
	sc := installScanner()
	if sc == nil {
		return skill.InstallOptions{}, errors.New("cannot scan skill: no credentialed model available (set EIGEN_SMALL_MODEL or credential a provider)")
	}
	return skill.InstallOptions{Dir: userSkillsDir(), Scanner: sc}, nil
}

// InstallSkillFromPath installs a skill from a local SKILL.md file or a
// directory containing one. The content is security-scanned before it is
// written; a RISKY verdict aborts the install (a *skill.RiskyError is returned,
// flattened to its message by Wails). Returns the resolved name + install path.
func (b *Bridge) InstallSkillFromPath(path string) (*SkillInstallDTO, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("skill path is empty")
	}
	opts, err := b.installOptions()
	if err != nil {
		return nil, err
	}
	res, err := skill.InstallFromPath(context.Background(), path, opts)
	if err != nil {
		return nil, err
	}
	return &SkillInstallDTO{Name: res.Name, Path: res.Path}, nil
}

// InstallSkillFromGitHub installs a skill from a GitHub reference of the form
// owner/repo[/subpath][@ref] (a leading github.com/ or https:// is tolerated).
// The fetched SKILL.md is security-scanned before it is written; a RISKY
// verdict aborts the install. Returns the resolved name + install path.
func (b *Bridge) InstallSkillFromGitHub(ownerRepo string) (*SkillInstallDTO, error) {
	ref, err := skill.ParseGitHubRef(ownerRepo)
	if err != nil {
		return nil, err
	}
	opts, err := b.installOptions()
	if err != nil {
		return nil, err
	}
	res, err := skill.InstallFromGitHub(context.Background(), ref, skill.DefaultFetcher, opts)
	if err != nil {
		return nil, err
	}
	return &SkillInstallDTO{Name: res.Name, Path: res.Path}, nil
}
