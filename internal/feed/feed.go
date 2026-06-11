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
	"encoding/json"
	"os"
	"path/filepath"
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
	f := Feed{Items: items, Scanned: time.Now()}
	save(f)
	return f
}
