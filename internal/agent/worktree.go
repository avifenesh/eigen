package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mutating parallel fan-out (Tier 16 v2): implementer children edit code in
// ISOLATED git worktrees, the parent captures each child's diff, validates the
// combined patch set in a throwaway worktree, and applies the clean result to
// the real workspace behind ONE approval. Isolation + patch-merge is what makes
// parallel WRITES safe; the read-only path (task_group) stays separate.
//
// Hard safety rails (per cross-vendor review):
//   - git repo only; session root must equal repo root; clean index+worktree;
//     born HEAD. Anything else → refuse.
//   - implementer children get read/search/write/edit/move ONLY — NO bash, NO
//     git, NO network (a worktree confines file writes but NOT shelling out).
//   - children never run git; the PARENT diffs each worktree.
//   - patches validated in a throwaway worktree before the real tree is touched;
//     conflicts are reported, never written as markers.
//   - approval happens at APPLY time on the exact combined diff.

// repoGitMu serializes worktree add/remove and ref-touching git ops per process
// (the daemon hosts many sessions; worktree metadata under .git is shared).
var repoGitMu sync.Mutex

const (
	gitOpTimeout     = 30 * time.Second
	maxPatchBytes    = 2 << 20 // 2 MiB per child patch
	maxCombinedBytes = 6 << 20 // 6 MiB combined
)

// gitText runs a git command in dir and returns trimmed stdout (errors include
// stderr). No shell; args are passed directly.
func gitText(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitRaw runs git and returns raw (untrimmed) stdout bytes — for diffs/patches
// where trimming/encoding must be preserved.
func gitRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// repoState is the verified git baseline for a mutating fan-out.
type repoState struct {
	root string // repo top-level (== session root, enforced)
	head string // base commit SHA
}

// precheckMutatingFanout verifies dir is safe for mutating parallel work and
// returns the baseline. Refuses (clear error) on: not a git repo, session root
// != repo root, unborn HEAD, or a dirty index/working tree.
func precheckMutatingFanout(ctx context.Context, dir string) (*repoState, error) {
	top, err := gitText(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("mutating fan-out needs a git repo (%v) — use the `task` tool for non-git or one-at-a-time edits", err)
	}
	absDir, _ := filepath.Abs(dir)
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	if topResolved, err := filepath.EvalSymlinks(top); err == nil {
		top = topResolved
	}
	if filepath.Clean(absDir) != filepath.Clean(top) {
		return nil, fmt.Errorf("mutating fan-out needs the session rooted at the repo root (session=%s, repo=%s)", absDir, top)
	}
	head, err := gitText(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("mutating fan-out needs a born HEAD (make an initial commit first): %v", err)
	}
	// Clean index + working tree.
	status, err := gitText(ctx, dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(status) != "" {
		return nil, fmt.Errorf("mutating fan-out needs a clean working tree — commit or stash your changes first")
	}
	return &repoState{root: top, head: head}, nil
}

// addWorktree creates a detached worktree at base under parent, named uniquely.
// Serialized: worktree add writes shared .git/worktrees metadata.
func addWorktree(ctx context.Context, repoRoot, parent, name, base string) (string, error) {
	wt := filepath.Join(parent, name)
	repoGitMu.Lock()
	defer repoGitMu.Unlock()
	if _, err := gitText(ctx, repoRoot, "worktree", "add", "--detach", wt, base); err != nil {
		return "", err
	}
	return wt, nil
}

// removeWorktree force-removes a worktree we created (serialized). Best-effort;
// returns the error for logging.
func removeWorktree(ctx context.Context, repoRoot, wt string) error {
	repoGitMu.Lock()
	defer repoGitMu.Unlock()
	_, err := gitText(ctx, repoRoot, "worktree", "remove", "--force", wt)
	return err
}

// capturePatch returns the child's changes as a binary-safe unified diff
// against base, including untracked (non-ignored) files via intent-to-add.
// Empty result means the child made no changes.
func capturePatch(ctx context.Context, wt, base string) ([]byte, error) {
	// Intent-to-add untracked, non-ignored files so they appear in the diff.
	untracked, err := gitText(ctx, wt, "ls-files", "-o", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(untracked) != "" {
		args := []string{"add", "-N", "--"}
		for _, f := range strings.Split(untracked, "\n") {
			if f = strings.TrimSpace(f); f != "" {
				args = append(args, f)
			}
		}
		if _, err := gitText(ctx, wt, args...); err != nil {
			return nil, err
		}
	}
	// Binary-safe, full-index, no external diff/textconv (no user hooks run).
	patch, err := gitRaw(ctx, wt, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", base, "--")
	if err != nil {
		return nil, err
	}
	if len(patch) > maxPatchBytes {
		return nil, fmt.Errorf("patch too large (%d bytes > %d)", len(patch), maxPatchBytes)
	}
	return patch, nil
}

// applyCheck reports whether patch applies cleanly at dir (no changes made).
func applyCheck(ctx context.Context, dir string, patch []byte) bool {
	cctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "apply", "--check", "--3way", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(string(patch))
	return cmd.Run() == nil
}

// applyPatch applies patch to the working tree at dir (real apply).
func applyPatch(ctx context.Context, dir string, patch []byte) error {
	cctx, cancel := context.WithTimeout(ctx, gitOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "apply", "--3way", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(string(patch))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// mkTempWorktreeParent makes a 0700 temp dir (outside the repo) to hold child
// worktrees for one fan-out run.
func mkTempWorktreeParent() (string, error) {
	return os.MkdirTemp("", "eigen-fanout-*")
}
