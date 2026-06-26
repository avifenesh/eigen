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
	ghBoardLimit    = 100 // max open PRs/issues pulled per search
	ghDoneLimit     = 30  // max recently-merged PRs for the Done column
)

func (l *ghRepoLane) bumpLatest(t time.Time) {
	if t.After(l.Latest) {
		l.Latest = t
	}
}

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

// ghWorkItem is one PR or issue with the signals the kanban derivation needs.
type ghWorkItem struct {
	IsPR    bool
	Number  int
	Title   string
	URL     string
	Updated time.Time
	// PR signals (zero/"" for issues).
	Draft          bool   // draft PR
	ReviewDecision string // APPROVED | CHANGES_REQUESTED | REVIEW_REQUIRED | ""
	State          string // OPEN | MERGED | CLOSED (lowercased varies; normalized upper)
	// Whether the current user is requested as a reviewer or assigned — drives
	// the "Needs you" column. Derived from the feed's @me classification, joined
	// by URL (see board.go), not fetched here.
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

	// Open PRs (with review/draft signals), open issues, and recently
	// merged/closed PRs (for the Done column). Each is one gh-search call across
	// all owners — bounded total.
	for _, w := range ghSearchWork(ctx, "prs", append(ownerArgs, "--state", "open")) {
		l := lane(w.repo)
		l.OpenPRs++
		l.Items = append(l.Items, w.item(true, "OPEN"))
		l.bumpLatest(w.updated)
	}
	for _, w := range ghSearchWork(ctx, "issues", append(ownerArgs, "--state", "open")) {
		l := lane(w.repo)
		l.OpenIssues++
		l.Items = append(l.Items, w.item(false, "OPEN"))
		l.bumpLatest(w.updated)
	}
	// Done: recently merged PRs (capped by the search limit + updated recency).
	for _, w := range ghSearchWork(ctx, "prs", append(ownerArgs, "--merged", "--limit", itoa(ghDoneLimit))) {
		l := lane(w.repo)
		it := w.item(true, "MERGED")
		l.Items = append(l.Items, it)
		l.bumpLatest(w.updated)
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
	repo           string
	number         int
	title          string
	url            string
	updated        time.Time
	draft          bool
	reviewDecision string
}

// item builds a ghWorkItem from a search row, stamping PR-ness + state.
func (r ghSearchRow) item(isPR bool, state string) ghWorkItem {
	return ghWorkItem{
		IsPR: isPR, Number: r.number, Title: r.title, URL: r.url, Updated: r.updated,
		Draft: r.draft, ReviewDecision: r.reviewDecision, State: state,
	}
}

// ghSearchWork runs one `gh search <prs|issues>` across all owners. The caller
// supplies state/limit args. PRs carry draft + reviewDecision; issues don't
// (gh rejects those json fields for issue searches), so the field set differs.
func ghSearchWork(ctx context.Context, what string, extraArgs []string) []ghSearchRow {
	jsonFields := "repository,number,title,url,updatedAt"
	if what == "prs" {
		jsonFields += ",isDraft,reviewDecision"
	}
	args := append([]string{"search", what}, extraArgs...)
	// Default a limit when the caller didn't pass one (open searches do).
	if !hasFlag(extraArgs, "--limit") {
		args = append(args, "--limit", itoa(ghBoardLimit))
	}
	args = append(args, "--json", jsonFields)
	out := ghOut(ctx, args...)
	if out == "" {
		return nil
	}
	var raw []struct {
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Number         int    `json:"number"`
		Title          string `json:"title"`
		URL            string `json:"url"`
		UpdatedAt      string `json:"updatedAt"`
		IsDraft        bool   `json:"isDraft"`
		ReviewDecision string `json:"reviewDecision"`
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return nil
	}
	rows := make([]ghSearchRow, 0, len(raw))
	for _, r := range raw {
		t, _ := time.Parse(time.RFC3339, r.UpdatedAt)
		rows = append(rows, ghSearchRow{
			repo: r.Repository.NameWithOwner, number: r.Number, title: r.Title, url: r.URL,
			updated: t, draft: r.IsDraft, reviewDecision: r.ReviewDecision,
		})
	}
	return rows
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
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
