package memory

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Canonical project-scope resolution. A memory scope must identify the PROJECT,
// not the directory you happened to launch from. Without this, a git worktree,
// a subdirectory, or a `cd ..`-rooted session each hashed to its own scope —
// fragmenting one project's memory across several stores (observed live: the
// main checkout and a codex worktree of the same repo held separate scopes).
//
// canonicalProjectDir maps any dir inside a git repo to the repo's MAIN-worktree
// root, which is stable across linked worktrees and subdirectories: git's
// --git-common-dir is SHARED by every worktree of a repo, so its parent is the
// one root they all agree on. Non-git dirs (and dirs where git is unavailable)
// fall back to the absolute path — unchanged from the pre-canonical behavior, so
// no existing non-repo scope re-keys.

// gitRootTimeout bounds the per-dir git probe so a slow/hung git never stalls a
// memory Open (which can run in tight loops over many dirs, e.g. the feed scan).
const gitRootTimeout = 3 * time.Second

// canonCache memoizes input-dir → canonical-root so repeated Open(dir) calls in
// a process (feed scans, session restore) shell git at most once per dir.
var canonCache sync.Map // map[string]string

// canonicalProjectDir returns the stable scope root for a project directory: the
// git main-worktree root when dir is inside a repo, else the absolute path. The
// result is cached per process. A blank dir is returned as-is (callers resolve
// "" to cwd before keying).
func canonicalProjectDir(dir string) string {
	if dir == "" {
		return dir
	}
	if v, ok := canonCache.Load(dir); ok {
		return v.(string)
	}
	root := resolveProjectRoot(dir)
	canonCache.Store(dir, root)
	return root
}

// resolveProjectRoot does the uncached resolution. It prefers the git
// main-worktree root (shared across worktrees), then the working-tree top (a
// subdir falls back to this if --git-common-dir is unavailable), then the
// symlink-resolved absolute path.
func resolveProjectRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil && resolved != "" {
		abs = resolved
	}
	// --git-common-dir is the directory ALL worktrees of a repo share (the main
	// repo's .git). Its parent is the main-worktree root that every worktree and
	// subdir of the project maps to. --path-format=absolute needs git ≥ 2.31.
	if common := gitRootText(abs, "rev-parse", "--path-format=absolute", "--git-common-dir"); common != "" {
		if root := mainRootFromCommonDir(common); root != "" {
			if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil && resolved != "" {
				return resolved
			}
			return root
		}
	}
	// Fallback for older git or odd layouts: the working-tree top maps a subdir
	// to its (worktree's) root. Linked worktrees still report their own top here,
	// so this is a weaker guarantee than --git-common-dir, but better than cwd.
	if top := gitRootText(abs, "rev-parse", "--show-toplevel"); top != "" {
		if resolved, rerr := filepath.EvalSymlinks(top); rerr == nil && resolved != "" {
			return resolved
		}
		return top
	}
	// Not a git repo (or git missing): the absolute path, exactly as before.
	return abs
}

// mainRootFromCommonDir turns a --git-common-dir value into the main-worktree
// root. The common dir is normally "<root>/.git"; its parent is the root. A
// bare/odd gitdir that doesn't end in ".git" yields "" so the caller falls back.
func mainRootFromCommonDir(common string) string {
	common = strings.TrimSpace(common)
	if common == "" {
		return ""
	}
	common = filepath.Clean(common)
	if filepath.Base(common) != ".git" {
		return "" // bare repo or detached gitdir — not a worktree root
	}
	root := filepath.Dir(common)
	if root == "" || root == "." || root == string(filepath.Separator) {
		return ""
	}
	return root
}

// gitRootText runs `git -C dir <args>` and returns trimmed stdout, or "" on any
// error (not a repo, git missing, timeout). Intentionally silent: a failed
// probe just means "not resolvable", and the caller falls back to the abs path.
func gitRootText(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitRootTimeout)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
