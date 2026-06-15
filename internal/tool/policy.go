package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Policy confines filesystem tools: a path must resolve to a location under one
// of the policy's roots and must not match a denied directory or filename
// pattern. It is enforced inside each tool (defense in depth), independent of
// the agent loop's gated/auto permission posture.
//
// Roots can be extended at runtime via AddRoot (the user-invoked /add-dir
// grant). The daemon hosts many sessions in one process and a session's tools
// all share one *Policy across goroutines (the turn runs on a different
// goroutine than control ops), so every read of roots and the AddRoot write are
// guarded by mu.
type Policy struct {
	mu    sync.RWMutex
	roots []string // absolute, symlink-resolved, cleaned roots
}

// deniedSegments are directory names that are never traversable (compared
// case-insensitively).
var deniedSegments = []string{".ssh", ".aws", ".gnupg", ".git"}

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

// Dir returns the policy's primary root (the project dir tools operate in).
func (p *Policy) Dir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.roots) > 0 {
		return p.roots[0]
	}
	return "."
}

// Roots returns a copy of the policy's allowed roots (primary first).
func (p *Policy) Roots() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string(nil), p.roots...)
}

// AddRoot extends the policy with an additional allowed directory at runtime —
// the user-invoked /add-dir grant (never the agent; the agent can't widen its
// own sandbox). The path must be an existing directory and must not itself sit
// under a denied segment (e.g. ~/.ssh). It is made absolute, symlink-resolved,
// and cleaned exactly like NewPolicy, then appended (deduped). The primary root
// (roots[0], used for bash cwd + relative resolution) is unchanged. Returns the
// normalized root that was added.
func (p *Policy) AddRoot(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	abs = filepath.Clean(abs)
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not an existing directory", path)
	}
	if reason := deniedReason(abs); reason != "" {
		return "", fmt.Errorf("%s is denied (%s)", path, reason)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ex := range p.roots {
		if ex == abs {
			return abs, nil // already a root (idempotent)
		}
	}
	p.roots = append(p.roots, abs)
	return abs, nil
}

// Resolve validates path against the policy and returns the absolute, symlink-
// resolved path to operate on, or an error explaining the denial. Relative
// paths resolve against the PRIMARY ROOT, not the process cwd — a daemon
// hosts many sessions rooted at different projects in one process, so the
// process cwd is meaningless.
func (p *Policy) Resolve(path string) (string, error) {
	primary := p.Dir()
	if !filepath.IsAbs(path) && primary != "." {
		path = filepath.Join(primary, path)
	}
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
	p.mu.RLock()
	defer p.mu.RUnlock()
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

// IsDenied reports whether path matches a denied directory or filename pattern.
func IsDenied(path string) bool { return deniedReason(path) != "" }

// DenyGlobs returns ripgrep -g exclude args for every denied pattern, so search
// and file-listing tools never read or enumerate sensitive files. Excludes are
// placed last so they take precedence over a caller's include glob.
func DenyGlobs() []string {
	var args []string
	for _, d := range deniedSegments {
		args = append(args, "-g", "!"+d, "-g", "!"+d+"/**")
	}
	for _, g := range deniedBasenames {
		args = append(args, "-g", "!"+g)
	}
	return args
}

// FilterDeniedLines drops any output line whose path (extracted by pathOf)
// matches a denied pattern — defense in depth behind DenyGlobs.
func FilterDeniedLines(out string, pathOf func(string) string) string {
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln != "" && IsDenied(pathOf(ln)) {
			continue
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

// TruncateUTF8 truncates s to at most max bytes without splitting a UTF-8 rune.
func TruncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// DeniedError is returned when a path violates the policy.
type DeniedError struct {
	Path   string
	Reason string
}

func (e *DeniedError) Error() string {
	return "path " + e.Path + " is denied: " + e.Reason
}
