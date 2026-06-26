package memory

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit makes a real repo with one commit at dir, or skips if git is absent.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "init")
}

// TestCanonicalProjectDir_SubdirSharesRoot is the core scope-fragmentation fix:
// a subdirectory of a repo must resolve to the same canonical root as the repo
// top, so they key the same memory scope.
func TestCanonicalProjectDir_SubdirSharesRoot(t *testing.T) {
	repo := t.TempDir()
	gitInit(t, repo)
	sub := filepath.Join(repo, "internal", "gui")
	if err := exec.Command("mkdir", "-p", sub).Run(); err != nil {
		t.Fatal(err)
	}

	rootFromTop := canonicalProjectDir(repo)
	rootFromSub := canonicalProjectDir(sub)
	if rootFromTop != rootFromSub {
		t.Fatalf("subdir keyed a different scope than repo root:\n top=%q\n sub=%q", rootFromTop, rootFromSub)
	}
	// And the canonical root must actually be the repo top (resolved).
	wantTop, _ := filepath.EvalSymlinks(repo)
	if rootFromTop != wantTop {
		t.Fatalf("canonical root = %q, want repo top %q", rootFromTop, wantTop)
	}
}

// TestCanonicalProjectDir_WorktreeSharesRoot is the worktree case the user hit:
// a linked git worktree must map to the SAME scope as the main checkout (git's
// --git-common-dir is shared across worktrees).
func TestCanonicalProjectDir_WorktreeSharesRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	gitInit(t, repo)
	wt := filepath.Join(t.TempDir(), "linked")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", "--detach", wt, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("worktree add failed (%v): %s", err, out)
	}
	defer exec.Command("git", "-C", repo, "worktree", "remove", "--force", wt).Run()

	rootMain := canonicalProjectDir(repo)
	rootWT := canonicalProjectDir(wt)
	if rootMain != rootWT {
		t.Fatalf("worktree fragmented the scope:\n main=%q\n  wt=%q", rootMain, rootWT)
	}
}

// TestCanonicalProjectDir_NonGitFallsBackToAbs guarantees no behavior change for
// non-repo dirs: the canonical root is the symlink-resolved absolute path, so
// existing non-git scopes keep their keys.
func TestCanonicalProjectDir_NonGitFallsBackToAbs(t *testing.T) {
	dir := t.TempDir() // a plain dir, no git
	got := canonicalProjectDir(dir)
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("non-git dir = %q, want abs %q", got, want)
	}
}

// TestOpen_SubdirAndRootSameStore is the end-to-end guarantee: Open(repo) and
// Open(repo/sub) return stores at the same on-disk scope dir.
func TestOpen_SubdirAndRootSameStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate ~/.eigen/memory
	repo := t.TempDir()
	gitInit(t, repo)
	sub := filepath.Join(repo, "pkg", "deep")
	if err := exec.Command("mkdir", "-p", sub).Run(); err != nil {
		t.Fatal(err)
	}

	sTop, err := Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	sSub, err := Open(sub)
	if err != nil {
		t.Fatal(err)
	}
	if sTop.Dir() != sSub.Dir() {
		t.Fatalf("Open(repo) and Open(subdir) keyed different stores:\n top=%q\n sub=%q", sTop.Dir(), sSub.Dir())
	}
}
