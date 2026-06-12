package feed

// The "suggest" source: a small model looks at light project context (recent
// commits, working-tree state, memory tails) and proposes what the user
// probably wants or missed — not just mirrors of raw state (git/github/memory
// cover those) but the step FORWARD: "you fixed the bug but never added the
// regression test", "this branch looks finished, open the PR". Suggestions are
// offers; their tasks instruct the session to take the first concrete step.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/memory"
)

// Suggester runs one small-model completion (injected by the app so feed does
// not depend on a provider; nil disables the source). system carries the
// instructions, prompt the data — separated because agentic-tuned small models
// follow a system contract far more reliably than inline prose.
type Suggester func(ctx context.Context, system, prompt string) (string, error)

// maxSuggestItems caps model suggestions per scan.
const maxSuggestItems = 3

// suggestTimeout bounds the model call: the feed scan runs in the background,
// but it should never hang the refresh cycle.
const suggestTimeout = 30 * time.Second

// scanSuggest gathers cheap per-project context and asks the small model for
// up to maxSuggestItems concrete next actions. Failure-isolated: any error or
// unparseable output yields nothing.
func scanSuggest(dirs []string, s Suggester) []Item {
	if s == nil {
		return nil
	}
	ctxt := suggestContext(dirs)
	if ctxt == "" {
		return nil
	}
	system := `You are a JSON-only suggestion engine inside a developer dashboard. You receive a snapshot of the user's projects (recent commits, working-tree state, notes). Propose up to 3 genuinely helpful next actions the user probably wants or missed — follow-through they forgot (tests after a fix, docs after a feature, a PR for a finished branch, cleanup they postponed), NOT restatements of the raw state (uncommitted/unpushed counts are already shown elsewhere).

Your ENTIRE reply must be a single JSON array, no prose, no code fences, no explanation. Each element:
{"title":"<≤60 chars, start with the project name>","detail":"<≤70 chars, why this matters>","dir":"<the project dir exactly as given>","task":"<instructions for an agent session: investigate briefly, then TAKE the first concrete step (write the test, draft the PR description, make the edit) rather than only asking questions; stop and ask only where a decision is genuinely the user's>"}

If nothing is worth suggesting, reply [].`
	ctx, cancel := context.WithTimeout(context.Background(), suggestTimeout)
	defer cancel()
	out, err := s(ctx, system, ctxt)
	if err != nil {
		return nil
	}
	return parseSuggestions(out, dirs)
}

// parseSuggestions extracts the JSON array from the model reply (leniently:
// first '[' to last ']') and validates each entry.
func parseSuggestions(out string, dirs []string) []Item {
	start := strings.Index(out, "[")
	end := strings.LastIndex(out, "]")
	if start < 0 || end <= start {
		return nil
	}
	var raw []struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Dir    string `json:"dir"`
		Task   string `json:"task"`
	}
	if json.Unmarshal([]byte(out[start:end+1]), &raw) != nil {
		return nil
	}
	known := map[string]bool{}
	for _, d := range dirs {
		known[d] = true
	}
	var items []Item
	for _, r := range raw {
		if len(items) >= maxSuggestItems {
			break
		}
		if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Task) == "" {
			continue
		}
		dir := r.Dir
		if !known[dir] {
			dir = "" // never trust a hallucinated path; "" roots at CWD
		}
		items = append(items, Item{
			Kind:   "suggest",
			Title:  clip(r.Title, 70),
			Detail: clip(r.Detail, 70),
			Dir:    dir,
			Task:   clip(r.Task, 2000),
		})
	}
	return items
}

// suggestContext builds the bounded context snapshot the model reasons over:
// per project — branch, working-tree summary, recent commit subjects, and the
// tail of project memory. Cheap, local, read-only.
func suggestContext(dirs []string) string {
	var b strings.Builder
	n := 0
	for _, dir := range dirs {
		if n >= 6 {
			break
		}
		if !isGitRepo(dir) {
			continue
		}
		n++
		b.WriteString("## project " + filepath.Base(dir) + " (dir: " + dir + ")\n")
		if br := gitLine(dir, "rev-parse", "--abbrev-ref", "HEAD"); br != "" {
			b.WriteString("branch: " + br + "\n")
		}
		if d := dirtyFiles(dir); d > 0 {
			b.WriteString("working tree: " + clip(gitOut(dir, "status", "--short"), 400) + "\n")
		}
		if log := gitOut(dir, "log", "--oneline", "-5"); log != "" {
			b.WriteString("recent commits:\n" + clip(log, 500) + "\n")
		}
		if mem, err := memory.Open(dir); err == nil {
			if notes := strings.TrimSpace(mem.Read()); notes != "" {
				bullets := splitBullets(notes)
				from := len(bullets) - 2
				if from < 0 {
					from = 0
				}
				b.WriteString("latest notes:\n")
				for _, bl := range bullets[from:] {
					b.WriteString(clip(bl, 300) + "\n")
				}
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// gitLine runs git and returns the first output line ("" on error).
func gitLine(dir string, args ...string) string {
	return strings.SplitN(gitOut(dir, args...), "\n", 2)[0]
}

// gitOut runs git in dir and returns trimmed stdout ("" on error).
func gitOut(dir string, args ...string) string {
	out, err := gitIn(dir, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
