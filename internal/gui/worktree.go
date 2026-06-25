package gui

// Worktree bridge layer: two read-only right-panel tools that run on the host
// (full fs + git access, no daemon round-trip).
//
//   - WorkingDiff(dir)      — the working tree's pending changes vs HEAD.
//   - FileTree(dir)         — a depth-limited file-explorer tree of a project dir.
//   - ReadFileForView(path) — a file's text for click-to-view.
//
// The frontend passes the dir (typically SessionStateDTO.Roots[0]). All three
// surface errors rather than swallowing them, except the deliberate
// "not a git repo" case which is a normal state, not a failure.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// worktreeGitTimeout bounds each git subprocess so a pathological repo (huge
	// pack, hung index lock) can't wedge the UI thread.
	worktreeGitTimeout = 8 * time.Second

	// maxPatchBytes caps the unified diff text we hand the webview. A 512KiB diff
	// already overflows any human-reviewable panel; past that we truncate and say so.
	maxPatchBytes = 512 << 10

	// fileTreeDepth is how deep FileTree descends (root entries are depth 1).
	fileTreeDepth = 3
	// maxTreeEntries caps total nodes so a pathological dir can't balloon the DTO.
	maxTreeEntries = 2000

	// maxViewBytes caps a single file we return for click-to-view.
	maxViewBytes = 256 << 10
	// binarySniffBytes is how much of a file we scan for a NUL byte before
	// deciding it's binary and refusing to return it as text.
	binarySniffBytes = 8 << 10
)

// worktreeSkip names directories never worth listing in the file explorer.
var worktreeSkip = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".svelte-kit": true, "target": true, ".next": true,
	".venv": true, "__pycache__": true, ".eigen": true,
}

// DiffFileDTO is one changed path with its line-delta, parsed from `git diff
// --numstat`. Adds/Dels are -1 for binary files (git reports them as "-").
type DiffFileDTO struct {
	Path string `json:"path"`
	Adds int    `json:"adds"`
	Dels int    `json:"dels"`
}

// WorkingDiffDTO is the working tree's pending changes vs HEAD. IsRepo=false
// (with no error) means dir simply isn't a git repo. Clean=true means it's a
// repo with no pending changes. Patch is the full unified diff text, capped at
// maxPatchBytes (a truncation note is appended when it overflows).
type WorkingDiffDTO struct {
	Dir       string        `json:"dir"`
	Branch    string        `json:"branch"`
	Patch     string        `json:"patch"`
	Files     []DiffFileDTO `json:"files"`
	IsRepo    bool          `json:"isRepo"`
	Clean     bool          `json:"clean"`
	Truncated bool          `json:"truncated"`
}

// FileEntryDTO is one node in the file-explorer tree. Path is absolute so the
// frontend can pass it straight back to ReadFileForView. Children is omitted
// for files and for dirs at the depth limit.
type FileEntryDTO struct {
	Name     string         `json:"name"`
	Path     string         `json:"path"`
	IsDir    bool           `json:"isDir"`
	Children []FileEntryDTO `json:"children,omitempty"`
}

// FileTreeDTO is the shallow tree rooted at Dir. Truncated=true means the
// entry cap (maxTreeEntries) was hit and the listing is partial.
type FileTreeDTO struct {
	Dir       string         `json:"dir"`
	Entries   []FileEntryDTO `json:"entries"`
	Truncated bool           `json:"truncated"`
}

// WorkingDiff runs `git -C <dir> diff HEAD` over the working tree and returns
// the pending changes vs HEAD.
//
// We use `git diff HEAD` (not bare `git diff`, which only shows the unstaged
// delta against the index) so the patch and per-file stats cover ALL pending
// changes — staged AND unstaged — in a single subprocess. The per-file
// adds/dels come from `git diff HEAD --numstat`. The patch text is capped at
// maxPatchBytes. If dir isn't a git repo, IsRepo is false and err is nil.
func (b *Bridge) WorkingDiff(dir string) (*WorkingDiffDTO, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("working diff: empty dir")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("working diff: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("working diff: %q is not a directory", dir)
	}

	out := &WorkingDiffDTO{Dir: dir}

	// Repo probe: a non-repo dir is a normal state, not an error.
	if s, err := worktreeGit(dir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(s) != "true" {
		return out, nil
	}
	out.IsRepo = true

	out.Branch = worktreeBranch(dir)

	// Per-file line deltas across all pending changes vs HEAD.
	if s, err := worktreeGit(dir, "diff", "HEAD", "--numstat"); err == nil {
		out.Files = parseNumstat(s)
	} else {
		return nil, fmt.Errorf("working diff numstat: %w", err)
	}

	// Full unified diff, capped.
	patch, err := worktreeGit(dir, "diff", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("working diff: %w", err)
	}
	if len(patch) > maxPatchBytes {
		patch = patch[:maxPatchBytes]
		out.Truncated = true
		patch += fmt.Sprintf("\n... [diff truncated at %d KiB] ...\n", maxPatchBytes>>10)
	}
	out.Patch = patch
	out.Clean = len(out.Files) == 0 && strings.TrimSpace(out.Patch) == ""
	return out, nil
}

// worktreeBranch returns the current branch name, or a detached/unknown
// fallback (mirrors the TUI git panel's logic).
func worktreeBranch(dir string) string {
	if s, err := worktreeGit(dir, "branch", "--show-current"); err == nil {
		if b := strings.TrimSpace(s); b != "" {
			return b
		}
	}
	if s, err := worktreeGit(dir, "rev-parse", "--short", "HEAD"); err == nil {
		if b := strings.TrimSpace(s); b != "" {
			return "detached@" + b
		}
	}
	return "(unknown)"
}

// parseNumstat parses `git diff --numstat` lines ("adds\tdels\tpath") into
// per-file deltas. Binary files report "-" for adds/dels; we encode those as -1.
func parseNumstat(out string) []DiffFileDTO {
	var files []DiffFileDTO
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		files = append(files, DiffFileDTO{
			Path: parts[2],
			Adds: numstatCount(parts[0]),
			Dels: numstatCount(parts[1]),
		})
	}
	return files
}

func numstatCount(s string) int {
	if s == "-" { // binary file
		return -1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// worktreeGit runs `git -C <dir> <args...>` with a bounded timeout, returning
// stdout. Stderr is folded into the error so a git failure surfaces its reason.
func worktreeGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeGitTimeout)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s timed out after %s", strings.Join(args, " "), worktreeGitTimeout)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return stdout.String(), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// FileTree returns a depth-limited file-explorer tree of dir. Build/VCS noise
// dirs are skipped; entries are sorted dirs-first then by name. Total nodes are
// capped at maxTreeEntries (Truncated reports a partial listing).
func (b *Bridge) FileTree(dir string) (*FileTreeDTO, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("file tree: empty dir")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("file tree: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("file tree: %q is not a directory", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("file tree: %w", err)
	}

	out := &FileTreeDTO{Dir: abs}
	count := 0
	var truncated bool

	var walk func(d string, depth int) []FileEntryDTO
	walk = func(d string, depth int) []FileEntryDTO {
		if depth > fileTreeDepth || truncated {
			return nil
		}
		entries, err := readDirSorted(d)
		if err != nil {
			return nil // unreadable subdir: skip quietly
		}
		var nodes []FileEntryDTO
		for _, e := range entries {
			if count >= maxTreeEntries {
				truncated = true
				break
			}
			name := e.Name()
			isDir := e.IsDir()
			if isDir && worktreeSkip[name] {
				continue
			}
			if strings.HasPrefix(name, ".") { // skip hidden entries
				continue
			}
			count++
			node := FileEntryDTO{
				Name:  name,
				Path:  filepath.Join(d, name),
				IsDir: isDir,
			}
			if isDir {
				node.Children = walk(node.Path, depth+1)
			}
			nodes = append(nodes, node)
		}
		return nodes
	}

	out.Entries = walk(abs, 1)
	out.Truncated = truncated
	return out, nil
}

// readDirSorted lists a directory, directories first then files, each alpha.
func readDirSorted(dir string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di // dirs first
		}
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

// ReadFileForView returns a file's text content for click-to-view. The path
// must be absolute and exist; the file is rejected if it looks binary (a NUL
// byte in the first binarySniffBytes) or is invalid UTF-8, and content is
// capped at maxViewBytes (a truncation note is appended when it overflows).
func (b *Bridge) ReadFileForView(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("read file: empty path")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("read file: %q is not an absolute path", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read file: %q is a directory", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	defer f.Close()

	// Read one byte past the cap so a file exactly at the cap isn't falsely
	// flagged truncated, while anything larger is.
	raw, err := io.ReadAll(io.LimitReader(f, maxViewBytes+1))
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	// Binary sniff: a NUL byte in the head means this isn't text.
	sniff := raw
	if len(sniff) > binarySniffBytes {
		sniff = sniff[:binarySniffBytes]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		return "", fmt.Errorf("read file: %q looks binary", path)
	}

	truncated := false
	if len(raw) > maxViewBytes {
		raw = raw[:maxViewBytes]
		truncated = true
	}

	content := string(raw)
	if !utf8.ValidString(content) {
		return "", fmt.Errorf("read file: %q is not valid UTF-8", path)
	}
	if truncated {
		content += fmt.Sprintf("\n... [file truncated at %d KiB] ...\n", maxViewBytes>>10)
	}
	return content, nil
}
