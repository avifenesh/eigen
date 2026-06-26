package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// ghTimeout bounds each gh call (network).
const ghTimeout = 8 * time.Second

// maxGHItems caps GitHub suggestions per category.
const maxGHItems = 4

var ghCommandCount atomic.Int64

// errGHAuth marks a gh failure that looks like the user being unauthenticated
// (gh installed but not logged in), as distinct from a genuine search error.
var errGHAuth = errors.New("gh not authenticated")

// scanGitHub asks `gh` for the user's actionable GitHub state: review
// requests (the world is waiting on you) and assigned open issues. Quietly
// returns nothing when gh is genuinely absent; when gh is present but the
// user isn't logged in, surfaces a single low-priority item nudging them to
// run `gh auth login` (otherwise the GitHub feed would just sit empty with no
// explanation). Other transient search errors stay silent.
func scanGitHub(ctx context.Context) []Item {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}
	var items []Item
	prs, err := ghSearch(ctx,
		"prs", []string{"--review-requested", "@me", "--state", "open"},
		"review requested",
		"Review this pull request: %s (%s). Use `gh pr view %d --repo %s` and `gh pr diff %d --repo %s` "+
			"to read it, then give me a critique: real issues first, style nits last, and a clear "+
			"approve/request-changes recommendation.")
	if errors.Is(err, errGHAuth) {
		return []Item{ghAuthItem()}
	}
	items = append(items, prs...)
	issues, err := ghSearch(ctx,
		"issues", []string{"--assignee", "@me", "--state", "open"},
		"assigned issue",
		"Work on this GitHub issue assigned to me: %s (%s). Read it with `gh issue view %d --repo %s` "+
			"and skim the discussion with `gh issue view %d --repo %s --comments`. Investigate the codebase "+
			"if it's checked out locally, propose a plan, and start on it after I confirm.")
	if errors.Is(err, errGHAuth) {
		return []Item{ghAuthItem()}
	}
	items = append(items, issues...)
	// Your own OPEN PRs across every org you belong to (incl. agent-sh) — work
	// in flight you authored, so you can jump back to address review/CI. Spans
	// all orgs because `--author @me` isn't scoped to one. Lower priority than
	// review-requested/assigned (those are blocking others), so it comes last.
	mine, err := ghSearch(ctx,
		"prs", []string{"--author", "@me", "--state", "open"},
		"your open PR",
		"This is your own open pull request: %s (%s). Check its review + CI status with "+
			"`gh pr view %d --repo %s` and `gh pr checks %d --repo %s`; if something needs addressing, "+
			"summarize it and propose the next step.")
	if errors.Is(err, errGHAuth) {
		return []Item{ghAuthItem()}
	}
	items = append(items, mine...)
	return items
}

// ghAuthItem is the single nudge shown when gh is installed but unauthenticated.
func ghAuthItem() Item {
	return Item{
		Kind:   "github",
		Title:  "GitHub feed needs sign-in",
		Detail: "gh is installed but not authenticated",
		Task: "Run `gh auth login` in a terminal to authenticate the GitHub CLI, then come back — " +
			"your review requests and assigned issues will show up here.",
	}
}

// ghResult is the JSON shape gh search returns for the fields we ask for.
type ghResult struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// ghSearch runs one gh search and maps results to feed items. taskFmt is
// templated with (title, repo, number, repo, number, repo). It returns
// errGHAuth when the failure looks like an unauthenticated gh (so the caller
// can nudge the user) and a plain error for any other failure.
func ghSearch(parent context.Context, what string, args []string, label, taskFmt string) ([]Item, error) {
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, ghTimeout)
	defer cancel()
	full := append([]string{"search", what},
		append(args, "--json", "number,title,url,repository", "--limit", fmt.Sprint(maxGHItems))...)
	ghCommandCount.Add(1)
	out, err := exec.CommandContext(ctx, "gh", full...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && isGHAuthError(ee.Stderr) {
			return nil, errGHAuth
		}
		return nil, err
	}
	var results []ghResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}
	var items []Item
	for _, r := range results {
		repo := r.Repository.NameWithOwner
		items = append(items, Item{
			Kind:   "github",
			Title:  fmt.Sprintf("%s: %s", label, clip(r.Title, 60)),
			Detail: fmt.Sprintf("%s#%d", repo, r.Number),
			URL:    r.URL,
			Task:   fmt.Sprintf(taskFmt, r.Title, repo, r.Number, repo, r.Number, repo),
		})
	}
	return items, nil
}

// ghAuthMarkers are substrings gh writes to stderr when the user is not
// logged in. gh phrases this a few ways across versions, so we match any of
// them (case-insensitively) and treat anything else as a transient/other
// failure to stay silent on.
var ghAuthMarkers = []string{
	"gh auth login",
	"not logged in",
	"authentication required",
	"requires authentication",
}

// isGHAuthError reports whether gh's stderr indicates the user is not logged
// in, as opposed to some other failure.
func isGHAuthError(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	for _, marker := range ghAuthMarkers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
