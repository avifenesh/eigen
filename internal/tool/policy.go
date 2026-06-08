package tool

import (
	"os"
	"path/filepath"
	"strings"
)

// Policy confines filesystem tools: a path must resolve to a location under one
// of the policy's roots and must not match a denied directory or filename
// pattern. It is enforced inside each tool (defense in depth), independent of
// the agent loop's gated/auto permission posture.
type Policy struct {
	roots []string // absolute, symlink-resolved, cleaned roots
}

// deniedSegments are directory names that are never traversable (compared
// case-insensitively).
var deniedSegments = []string{".ssh", ".aws", ".gnupg"}

// deniedBasenames are filename globs (filepath.Match, case-insensitive) that are
// never readable.
var deniedBasenames = []string{
	".env", ".env.*", "*.pem", "*.key", "id_rsa", "id_rsa.pub", "id_ed25519",
	"credentials", "*.kdbx", ".npmrc", ".pypirc", ".netrc", "*.p12", "*.pfx",
}

// NewPolicy builds a policy whose roots are made absolute, symlink-resolved, and
// cleaned so prefix checks against resolved paths are sound.
func NewPolicy(roots ...string) *Policy {
	p := &Policy{}
	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		p.roots = append(p.roots, filepath.Clean(abs))
	}
	return p
}

// DefaultPolicy confines tools to the current working directory.
func DefaultPolicy() *Policy {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return NewPolicy(cwd)
}

// Resolve validates path against the policy and returns the absolute, symlink-
// resolved path to operate on, or an error explaining the denial.
func (p *Policy) Resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved := resolveSymlinks(filepath.Clean(abs))

	if !p.within(resolved) {
		return "", &DeniedError{Path: path, Reason: "outside the allowed roots"}
	}
	if reason := deniedReason(resolved); reason != "" {
		return "", &DeniedError{Path: path, Reason: reason}
	}
	return resolved, nil
}

func (p *Policy) within(path string) bool {
	for _, root := range p.roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolveSymlinks resolves symlinks for the path, or — when the path does not
// yet exist (e.g. a file a write tool will create) — resolves the longest
// existing ancestor and keeps the remainder literal. This prevents a symlinked
// parent directory from escaping a root on create.
func resolveSymlinks(abs string) string {
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	dir := abs
	var rest []string
	for {
		rest = append([]string{filepath.Base(dir)}, rest...)
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root with nothing resolvable
		}
		if r, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		}
		dir = parent
	}
	return abs
}

func deniedReason(path string) string {
	lower := strings.ToLower(path)
	for _, seg := range strings.Split(lower, string(filepath.Separator)) {
		for _, d := range deniedSegments {
			if seg == d {
				return "sensitive directory " + d
			}
		}
	}
	base := filepath.Base(lower)
	for _, g := range deniedBasenames {
		if ok, _ := filepath.Match(g, base); ok {
			return "sensitive file pattern " + g
		}
	}
	return ""
}

// DeniedError is returned when a path violates the policy.
type DeniedError struct {
	Path   string
	Reason string
}

func (e *DeniedError) Error() string {
	return "path " + e.Path + " is denied: " + e.Reason
}
