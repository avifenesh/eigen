package gui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/config"
)

// GitHub source for the work board: open PRs + issues across the user's account
// and their orgs (e.g. agent-sh) become remote lanes, so the board spans repos
// that need attention without a local checkout. Uses `gh search` so the whole
// sweep is TWO calls (all PRs, all issues across every owner) rather than N×2
// per-repo calls — fast enough to refresh in the background and cache.

const (
	ghBoardTimeout  = 15 * time.Second
	ghBoardCacheTTL = 5 * time.Minute
	ghBoardLimit    = 100 // max PRs/issues pulled per search
)

// ghRepoLane is a GitHub repo as a board lane (remote — no local clone). Carries
// the actionable PRs/issues so the board shows real work, not just a count.
type ghRepoLane struct {
	NameWithOwner string
	URL           string
	OpenPRs       int
	OpenIssues    int
	Items         []ghWorkItem
	Latest        time.Time // most recent activity across its items (for ordering)
}

// ghWorkItem is one open PR or issue.
type ghWorkItem struct {
	IsPR    bool
	Number  int
	Title   string
	URL     string
	Updated time.Time
}

var (
	ghBoardMu    sync.Mutex
	ghBoardCache []ghRepoLane
	ghBoardAt    time.Time
	ghBoardInFlt bool
)

// githubBoardLanes returns the GitHub repo lanes from a short cache, kicking a
// background refresh on a miss so Board() stays instant. ok=false when gh is
// unavailable (board then shows only local lanes).
func githubBoardLanes() ([]ghRepoLane, bool) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, false
	}
	ghBoardMu.Lock()
	fresh := !ghBoardAt.IsZero() && time.Since(ghBoardAt) < ghBoardCacheTTL
	cached := ghBoardCache
	if !fresh && !ghBoardInFlt {
		ghBoardInFlt = true
		go refreshGitHubBoard()
	}
	ghBoardMu.Unlock()
	return cached, true
}

func refreshGitHubBoard() {
	defer func() {
		ghBoardMu.Lock()
		ghBoardInFlt = false
		ghBoardMu.Unlock()
	}()
	lanes := buildGitHubLanes(context.Background())
	ghBoardMu.Lock()
	ghBoardCache = lanes
	ghBoardAt = time.Now()
	ghBoardMu.Unlock()
}

// boardOwners resolves which GitHub owners to scan: env → config → auto-detect
// (the gh user + their orgs).
func boardOwners(ctx context.Context) []string {
	if env := strings.TrimSpace(os.Getenv("EIGEN_BOARD_GH_OWNERS")); env != "" {
		return splitOwners(env)
	}
	if cfg := config.Load(); len(cfg.BoardGitHubOwners) > 0 {
		return cfg.BoardGitHubOwners
	}
	var owners []string
	if u := ghOut(ctx, "api", "user", "--jq", ".login"); u != "" {
		owners = append(owners, u)
	}
	if out := ghOut(ctx, "api", "user/orgs", "--jq", ".[].login"); out != "" {
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				owners = append(owners, l)
			}
		}
	}
	return owners
}

func splitOwners(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// buildGitHubLanes searches open PRs + issues across all owners (2 calls) and
// groups them into per-repo lanes. Only repos with open work appear — the board
// surfaces what needs attention, not every repo.
func buildGitHubLanes(ctx context.Context) []ghRepoLane {
	owners := boardOwners(ctx)
	if len(owners) == 0 {
		return nil
	}
	ownerArgs := make([]string, 0, len(owners)*2)
	for _, o := range owners {
		ownerArgs = append(ownerArgs, "--owner", o)
	}

	byRepo := map[string]*ghRepoLane{}
	lane := func(repo string) *ghRepoLane {
		l := byRepo[repo]
		if l == nil {
			l = &ghRepoLane{NameWithOwner: repo, URL: "https://github.com/" + repo}
			byRepo[repo] = l
		}
		return l
	}

	for _, w := range ghSearchWork(ctx, "prs", ownerArgs) {
		l := lane(w.repo)
		l.OpenPRs++
		l.Items = append(l.Items, ghWorkItem{IsPR: true, Number: w.number, Title: w.title, URL: w.url, Updated: w.updated})
		if w.updated.After(l.Latest) {
			l.Latest = w.updated
		}
	}
	for _, w := range ghSearchWork(ctx, "issues", ownerArgs) {
		l := lane(w.repo)
		l.OpenIssues++
		l.Items = append(l.Items, ghWorkItem{IsPR: false, Number: w.number, Title: w.title, URL: w.url, Updated: w.updated})
		if w.updated.After(l.Latest) {
			l.Latest = w.updated
		}
	}

	lanes := make([]ghRepoLane, 0, len(byRepo))
	for _, l := range byRepo {
		// Newest activity first within a lane.
		sort.Slice(l.Items, func(i, j int) bool { return l.Items[i].Updated.After(l.Items[j].Updated) })
		lanes = append(lanes, *l)
	}
	sort.Slice(lanes, func(i, j int) bool { return lanes[i].Latest.After(lanes[j].Latest) })
	return lanes
}

type ghSearchRow struct {
	repo    string
	number  int
	title   string
	url     string
	updated time.Time
}

// ghSearchWork runs one `gh search <prs|issues>` across all owners, open state.
func ghSearchWork(ctx context.Context, what string, ownerArgs []string) []ghSearchRow {
	args := append([]string{"search", what}, ownerArgs...)
	args = append(args, "--state", "open", "--limit", itoa(ghBoardLimit),
		"--json", "repository,number,title,url,updatedAt")
	out := ghOut(ctx, args...)
	if out == "" {
		return nil
	}
	var raw []struct {
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Number    int    `json:"number"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		UpdatedAt string `json:"updatedAt"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return nil
	}
	rows := make([]ghSearchRow, 0, len(raw))
	for _, r := range raw {
		t, _ := time.Parse(time.RFC3339, r.UpdatedAt)
		rows = append(rows, ghSearchRow{repo: r.Repository.NameWithOwner, number: r.Number, title: r.Title, url: r.URL, updated: t})
	}
	return rows
}

func ghOut(ctx context.Context, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, ghBoardTimeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, "gh", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
