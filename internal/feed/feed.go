// Package feed implements the proactive action feed: background scanners
// over fast local sources (git state, project memory) and GitHub (assigned
// issues, review requests) that produce one-keystroke session starters —
// "solve this issue", "commit what you left dirty", "do the thing you said
// you'd do" — surfaced on the app's home and project pages.
//
// Design: scanning is cheap and read-only; every Item carries a ready-made
// task prompt and the project dir to root the session at. Results are cached
// (~/.eigen/feed.json) so the app renders instantly and refreshes behind.
package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Item is one offered action.
type Item struct {
	Kind   string `json:"kind"` // "git" | "github" | "memory"
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Dir    string `json:"dir,omitempty"` // project to root the session at
	Task   string `json:"task"`          // the prompt a new session starts with
	URL    string `json:"url,omitempty"`
}

// Feed is the cached scan result.
type Feed struct {
	Items   []Item    `json:"items"`
	Scanned time.Time `json:"scanned"`
}

// CachePath is where the feed cache lives.
func CachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "feed.json")
}

// cacheTTL is how long a cached feed is considered fresh.
const cacheTTL = 10 * time.Minute

// Load returns the cached feed (possibly stale; zero Feed when absent) and
// whether it is still fresh.
func Load() (Feed, bool) {
	var f Feed
	b, err := os.ReadFile(CachePath())
	if err != nil || json.Unmarshal(b, &f) != nil {
		return Feed{}, false
	}
	return f, time.Since(f.Scanned) < cacheTTL
}

// save writes the cache (best-effort).
func save(f Feed) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(CachePath()), 0o755)
	tmp := CachePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, CachePath())
	}
}

// Scan runs every scanner over the given project dirs and returns the fresh
// feed (also cached). Order: git (actionable now) → memory (your own intent)
// → github (the world's asks). Each scanner is bounded and failure-isolated.
func Scan(projectDirs []string) Feed {
	var items []Item
	items = append(items, scanGit(projectDirs)...)
	items = append(items, scanMemory(projectDirs)...)
	items = append(items, scanGitHub()...)
	f := Feed{Items: rank(items), Scanned: time.Now()}
	save(f)
	return f
}

// Key is a stable identity for dismissals: kind + title + dir. Content-based,
// so a dismissed item stays dismissed across rescans until it CHANGES (e.g.
// the dirty-file count moves) — at which point it's arguably news again.
func (it Item) Key() string {
	h := sha256.Sum256([]byte(it.Kind + "\x00" + it.Title + "\x00" + it.Dir))
	return hex.EncodeToString(h[:8])
}

// rank orders items by actionability: review requests (others are blocked on
// you) > assigned issues > your own uncommitted work > unpushed > memory
// intents. Stable within a score, preserving scan order.
func rank(items []Item) []Item {
	sort.SliceStable(items, func(i, j int) bool {
		return score(items[i]) > score(items[j])
	})
	return items
}

func score(it Item) int {
	switch it.Kind {
	case "github":
		if strings.HasPrefix(it.Title, "review requested") {
			return 90
		}
		return 70
	case "git":
		if strings.Contains(it.Title, "uncommitted") {
			return 60
		}
		return 50 // unpushed
	case "memory":
		return 40
	}
	return 0
}

// --- dismissals ---------------------------------------------------------

// dismissTTL is how long a dismissal holds. Long enough to stop nagging,
// short enough that a truly ignored loose end resurfaces eventually.
const dismissTTL = 14 * 24 * time.Hour

// dismissedPath is the dismissal store.
func dismissedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "feed-dismissed.json")
}

// loadDismissed returns the live (unexpired) dismissal set.
func loadDismissed() map[string]time.Time {
	out := map[string]time.Time{}
	b, err := os.ReadFile(dismissedPath())
	if err != nil {
		return out
	}
	var raw map[string]time.Time
	if json.Unmarshal(b, &raw) != nil {
		return out
	}
	for k, t := range raw {
		if time.Since(t) < dismissTTL {
			out[k] = t
		}
	}
	return out
}

// Dismiss records an item as dismissed (it stops appearing until its content
// changes or the dismissal expires).
func Dismiss(it Item) {
	d := loadDismissed()
	d[it.Key()] = time.Now()
	b, err := json.Marshal(d)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(dismissedPath()), 0o755)
	tmp := dismissedPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, dismissedPath())
	}
}

// FilterDismissed drops dismissed items (call at render time — the cache
// keeps everything so un-expiring works without a rescan).
func FilterDismissed(items []Item) []Item {
	d := loadDismissed()
	if len(d) == 0 {
		return items
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if _, dead := d[it.Key()]; !dead {
			out = append(out, it)
		}
	}
	return out
}

// Top selects up to limit items in rank order, capping each kind at perKind
// so one noisy source (e.g. a busy GitHub week) can't crowd the others off
// the home page. Skipped items backfill at the end if the limit isn't met.
func Top(items []Item, limit, perKind int) []Item {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	counts := map[string]int{}
	var out, overflow []Item
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		if counts[it.Kind] >= perKind {
			overflow = append(overflow, it)
			continue
		}
		counts[it.Kind]++
		out = append(out, it)
	}
	for _, it := range overflow {
		if len(out) >= limit {
			break
		}
		out = append(out, it)
	}
	return out
}
