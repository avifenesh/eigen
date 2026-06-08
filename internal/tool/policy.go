package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Policy confines filesystem tools: a path must resolve to a location under one
// of Roots and must not match a denied directory or filename pattern. It is
// enforced inside each tool (defense in depth), independent of the agent loop's
// gated/auto permission posture.
type Policy struct {
	Roots []string // absolute, cleaned roots a path must fall within
}

// deniedSegments are directory names that are never traversable.
var deniedSegments = []string{".ssh", ".aws", ".gnupg"}

// deniedBasenames are filename globs (filepath.Match) that are never readable.
var deniedBasenames = []string{".env", "*.pem", "*.key", "id_rsa", "id_rsa.pub", "credentials", "*.kdbx"}

// DefaultPolicy confines tools to the current working directory.
func DefaultPolicy() *Policy {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	return &Policy{Roots: []string{filepath.Clean(abs)}}
}

// Resolve validates path against the policy and returns the absolute, symlink-
// resolved path to operate on, or an error explaining the denial.
func (p *Policy) Resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	resolved := resolveSymlinks(filepath.Clean(abs))

	if !p.within(resolved) {
		return "", fmt.Errorf("path %q is outside the allowed roots", path)
	}
	if reason := deniedReason(resolved); reason != "" {
		return "", fmt.Errorf("path %q is denied: %s", path, reason)
	}
	return resolved, nil
}

func (p *Policy) within(path string) bool {
	for _, root := range p.Roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolveSymlinks resolves a fully-existing path's symlinks so a link inside a
// root cannot point outside it. For paths that do not yet exist (e.g. a file a
// future write tool will create) it returns the cleaned input; tighter ancestor
// resolution lands with the write tools.
func resolveSymlinks(abs string) string {
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}

func deniedReason(path string) string {
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		for _, d := range deniedSegments {
			if seg == d {
				return "sensitive directory " + d
			}
		}
	}
	base := filepath.Base(path)
	for _, g := range deniedBasenames {
		if ok, _ := filepath.Match(g, base); ok {
			return "sensitive file pattern " + g
		}
	}
	return ""
}
