package feed

// The "suggest" source: a mid-tier model looks at light project context
// (recent commits, working-tree state, README, memory tails) and proposes what
// the user probably wants or missed — not mirrors of raw state (git/github/
// memory cover those) but the step FORWARD: the missing regression test, the
// finished branch that needs a PR, the feature worth prototyping next, the
// workflow inefficiency worth a quick session. Suggestions are offers — easy
// to clear — so bold beats timid. They run on their OWN cadence (suggestTTL),
// slower than the cheap scanners: the model call is the expensive part, and
// fresh-every-10-minutes adds nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
// but it should never hang the refresh cycle. Mid-tier models think longer
// than small ones.
const suggestTimeout = 90 * time.Second

// suggestTTL is the suggest source's own refresh cadence. The cheap scanners
// (git/memory/github) refresh every feed tick; the model call refreshes only
// when this expires — suggestions don't go stale in minutes, and the model
// (even an idle one) shouldn't be hit every 10 minutes for the same context.
const suggestTTL = 90 * time.Minute

// suggestCachePath stores the last model suggestions between scans.
func suggestCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "feed-suggest.json")
}

// suggestCache is the persisted suggest state.
type suggestCache struct {
	Items   []Item    `json:"items"`
	Scanned time.Time `json:"scanned"`
}

func loadSuggestCache() (suggestCache, bool) {
	var c suggestCache
	b, err := os.ReadFile(suggestCachePath())
	if err != nil || json.Unmarshal(b, &c) != nil {
		return suggestCache{}, false
	}
	return c, time.Since(c.Scanned) < suggestTTL
}

func saveSuggestCache(items []Item) {
	b, err := json.Marshal(suggestCache{Items: items, Scanned: time.Now()})
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(suggestCachePath()), 0o755)
	tmp := suggestCachePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, suggestCachePath())
	}
}

// scanSuggest returns model suggestions: the cached set while it's fresh
// (its own TTL, slower than the feed tick), a fresh model call when stale.
// Failure-isolated: an erroring model or unparseable output falls back to the
// stale cache (better yesterday's good ideas than none). It inherits the app
// lifetime context so closing the app cancels the in-flight model request.
func scanSuggest(parent context.Context, dirs []string, s Suggester) []Item {
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return nil
	}
	if s == nil {
		return nil
	}
	cached, fresh := loadSuggestCache()
	if fresh {
		return cached.Items
	}
	ctxt := suggestContext(dirs)
	if ctxt == "" {
		return cached.Items
	}
	system := `You are a JSON-only suggestion engine inside a developer's personal dashboard. You receive a snapshot of their projects (recent commits, working-tree state, README intro, notes). Propose up to 3 genuinely useful next actions. Be bold — these are offers the user can clear with one keystroke, so a sharp guess beats a safe restatement.

Good suggestions span:
- FOLLOW-THROUGH they forgot: the regression test after a fix, docs after a feature, the PR for a finished branch, finishing what's half-committed, postponed cleanup.
- THE NEXT FEATURE STEP: from the project's trajectory, propose the concrete next capability worth building — e.g. a rollout/feature-gate mechanism to trial a new model or behavior on a slice before fully releasing it, a missing benchmark before a perf claim, an A/B path for a risky change.
- WORK THE USER MUST DO: things only they can decide or unblock (a config to set, a credential to rotate, a choice between two designs) — surface it crisply.
- WORKFLOW IMPROVEMENTS: when the snapshot shows repeated friction (manual steps, flaky areas, missing automation), offer a quick focused session to fix the workflow itself.

NOT raw-state restatements (uncommitted/unpushed counts are already shown elsewhere). Never propose destructive actions (force-push, deletes, deploys).

Your ENTIRE reply must be a single JSON array, no prose, no code fences. Each element:
{"title":"<≤60 chars, start with the project name>","detail":"<≤70 chars, why this matters>","dir":"<the project dir exactly as given>","task":"<instructions for an agent session: investigate briefly, then TAKE the first concrete step (write the test, scaffold the feature gate, draft the PR/design) rather than only asking questions; stop and ask only where a decision is genuinely the user's>"}

If nothing is worth suggesting, reply [].`
	ctx, cancel := context.WithTimeout(parent, suggestTimeout)
	defer cancel()
	out, err := s(ctx, system, ctxt)
	if err != nil {
		return cached.Items // stale beats nothing
	}
	items := parseSuggestions(out, dirs)
	if items == nil {
		return cached.Items
	}
	// Drop ideas we've recently surfaced (this run or earlier) or that the user
	// dismissed, so the model can't re-propose the same thing scan after scan.
	items = dedupSuggestions(items)
	// If dedup emptied a batch the model actually produced, keep the prior cache
	// rather than flipping the feed to nothing.
	if len(items) == 0 && len(cached.Items) > 0 {
		return cached.Items
	}
	recordSeenSuggest(items)
	saveSuggestCache(items)
	return items
}

// dedupSuggestions drops suggestions whose key was recently surfaced or
// dismissed, so ideas don't repeat run over run.
func dedupSuggestions(items []Item) []Item {
	seen := loadSeenSuggest()
	dismissed := loadDismissed()
	if len(seen) == 0 && len(dismissed) == 0 {
		return items
	}
	out := items[:0:0]
	for _, it := range items {
		k := it.Key()
		if _, ok := seen[k]; ok {
			continue
		}
		if _, ok := dismissed[k]; ok {
			continue
		}
		out = append(out, it)
	}
	return out
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

// maxSuggestDirs caps how many projects feed the suggestion prompt. Wider than
// "just the last session" so ideas draw on recent cross-session activity, but
// still bounded — this is a single LLM prompt and each dir costs a few cheap
// git reads.
const maxSuggestDirs = 12

// suggestContext builds the bounded context snapshot the model reasons over:
// per project — branch, working-tree summary, recent commit subjects, the
// README's opening (what the project IS, so feature suggestions land), and
// the tail of project memory. Cheap, local, read-only.
//
// Projects are ordered by most-recent commit so the prompt spans the user's
// recently-active work rather than being anchored to one last session, then
// bounded to maxSuggestDirs. Safe when dirs are empty or none are git repos.
func suggestContext(dirs []string) string {
	dirs = orderByRecentActivity(dirs)
	var b strings.Builder
	n := 0
	for _, dir := range dirs {
		if n >= maxSuggestDirs {
			break
		}
		if !isGitRepo(dir) {
			continue
		}
		n++
		b.WriteString("## project " + filepath.Base(dir) + " (dir: " + dir + ")\n")
		if intro := readmeIntro(dir); intro != "" {
			b.WriteString("about: " + intro + "\n")
		}
		if br := gitLine(dir, "rev-parse", "--abbrev-ref", "HEAD"); br != "" {
			b.WriteString("branch: " + br + "\n")
		}
		if d := dirtyFiles(dir); d > 0 {
			b.WriteString("working tree: " + clip(gitOut(dir, "status", "--short"), 400) + "\n")
		}
		if log := gitOut(dir, "log", "--oneline", "-8"); log != "" {
			b.WriteString("recent commits:\n" + clip(log, 700) + "\n")
		}
		if mem, err := memory.Open(dir); err == nil {
			if notes := strings.TrimSpace(mem.Read()); notes != "" {
				bullets := splitBullets(notes)
				from := len(bullets) - 3
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

// orderByRecentActivity sorts dirs by their last commit time, most recent
// first, so the (bounded) context window favors the projects the user has
// actually touched lately across sessions. Non-repos and repos with no commits
// sort last (timestamp 0) but are kept — the caller skips non-repos anyway.
// Stable, so equal/zero timestamps preserve the caller's original order.
func orderByRecentActivity(dirs []string) []string {
	if len(dirs) < 2 {
		return dirs
	}
	out := append([]string(nil), dirs...)
	last := make(map[string]int64, len(out))
	for _, dir := range out {
		last[dir] = lastCommitUnix(dir)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return last[out[i]] > last[out[j]]
	})
	return out
}

// lastCommitUnix returns the Unix time of HEAD's commit (0 on any error, e.g.
// not a repo or no commits yet). One cheap local git read.
func lastCommitUnix(dir string) int64 {
	out := gitOut(dir, "log", "-1", "--format=%ct")
	var t int64
	fmt.Sscanf(out, "%d", &t)
	return t
}

// readmeIntro returns the first descriptive lines of the project README —
// enough for the model to know what the project IS without reading code.
func readmeIntro(dir string) string {
	for _, name := range []string{"README.md", "README", "readme.md"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var lines []string
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(strings.TrimLeft(ln, "# "))
			if ln == "" || strings.HasPrefix(ln, "[![") || strings.HasPrefix(ln, "<!--") {
				continue
			}
			lines = append(lines, ln)
			if len(lines) >= 3 {
				break
			}
		}
		return clip(strings.Join(lines, " · "), 280)
	}
	return ""
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
