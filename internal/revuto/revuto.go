// Package revuto is eigen's native integration with the user's `revuto` AI
// PR-reviewer daemon: list registered reviewers, see per-repo state, and trigger
// review/learn/decay or pause/resume — by shelling the `revuto` CLI (which emits
// JSON). A local CLI-backed built-in (like internal/google is REST-backed and
// internal/obsidian is FS-backed); surfaced as a connector card + agent tools.
package revuto

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// cliTimeout bounds a revuto CLI call. `list`/`pause`/`resume` are fast; a
// `trigger` review can be slow (it runs a model), so the caller passes a longer
// ctx for those.
const cliTimeout = 20 * time.Second

// Reviewer is one registered repo in revuto (mirrors `revuto list --json`).
type Reviewer struct {
	Repo           string            `json:"repo"`
	Paused         bool              `json:"paused"`
	AutoActivate   bool              `json:"autoActivate"`
	BotLogin       string            `json:"botLogin,omitempty"`
	Schedules      map[string]string `json:"schedules,omitempty"`
	AuthorAllowlist []string         `json:"authorAllowlist,omitempty"`
}

// Available reports whether the `revuto` CLI is installed.
func Available() bool {
	_, err := exec.LookPath("revuto")
	return err == nil
}

// List returns the registered reviewers via `revuto list --json`.
func List(ctx context.Context) ([]Reviewer, error) {
	out, err := run(ctx, cliTimeout, "list", "--json")
	if err != nil {
		return nil, err
	}
	var rs []Reviewer
	if err := json.Unmarshal([]byte(out), &rs); err != nil {
		return nil, err
	}
	return rs, nil
}

// Trigger runs a job (review|learn|decay; default review) for a repo NOW. Slow
// (runs a model) — give it a generous ctx. Returns the CLI's JSON outcome text.
func Trigger(ctx context.Context, repo, job string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || !strings.Contains(repo, "/") {
		return "", errBadRepo
	}
	args := []string{"trigger", repo}
	if j := strings.TrimSpace(job); j != "" {
		args = append(args, j)
	}
	// review/learn/decay can take minutes; use a long timeout.
	return run(ctx, 10*time.Minute, args...)
}

// Pause / Resume toggle scheduling for a repo.
func Pause(ctx context.Context, repo string) error  { return ctlRepo(ctx, "pause", repo) }
func Resume(ctx context.Context, repo string) error { return ctlRepo(ctx, "resume", repo) }

func ctlRepo(ctx context.Context, verb, repo string) error {
	repo = strings.TrimSpace(repo)
	if repo == "" || !strings.Contains(repo, "/") {
		return errBadRepo
	}
	_, err := run(ctx, cliTimeout, verb, repo)
	return err
}

// run executes the revuto CLI with a timeout, returning trimmed stdout.
func run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, "revuto", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// errBadRepo is returned for a malformed owner/repo argument.
var errBadRepo = revutoError("repo must be owner/name")

type revutoError string

func (e revutoError) Error() string { return "revuto: " + string(e) }
