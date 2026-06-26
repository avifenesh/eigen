package feed

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds each git probe (local, should be instant).
const gitTimeout = 3 * time.Second

// maxGitItems caps git suggestions so one messy machine doesn't flood the feed.
const maxGitItems = 6

// scanGit probes each project's git state for actionable loose ends:
// uncommitted changes and unpushed commits.
func scanGit(dirs []string) []Item {
	var items []Item
	for _, dir := range dirs {
		if len(items) >= maxGitItems {
			break
		}
		if !isGitRepo(dir) {
			continue
		}
		name := filepath.Base(dir)
		if n := dirtyFiles(dir); n > 0 {
			items = append(items, Item{
				Kind:   "git",
				Title:  fmt.Sprintf("%s: %d uncommitted file(s)", name, n),
				Detail: "review the working tree and commit coherent chunks",
				Dir:    dir,
				Task: "The working tree has uncommitted changes. Run `git status` and `git diff`, " +
					"review what's there, and commit it in coherent chunks with good messages. " +
					"Ask me only if something looks half-finished or risky to commit.",
			})
		}
		if n := unpushed(dir); n > 0 {
			items = append(items, Item{
				Kind:   "git",
				Title:  fmt.Sprintf("%s: %d unpushed commit(s)", name, n),
				Detail: "local commits not on the remote",
				Dir:    dir,
				Task: fmt.Sprintf("There are %d local commits not pushed to the remote. "+
					"Show me `git log @{u}..HEAD --oneline`, summarize what they contain, and ask whether to push.", n),
			})
		}
		if n := behind(dir); n > 0 {
			items = append(items, Item{
				Kind:   "git",
				Title:  fmt.Sprintf("%s: behind upstream by %d commit(s)", name, n),
				Detail: "the remote moved — review + integrate",
				Dir:    dir,
				Task: fmt.Sprintf("This checkout is %d commits behind its upstream. "+
					"Run `git fetch` then `git log --oneline HEAD..@{u}` and summarize what changed upstream. "+
					"If my working tree is clean and the changes look safe, rebase/pull; otherwise tell me what conflicts to expect.", n),
			})
		}
	}
	return items
}

func isGitRepo(dir string) bool {
	out, err := gitIn(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// dirtyFiles returns the count of modified/untracked files. Uses the
// NUL-separated porcelain format so paths containing newlines stay intact and
// rename/copy records (which carry a trailing origin path as a second field)
// are counted as the single change they represent.
func dirtyFiles(dir string) int {
	out, err := gitIn(dir, "status", "--porcelain", "-z")
	if err != nil {
		return 0
	}
	// -z terminates each field with NUL; a trailing NUL leaves an empty tail.
	fields := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	n := 0
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		n++
		// Rename (R) and copy (C) records spell "XY <path>" and are followed
		// by a separate field holding the origin path — skip it so the pair
		// counts as one file. The status code is the first two columns.
		if len(f) >= 2 && (f[0] == 'R' || f[0] == 'C') {
			i++
		}
	}
	return n
}

// behind returns the count of upstream commits not in HEAD (0 when no
// upstream). Reads only local refs — the feed scan must stay fast and
// offline; a stale remote-tracking ref just means the count lags until the
// next fetch (cron, IDE, or any pull updates it).
func behind(dir string) int {
	out, err := gitIn(dir, "rev-list", "--count", "HEAD..@{u}")
	if err != nil {
		return 0
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

// unpushed returns the count of commits ahead of upstream (0 when no upstream).
func unpushed(dir string) int {
	out, err := gitIn(dir, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return 0
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

func gitIn(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
