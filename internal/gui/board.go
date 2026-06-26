package gui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/config"
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
	// Pinned: the user pinned this lane so it shows even when idle. Pin key is the
	// dir (local) or owner/name (remote).
	Pinned bool `json:"pinned"`
}

// laneKey is the stable pin key for a lane: its repo (remote) or dir (local).
func (l BoardLaneDTO) laneKey() string {
	if l.Remote {
		return l.Repo
	}
	return l.Dir
}

// ── Kanban: a cross-repo, DERIVED column board ──────────────────────────────
// Columns reflect git+GitHub reality (no manual status field). Each work item
// is assigned a column by deriving its status from the signals the board
// already gathers + a cheap feed/@me + active-session join.

// KanbanCardDTO is one item on the kanban (a PR, issue, or local git loose-end).
type KanbanCardDTO struct {
	Key    string `json:"key"`
	Repo   string `json:"repo"` // owner/name or local project name (the disambiguator)
	Title  string `json:"title"`
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
	Kind   string `json:"kind"` // "pr" | "issue" | "git"
	// Derived signals for badges.
	Review   string `json:"review,omitempty"` // "approved" | "changes" | "pending"
	Draft    bool   `json:"draft,omitempty"`
	NeedsYou bool   `json:"needsYou,omitempty"` // review-requested / changes-requested / assigned
	Session  bool   `json:"session,omitempty"`  // an eigen session is active on this repo
	AgeHours int    `json:"ageHours,omitempty"` // since last activity (reddens past ~48h)
	Task     string `json:"task,omitempty"`     // for local git cards (Start →)
	Dir      string `json:"dir,omitempty"`      // local project dir (git cards / session start)
}

// KanbanColumnDTO is one column with its ordered cards.
type KanbanColumnDTO struct {
	ID    string          `json:"id"` // needs-you | todo | in-progress | in-review | done
	Title string          `json:"title"`
	Cards []KanbanCardDTO `json:"cards"`
}

// KanbanDTO is the whole derived board.
type KanbanDTO struct {
	Columns []KanbanColumnDTO `json:"columns"`
}

// Kanban derives the cross-repo column board from the same data the lane board
// uses (cached GitHub work + local git + the feed's @me classification + active
// eigen sessions). Read-only: columns reflect reality; the user acts via card
// buttons and the card re-derives next refresh.
func (b *Bridge) Kanban() (*KanbanDTO, error) {
	// "Needs you" join: the feed already classified the user's review-requested /
	// assigned / own-PR items by URL — reuse it to flag attention without extra
	// gh calls.
	needsYou := map[string]bool{}
	f, _ := feed.Load()
	for _, it := range f.Items {
		if it.Kind == "github" && it.URL != "" {
			t := strings.ToLower(it.Title)
			if strings.HasPrefix(t, "review requested") || strings.HasPrefix(t, "assigned issue") {
				needsYou[it.URL] = true
			}
		}
	}

	// Active-session repos: a working/approval session rooted in a project dir →
	// that repo has an agent on it (badge + nudges In Progress).
	sessionDirs := map[string]bool{}  // dir → has an active session
	approvalDirs := map[string]bool{} // dir → session awaiting approval (Needs you)
	if sess, err := b.Sessions(); err == nil {
		for _, s := range sess {
			if s.Dir == "" {
				continue
			}
			if s.Status == "working" || s.Status == "approval" {
				sessionDirs[s.Dir] = true
			}
			if s.Status == "approval" {
				approvalDirs[s.Dir] = true
			}
		}
	}

	cols := map[string]*KanbanColumnDTO{
		"needs-you":   {ID: "needs-you", Title: "Needs you"},
		"todo":        {ID: "todo", Title: "Todo"},
		"in-progress": {ID: "in-progress", Title: "In progress"},
		"in-review":   {ID: "in-review", Title: "In review"},
		"done":        {ID: "done", Title: "Done"},
	}
	add := func(colID string, c KanbanCardDTO) { cols[colID].Cards = append(cols[colID].Cards, c) }

	now := time.Now()
	ageHours := func(t time.Time) int {
		if t.IsZero() {
			return 0
		}
		h := int(now.Sub(t).Hours())
		if h < 0 {
			h = 0
		}
		return h
	}

	// GitHub work items → cards. (Uses the cached GH lanes the board already has.)
	if ghLanes, ok := githubBoardLanes(); ok {
		for _, lane := range ghLanes {
			repoName := lane.NameWithOwner
			for _, w := range lane.Items {
				c := KanbanCardDTO{
					Key: repoName + "#" + itoa(w.Number), Repo: repoName, Title: w.Title,
					Number: w.Number, URL: w.URL, Draft: w.Draft, AgeHours: ageHours(w.Updated),
					NeedsYou: needsYou[w.URL],
				}
				if w.IsPR {
					c.Kind = "pr"
					switch w.ReviewDecision {
					case "APPROVED":
						c.Review = "approved"
					case "CHANGES_REQUESTED":
						c.Review = "changes"
					case "REVIEW_REQUIRED":
						c.Review = "pending"
					}
				} else {
					c.Kind = "issue"
				}
				add(kanbanColumnFor(w, c.NeedsYou), c)
			}
		}
	}

	// Local git loose-ends (uncommitted/unpushed/behind) → In Progress cards,
	// with the active-session badge. These are "code exists, not yet a PR".
	for _, lane := range b.localLanes() {
		if lane.Dirty == 0 && lane.Unpushed == 0 && lane.Behind == 0 {
			continue
		}
		hasSession := sessionDirs[lane.Dir]
		title := lane.Name + ": "
		switch {
		case lane.Dirty > 0:
			title += itoa(lane.Dirty) + " uncommitted"
		case lane.Unpushed > 0:
			title += itoa(lane.Unpushed) + " unpushed"
		default:
			title += itoa(lane.Behind) + " behind"
		}
		add("in-progress", KanbanCardDTO{
			Key: "git|" + lane.Dir, Repo: lane.Name, Title: title, Kind: "git",
			Dir: lane.Dir, Session: hasSession,
			Task: "Review the working tree (git status/diff) and commit coherent chunks, or push/integrate as needed.",
		})
	}

	// Tag GitHub cards whose repo has an active local session, and order each
	// column: needs-you first, then session, then oldest (aging) first.
	order := []string{"needs-you", "todo", "in-progress", "in-review", "done"}
	out := &KanbanDTO{}
	for _, id := range order {
		col := cols[id]
		sort.SliceStable(col.Cards, func(i, j int) bool {
			a, b2 := col.Cards[i], col.Cards[j]
			if a.NeedsYou != b2.NeedsYou {
				return a.NeedsYou
			}
			if a.Session != b2.Session {
				return a.Session
			}
			return a.AgeHours > b2.AgeHours // oldest first within a column
		})
		if col.Cards == nil {
			col.Cards = []KanbanCardDTO{}
		}
		out.Columns = append(out.Columns, *col)
	}
	return out, nil
}

// kanbanColumnFor derives a GitHub item's column (first match wins, terminal
// states dominate). needsYou short-circuits open work into the attention column.
func kanbanColumnFor(w ghWorkItem, needsYou bool) string {
	if w.State == "MERGED" || w.State == "CLOSED" {
		return "done"
	}
	if w.IsPR {
		if needsYou || w.ReviewDecision == "CHANGES_REQUESTED" {
			return "needs-you"
		}
		if w.Draft {
			return "in-progress"
		}
		return "in-review" // open non-draft PR
	}
	// Issue.
	if needsYou {
		return "needs-you"
	}
	return "todo"
}

// localLanes builds just the local (checkout) lanes with git context — the same
// per-dir probe the lane board does, factored so Kanban can reuse it.
func (b *Bridge) localLanes() []BoardLaneDTO {
	var lanes []BoardLaneDTO
	for _, dir := range b.projectDirs() {
		if dir == "" {
			continue
		}
		lanes = append(lanes, buildLane(dir, nil))
	}
	return lanes
}

// active reports whether a lane has anything worth showing on its own (open
// work or local changes). Idle lanes only appear when pinned.
func (l BoardLaneDTO) active() bool {
	return len(l.Items) > 0 || l.Dirty > 0 || l.Unpushed > 0 || l.Behind > 0 || l.OpenPRs > 0 || l.OpenIss > 0
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

	// Scope = pinned + active: mark pinned lanes, then drop idle lanes that
	// aren't pinned so the board stays "what needs attention" + a stable pinned
	// set. (config.BoardPinned keys: dir for local, owner/name for remote.)
	pinned := boardPinnedSet()
	kept := lanes[:0]
	for _, l := range lanes {
		l.Pinned = pinned[l.laneKey()]
		if l.active() || l.Pinned {
			kept = append(kept, l)
		}
	}
	lanes = kept

	out := &BoardDTO{Lanes: lanes}
	if !f.Scanned.IsZero() {
		out.Scanned = f.Scanned.Format(time.RFC3339)
	}
	return out, nil
}

// boardPinnedSet loads the pinned-lane keys as a set.
func boardPinnedSet() map[string]bool {
	cfg := config.Load()
	set := make(map[string]bool, len(cfg.BoardPinned))
	for _, k := range cfg.BoardPinned {
		set[k] = true
	}
	return set
}

// PinLane / UnpinLane toggle whether a lane stays on the board when idle. key is
// the lane's dir (local) or owner/name (remote). Persisted to config.
func (b *Bridge) PinLane(key string) error   { return setLanePinned(key, true) }
func (b *Bridge) UnpinLane(key string) error { return setLanePinned(key, false) }

func setLanePinned(key string, pinned bool) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty lane key")
	}
	cfg := config.Load()
	out := cfg.BoardPinned[:0]
	has := false
	for _, k := range cfg.BoardPinned {
		if k == key {
			has = true
			if !pinned {
				continue // drop it
			}
		}
		out = append(out, k)
	}
	if pinned && !has {
		out = append(out, key)
	}
	cfg.BoardPinned = out
	return config.Save(cfg)
}

// ReviewPR starts a session that reviews a specific GitHub PR by URL. The agent
// reads the PR + diff and gives a critique — the board's per-card "Review"
// action. Returns the new session id.
func (b *Bridge) ReviewPR(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("PR url required")
	}
	task := "Review this pull request: " + url + ". Use `gh pr view " + url + "` and `gh pr diff " + url +
		"` to read it, then give me a critique — real issues first, style nits last, and a clear " +
		"approve/request-changes recommendation."
	return b.StartFromFeed("", task)
}

// WorkIssue starts a session to work a specific GitHub issue by URL — the
// board's per-card "Start working" action on an issue.
func (b *Bridge) WorkIssue(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("issue url required")
	}
	task := "Work on this GitHub issue: " + url + ". Read it with `gh issue view " + url +
		" --comments`, investigate the codebase if it's checked out locally, propose a plan, and start on it after I confirm."
	return b.StartFromFeed("", task)
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
