package tui

// Read-only git tab for the right panel (Tier 11). This first cut is cheap and
// local: branch, ahead/behind, staged/unstaged/untracked counts, and diff stat.
// It never fetches and never mutates the repo.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const gitPanelTimeout = 2 * time.Second

type gitSummary struct {
	Dir       string
	Repo      bool
	Branch    string
	Ahead     int
	Behind    int
	Staged    int
	Unstaged  int
	Untracked int
	DiffStat  string
	Err       string
}

func (m *model) gitLines(h int) []string {
	s := gitSummaryFor(m.sessionDir())
	pw := m.rightCols()
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(pw-2), pw))
	contentW := pw - 4
	add := func(s string) {
		if len(lines) >= h {
			return
		}
		lines = append(lines, changesPad(ansiTrunc(s, contentW), pw))
	}
	if !s.Repo {
		add("not a git repo")
		if s.Dir != "" {
			add(s.Dir)
		}
		for len(lines) < h {
			lines = append(lines, changesPad("", pw))
		}
		return lines
	}
	add("branch  " + s.Branch)
	if s.Ahead > 0 || s.Behind > 0 {
		add(fmt.Sprintf("sync    ↑%d ↓%d", s.Ahead, s.Behind))
	} else {
		add("sync    up to date")
	}
	add(fmt.Sprintf("files   staged %d", s.Staged))
	add(fmt.Sprintf("        changed %d", s.Unstaged))
	add(fmt.Sprintf("        untracked %d", s.Untracked))
	if strings.TrimSpace(s.DiffStat) != "" {
		add("diffstat")
		for _, ln := range strings.Split(strings.TrimSpace(s.DiffStat), "\n") {
			add("  " + strings.TrimSpace(ln))
		}
	} else {
		add("diff    clean")
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", pw))
	}
	return lines
}

func gitSummaryFor(dir string) gitSummary {
	s := gitSummary{Dir: dir}
	if dir == "" {
		s.Err = "no session dir"
		return s
	}
	if out, err := gitPanelIn(dir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return s
	}
	s.Repo = true
	if out, err := gitPanelIn(dir, "branch", "--show-current"); err == nil {
		s.Branch = strings.TrimSpace(out)
	}
	if s.Branch == "" {
		if out, err := gitPanelIn(dir, "rev-parse", "--short", "HEAD"); err == nil {
			s.Branch = "detached@" + strings.TrimSpace(out)
		}
	}
	if s.Branch == "" {
		s.Branch = "(unknown)"
	}
	if out, err := gitPanelIn(dir, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		fmt.Sscanf(strings.TrimSpace(out), "%d", &s.Ahead)
	}
	if out, err := gitPanelIn(dir, "rev-list", "--count", "HEAD..@{u}"); err == nil {
		fmt.Sscanf(strings.TrimSpace(out), "%d", &s.Behind)
	}
	if out, err := gitPanelIn(dir, "status", "--porcelain=v1"); err == nil {
		for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if ln == "" {
				continue
			}
			if strings.HasPrefix(ln, "??") {
				s.Untracked++
				continue
			}
			if len(ln) >= 2 {
				if ln[0] != ' ' {
					s.Staged++
				}
				if ln[1] != ' ' {
					s.Unstaged++
				}
			}
		}
	}
	if out, err := gitPanelIn(dir, "diff", "--stat", "--", "."); err == nil {
		s.DiffStat = out
	}
	return s
}

func gitPanelIn(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitPanelTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
