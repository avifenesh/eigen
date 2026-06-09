package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Installed reports the result of installing a skill.
type Installed struct {
	Name string
	Path string // where the SKILL.md was written
	Scan ScanResult
}

// InstallOptions configures an install.
type InstallOptions struct {
	// Dir is the skills directory to install into (e.g. ~/.eigen/skills).
	Dir string
	// Name overrides the skill name (default: the source's name).
	Name string
	// Scanner, if set, vets the skill content before writing. A RISKY verdict
	// aborts the install unless Force is set.
	Scanner Scanner
	// Force installs even when the scan flags the skill as risky.
	Force bool
	// Overwrite replaces an existing skill of the same name.
	Overwrite bool
}

// InstallFromPath installs a skill from a local path: either a SKILL.md file or
// a directory containing one. The content is scanned (if a Scanner is set)
// before being written into opts.Dir.
func InstallFromPath(ctx context.Context, src string, opts InstallOptions) (Installed, error) {
	content, srcName, err := readSkillFromPath(src)
	if err != nil {
		return Installed{}, err
	}
	return finishInstall(ctx, content, srcName, opts)
}

// readSkillFromPath loads SKILL.md content from a file or directory, returning
// the content and a fallback name (the parent/dir basename).
func readSkillFromPath(src string) (content, name string, err error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", "", err
	}
	file := src
	if info.IsDir() {
		file = filepath.Join(src, "SKILL.md")
		name = filepath.Base(filepath.Clean(src))
	} else {
		name = filepath.Base(filepath.Dir(src))
		if name == "." || name == "" || name == string(filepath.Separator) {
			name = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", file, err)
	}
	return string(data), name, nil
}

// Fetcher fetches the bytes at a URL (injectable so GitHub installs are
// testable without network).
type Fetcher func(ctx context.Context, url string) ([]byte, error)

// GitHubRef identifies a skill on GitHub: owner/repo, an optional path within
// the repo to the skill directory or SKILL.md, and an optional @ref (branch,
// tag, or commit; default the repo's default branch via "HEAD").
type GitHubRef struct {
	Owner string
	Repo  string
	Path  string // "" → repo root (expects SKILL.md there)
	Ref   string // "" → HEAD
}

// ParseGitHubRef parses "owner/repo", "owner/repo/sub/dir", and an optional
// trailing "@ref". A leading "github.com/" or "https://github.com/" is allowed.
func ParseGitHubRef(s string) (GitHubRef, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimPrefix(s, "gh:")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")

	ref := ""
	if at := strings.LastIndexByte(s, '@'); at >= 0 {
		ref = s[at+1:]
		s = s[:at]
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return GitHubRef{}, fmt.Errorf("invalid GitHub ref %q (want owner/repo[/path][@ref])", s)
	}
	g := GitHubRef{Owner: parts[0], Repo: parts[1], Ref: ref}
	if len(parts) > 2 {
		g.Path = strings.Join(parts[2:], "/")
	}
	return g, nil
}

// rawURL builds the raw.githubusercontent.com URL for a file in the ref.
func (g GitHubRef) rawURL(file string) string {
	ref := g.Ref
	if ref == "" {
		ref = "HEAD"
	}
	p := strings.Trim(g.Path, "/")
	full := file
	if p != "" {
		full = p + "/" + file
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", g.Owner, g.Repo, ref, full)
}

// InstallFromGitHub fetches a skill's SKILL.md from GitHub (via raw.github
// usercontent.com), scans it, and installs it. fetch is injected so the call is
// testable; pass DefaultFetcher in production.
func InstallFromGitHub(ctx context.Context, ref GitHubRef, fetch Fetcher, opts InstallOptions) (Installed, error) {
	if fetch == nil {
		return Installed{}, fmt.Errorf("install: nil fetcher")
	}
	url := ref.rawURL("SKILL.md")
	data, err := fetch(ctx, url)
	if err != nil {
		return Installed{}, fmt.Errorf("fetch %s: %w", url, err)
	}
	name := opts.Name
	if name == "" {
		// Default the name to the last path segment, else the repo name.
		if p := strings.Trim(ref.Path, "/"); p != "" {
			name = filepath.Base(p)
		} else {
			name = ref.Repo
		}
	}
	return finishInstall(ctx, string(data), name, opts)
}

// finishInstall scans (optionally) then writes the skill into opts.Dir.
func finishInstall(ctx context.Context, content, fallbackName string, opts InstallOptions) (Installed, error) {
	if strings.TrimSpace(content) == "" {
		return Installed{}, fmt.Errorf("empty skill content")
	}
	// Resolve the skill name: explicit override → frontmatter name → fallback.
	name := opts.Name
	if name == "" {
		if fn, _ := parseFrontmatter(content); fn != "" {
			name = fn
		} else {
			name = fallbackName
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Installed{}, fmt.Errorf("could not determine a skill name")
	}

	var scan ScanResult
	scan.Safe = true // default when no scanner is configured
	if opts.Scanner != nil {
		res, err := opts.Scanner.Scan(ctx, name, content)
		if err != nil {
			return Installed{}, fmt.Errorf("scan: %w", err)
		}
		scan = res
		if !res.Safe && !opts.Force {
			return Installed{Name: name, Scan: res}, &RiskyError{Name: name, Reasons: res.Reasons}
		}
	}

	desc := ""
	if _, d := parseFrontmatter(content); d != "" {
		desc = d
	}
	body := stripFrontmatter(content)

	if opts.Overwrite {
		_ = os.RemoveAll(filepath.Join(opts.Dir, name))
	}
	path, err := Save(opts.Dir, name, desc, body)
	if err != nil {
		return Installed{Name: name, Scan: scan}, err
	}
	return Installed{Name: name, Path: path, Scan: scan}, nil
}

// RiskyError is returned when a scan flags a skill and Force is not set.
type RiskyError struct {
	Name    string
	Reasons []string
}

func (e *RiskyError) Error() string {
	r := strings.Join(e.Reasons, "; ")
	if r == "" {
		r = "flagged by the security scan"
	}
	return fmt.Sprintf("skill %q looks risky and was not installed: %s (use --force to override)", e.Name, r)
}
