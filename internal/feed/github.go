package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ghTimeout bounds each gh call (network).
const ghTimeout = 8 * time.Second

// maxGHItems caps GitHub suggestions per category.
const maxGHItems = 4

// scanGitHub asks `gh` for the user's actionable GitHub state: review
// requests (the world is waiting on you) and assigned open issues. Quietly
// returns nothing when gh is absent or unauthenticated.
func scanGitHub() []Item {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}
	var items []Item
	items = append(items, ghSearch(
		"prs", []string{"--review-requested", "@me", "--state", "open"},
		"review requested",
		"Review this pull request: %s (%s). Use `gh pr view %d --repo %s` and `gh pr diff %d --repo %s` "+
			"to read it, then give me a critique: real issues first, style nits last, and a clear "+
			"approve/request-changes recommendation.")...)
	items = append(items, ghSearch(
		"issues", []string{"--assignee", "@me", "--state", "open"},
		"assigned issue",
		"Work on this GitHub issue assigned to me: %s (%s). Read it with `gh issue view %d --repo %s` "+
			"and skim the discussion with `gh issue view %d --repo %s --comments`. Investigate the codebase "+
			"if it's checked out locally, propose a plan, and start on it after I confirm.")...)
	return items
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
// templated with (title, repo, number, repo, number, repo).
func ghSearch(what string, args []string, label, taskFmt string) []Item {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	full := append([]string{"search", what},
		append(args, "--json", "number,title,url,repository", "--limit", fmt.Sprint(maxGHItems))...)
	out, err := exec.CommandContext(ctx, "gh", full...).Output()
	if err != nil {
		return nil
	}
	var results []ghResult
	if json.Unmarshal(out, &results) != nil {
		return nil
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
	return items
}
