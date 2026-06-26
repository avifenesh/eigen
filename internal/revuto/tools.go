package revuto

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/tool"
)

// Tools returns the revuto reviewer tools (niche-grouped "revuto"). They no-op
// with a clear error when the CLI is absent, so registration is always safe.
func Tools() []tool.Definition {
	const group = "revuto"
	const gist = "the user's revuto AI PR-reviewer daemon — list reviewers, trigger review/learn/decay, pause/resume"
	return []tool.Definition{
		{
			Name:        "revuto_list",
			Description: "List the repos registered with the user's revuto PR-reviewer, with their paused/schedule state.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			ReadOnly:    true,
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "reviewers", CapabilityDesc: "inspect + control revuto reviewers",
			Run: func(ctx context.Context, _ json.RawMessage) (string, error) { return runList(ctx) },
		},
		{
			Name:        "revuto_trigger",
			Description: "Run a revuto job NOW for a repo. Args: {\"repo\":\"owner/name\",\"job\":\"review\"} — job is review|learn|decay (default review). Slow (runs a model). Returns the outcome JSON.",
			Parameters:  json.RawMessage(`{"type":"object","required":["repo"],"properties":{"repo":{"type":"string"},"job":{"type":"string","enum":["review","learn","decay"]}}}`),
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "reviewers", CapabilityDesc: "inspect + control revuto reviewers",
			Run: func(ctx context.Context, args json.RawMessage) (string, error) { return runTrigger(ctx, args) },
		},
		{
			Name:        "revuto_pause",
			Description: "Pause or resume revuto scheduling for a repo. Args: {\"repo\":\"owner/name\",\"resume\":false} — resume:true re-enables.",
			Parameters:  json.RawMessage(`{"type":"object","required":["repo"],"properties":{"repo":{"type":"string"},"resume":{"type":"boolean"}}}`),
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "reviewers", CapabilityDesc: "inspect + control revuto reviewers",
			Run: func(ctx context.Context, args json.RawMessage) (string, error) { return runPause(ctx, args) },
		},
	}
}

func runList(ctx context.Context) (string, error) {
	rs, err := List(ctx)
	if err != nil {
		return "", err
	}
	if len(rs) == 0 {
		return "No repos registered with revuto.", nil
	}
	var b strings.Builder
	for _, r := range rs {
		state := "active"
		if r.Paused {
			state = "PAUSED"
		}
		fmt.Fprintf(&b, "- %s (%s)\n", r.Repo, state)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func runTrigger(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Repo string `json:"repo"`
		Job  string `json:"job"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	out, err := Trigger(ctx, in.Repo, in.Job)
	if err != nil {
		return "", err
	}
	if out == "" {
		out = "(no output)"
	}
	return out, nil
}

func runPause(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Repo   string `json:"repo"`
		Resume bool   `json:"resume"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if in.Resume {
		if err := Resume(ctx, in.Repo); err != nil {
			return "", err
		}
		return "resumed " + in.Repo, nil
	}
	if err := Pause(ctx, in.Repo); err != nil {
		return "", err
	}
	return "paused " + in.Repo, nil
}

// Status is the connector card view for the GUI.
type Status struct {
	Available bool `json:"available"` // the revuto CLI is installed
	Count     int  `json:"count"`     // registered reviewers (best-effort)
	Paused    int  `json:"paused"`
}

// CurrentStatus reports CLI availability + reviewer counts (best-effort; counts
// are 0 when the CLI/list fails).
func CurrentStatus(ctx context.Context) Status {
	st := Status{Available: Available()}
	if !st.Available {
		return st
	}
	if rs, err := List(ctx); err == nil {
		st.Count = len(rs)
		for _, r := range rs {
			if r.Paused {
				st.Paused++
			}
		}
	}
	return st
}
