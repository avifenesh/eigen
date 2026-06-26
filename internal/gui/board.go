package gui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/feed"
)

// Work board — the cross-project command surface. eigen is a working station;
// this is "what's going on across ALL my projects" in one place: per-project
// lanes carrying git state (branch, uncommitted / unpushed / behind), open
// GitHub PRs/issues + git loose-ends (from the proactive feed), and a TODO/FIXME
// count. Each lane's items are one-click startable like the Home feed.
//
// Data sources, all best-effort + fast: the cached feed (instant; github/git
// signals already scanned in the background) grouped by project dir, plus a
// couple of local git probes per lane (branch + TODO grep). No new scanners.

// BoardItemDTO is one actionable card in a lane (mirrors a feed item).
type BoardItemDTO struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"` // git | github
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Dir    string `json:"dir,omitempty"`
	Task   string `json:"task,omitempty"`
	URL    string `json:"url,omitempty"`
}

// BoardLaneDTO is one project's column. A lane is either LOCAL (a checkout, with
// git state) or REMOTE (a GitHub repo with no local clone, just open PR/issue
// counts + a URL).
type BoardLaneDTO struct {
	Name     string         `json:"name"`   // project basename / repo name
	Dir      string         `json:"dir"`    // absolute path (local lanes; "" for remote)
	Branch   string         `json:"branch"` // current git branch ("" when not a repo)
	Dirty    int            `json:"dirty"`  // uncommitted files
	Unpushed int            `json:"unpushed"`
	Behind   int            `json:"behind"`
	Todos    int            `json:"todos"` // TODO/FIXME count (tracked files)
	OpenPRs  int            `json:"openPrs"`
	OpenIss  int            `json:"openIss"`
	Items    []BoardItemDTO `json:"items"` // actionable cards (feed items for this dir)
	// Remote-only fields.
	Remote bool   `json:"remote"` // a GitHub repo lane (no local checkout)
	Repo   string `json:"repo"`   // owner/name (remote lanes)
	URL    string `json:"url"`    // repo URL (remote lanes)
}

// BoardDTO is the whole board.
type BoardDTO struct {
	Lanes   []BoardLaneDTO `json:"lanes"`
	Scanned string         `json:"scanned,omitempty"` // RFC3339 of the underlying feed scan
}

// Board builds the cross-project work board from the cached feed (grouped by
// project) enriched with per-lane git + TODO probes. Instant: it does NOT
// trigger a feed rescan (the background feedLoop owns that); it reads what's
// cached so the board renders immediately.
func (b *Bridge) Board() (*BoardDTO, error) {
	f, _ := feed.Load()

	// Group feed git/github items by their project dir.
	byDir := map[string][]BoardItemDTO{}
	for _, it := range f.Items {
		if it.Kind != "git" && it.Kind != "github" {
			continue
		}
		dir := it.Dir
		byDir[dir] = append(byDir[dir], BoardItemDTO{
			Key: it.Kind + "|" + it.Title, Kind: it.Kind,
			Title: it.Title, Detail: it.Detail, Dir: it.Dir, Task: it.Task, URL: it.URL,
		})
	}

	// Lane set = the known project dirs (so a clean project still shows), unioned
	// with any feed-item dirs (github items may have no local dir).
	dirs := b.projectDirs()
	seen := map[string]bool{}
	lanes := make([]BoardLaneDTO, 0, len(dirs))
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		lanes = append(lanes, buildLane(dir, byDir[dir]))
	}
	for _, d := range dirs {
		add(d)
	}
	for d := range byDir {
		add(d)
	}

	// Order LOCAL lanes: most actionable first (items desc), then dirty, then name.
	sort.Slice(lanes, func(i, j int) bool {
		li, lj := lanes[i], lanes[j]
		ai, aj := len(li.Items), len(lj.Items)
		if ai != aj {
			return ai > aj
		}
		if li.Dirty != lj.Dirty {
			return li.Dirty > lj.Dirty
		}
		return li.Name < lj.Name
	})

	// Append REMOTE GitHub repo lanes (owned + org repos, e.g. agent-sh), so the
	// board spans projects without a local checkout. Skip a repo whose name
	// already has a local lane (the local one is richer). gh-cached + background-
	// refreshed, so this stays instant.
	if ghLanes, ok := githubBoardLanes(); ok {
		localNames := map[string]bool{}
		for _, l := range lanes {
			localNames[strings.ToLower(l.Name)] = true
		}
		for _, r := range ghLanes {
			name := r.NameWithOwner
			if i := strings.LastIndexByte(name, '/'); i >= 0 {
				name = name[i+1:]
			}
			if localNames[strings.ToLower(name)] {
				continue // already shown as a local lane (the local one is richer)
			}
			items := make([]BoardItemDTO, 0, len(r.Items))
			for _, w := range r.Items {
				kindLabel := "issue"
				if w.IsPR {
					kindLabel = "PR"
				}
				items = append(items, BoardItemDTO{
					Key:    r.NameWithOwner + "#" + itoa(w.Number),
					Kind:   "github",
					Title:  w.Title,
					Detail: kindLabel + " #" + itoa(w.Number),
					URL:    w.URL,
				})
			}
			lanes = append(lanes, BoardLaneDTO{
				Name:    name,
				Remote:  true,
				Repo:    r.NameWithOwner,
				URL:     r.URL,
				OpenPRs: r.OpenPRs,
				OpenIss: r.OpenIssues,
				Items:   items,
			})
		}
	}

	out := &BoardDTO{Lanes: lanes}
	if !f.Scanned.IsZero() {
		out.Scanned = f.Scanned.Format(time.RFC3339)
	}
	return out, nil
}

// buildLane assembles one project lane: feed items + local git/TODO context.
func buildLane(dir string, items []BoardItemDTO) BoardLaneDTO {
	lane := BoardLaneDTO{
		Name:  filepath.Base(strings.TrimRight(dir, "/")),
		Dir:   dir,
		Items: items,
	}
	if items == nil {
		lane.Items = []BoardItemDTO{}
	}
	// Count PR/issue items for the lane header.
	for _, it := range items {
		if it.Kind == "github" {
			if strings.Contains(strings.ToLower(it.Title), "pull request") || strings.Contains(strings.ToLower(it.Title), "pr ") {
				lane.OpenPRs++
			} else {
				lane.OpenIss++
			}
		}
	}
	// Local git context (skips quickly when dir isn't a repo / is gone).
	if branch, ok := gitBranch(dir); ok {
		lane.Branch = branch
		lane.Dirty = countDirty(dir)
		lane.Unpushed = countRevs(dir, "@{u}..HEAD")
		lane.Behind = countRevs(dir, "HEAD..@{u}")
		lane.Todos = countTodos(dir)
	}
	return lane
}

const boardGitTimeout = 3 * time.Second

func gitRun(dir string, args ...string) (string, bool) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), boardGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// gitBranch returns the current branch name (or a short SHA when detached); ok
// is false when dir isn't a git work tree.
func gitBranch(dir string) (string, bool) {
	if out, ok := gitRun(dir, "rev-parse", "--is-inside-work-tree"); !ok || out != "true" {
		return "", false
	}
	if b, ok := gitRun(dir, "rev-parse", "--abbrev-ref", "HEAD"); ok {
		if b == "HEAD" { // detached — show the short SHA instead
			if sha, ok := gitRun(dir, "rev-parse", "--short", "HEAD"); ok {
				return "@" + sha, true
			}
		}
		return b, true
	}
	return "", true
}

func countDirty(dir string) int {
	out, ok := gitRun(dir, "status", "--porcelain")
	if !ok || out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

func countRevs(dir, rng string) int {
	out, ok := gitRun(dir, "rev-list", "--count", rng)
	if !ok {
		return 0
	}
	n := 0
	for _, r := range out {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// maxTodoScan bounds the TODO grep so a huge repo stays fast.
const maxTodoScan = 2000

// countTodos counts TODO/FIXME markers in tracked files (via git grep, so it
// honors .gitignore and skips vendored junk). Capped; 0 when not a repo.
func countTodos(dir string) int {
	// git grep -I (skip binary) -E pattern; -c per-file counts, summed.
	out, ok := gitRun(dir, "grep", "-I", "-E", "-c", "(TODO|FIXME)")
	if !ok || out == "" {
		return 0
	}
	total := 0
	for _, line := range strings.Split(out, "\n") {
		// format: path:count
		if i := strings.LastIndexByte(line, ':'); i >= 0 {
			n := 0
			for _, r := range line[i+1:] {
				if r >= '0' && r <= '9' {
					n = n*10 + int(r-'0')
				}
			}
			total += n
			if total >= maxTodoScan {
				return maxTodoScan
			}
		}
	}
	return total
}
