package gui

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/avifenesh/eigen/internal/feed"
)

// Feed bridge layer. The proactive feed is the home base's "act on" surface:
// git/github/memory signals + LLM-suggested ideas, each a one-click task that
// starts a session pre-loaded with its prompt. Feed() is instant (cache); a
// background feedLoop rescans (slow: git/github/memory + the suggester) and
// pushes fresh results to the frontend via the eigen:feed event.

const eventFeed = "eigen:feed"

// feedScanEvery matches the TUI's refresh cadence.
const feedScanEvery = 10 * time.Minute

// feedScanning is the single-flight guard for the proactive-feed scan. A scan is
// slow by nature (git/gh subprocesses + a ~90s LLM suggester, capped at 2 min)
// and races a fixed set of on-disk paths (feed.json / feed-suggest.json), so at
// most one may run at a time. The user-triggerable "Refresh feed" verb and the
// feedLoop ticker both go through scanFeed, which CAS-acquires this guard; a
// trigger that lands while a scan is in flight coalesces into a no-op. It lives
// at package scope (not on Bridge) because the GUI hosts exactly one Bridge —
// the singleton Wails service — and the guard must be owned by this file alone.
var feedScanning atomic.Bool

// FeedItemDTO mirrors feed.Item plus its stable dismiss key + a display dir name.
type FeedItemDTO struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"` // git | github | memory | suggest
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	Dir     string `json:"dir,omitempty"`
	DirName string `json:"dirName,omitempty"`
	Task    string `json:"task"`
	URL     string `json:"url,omitempty"`
}

// FeedDTO is the proactive-feed snapshot.
type FeedDTO struct {
	Items     []FeedItemDTO `json:"items"`
	ScannedMs int64         `json:"scannedMs"`
	Fresh     bool          `json:"fresh"` // false = never scanned (cache miss)
}

func toFeedItemDTO(it feed.Item) FeedItemDTO {
	name := ""
	if it.Dir != "" {
		name = filepath.Base(it.Dir)
	}
	return FeedItemDTO{
		Key: it.Key(), Kind: it.Kind, Title: it.Title, Detail: it.Detail,
		Dir: it.Dir, DirName: name, Task: it.Task, URL: it.URL,
	}
}

func feedDTO(f feed.Feed, fresh bool) *FeedDTO {
	items := feed.Top(feed.FilterDismissed(f.Items), 12, 4)
	out := make([]FeedItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, toFeedItemDTO(it))
	}
	var scanned int64
	if !f.Scanned.IsZero() {
		scanned = f.Scanned.UnixMilli()
	}
	return &FeedDTO{Items: out, ScannedMs: scanned, Fresh: fresh}
}

// Feed returns the cached proactive feed instantly (Fresh=false on a cache
// miss — the background feedLoop fills it in and emits eigen:feed).
func (b *Bridge) Feed() (*FeedDTO, error) {
	f, ok := feed.Load()
	if ok {
		b.mu.Lock()
		b.lastFeed = f
		b.mu.Unlock()
	}
	return feedDTO(f, ok), nil
}

// FeedFor returns the feed items scoped to a single project dir (its loose
// ends), in feed order, dismissed filtered.
func (b *Bridge) FeedFor(dir string) ([]FeedItemDTO, error) {
	b.mu.Lock()
	f := b.lastFeed
	b.mu.Unlock()
	if len(f.Items) == 0 {
		f, _ = feed.Load()
	}
	var out []FeedItemDTO
	for _, it := range feed.FilterDismissed(f.Items) {
		if it.Dir == dir {
			out = append(out, toFeedItemDTO(it))
		}
	}
	return out, nil
}

// StartFromFeed atomically starts a session rooted at dir with the feed item's
// task pre-submitted, returning the new session id — the GUI analogue of the
// TUI's one-keystroke "act on" flow. Server-side so it can't half-start.
func (b *Bridge) StartFromFeed(dir, task string) (string, error) {
	id, err := b.NewSession(dir, "", "")
	if err != nil {
		return "", err
	}
	if task != "" {
		if err := b.SendInput(id, task, nil, nil); err != nil {
			return id, err // session exists; surface the send failure
		}
	}
	return id, nil
}

// DismissFeed hides a feed item by key so it stops surfacing. Rebuilds the full
// Item from the last scan (the DTO only carries the key), then re-emits the
// freshened feed.
func (b *Bridge) DismissFeed(key string) error {
	b.mu.Lock()
	f := b.lastFeed
	b.mu.Unlock()
	if len(f.Items) == 0 {
		f, _ = feed.Load()
	}
	for _, it := range f.Items {
		if it.Key() == key {
			feed.Dismiss(it)
			break
		}
	}
	// Push the freshened (item now filtered) view so every surface updates.
	b.emit(eventFeed, feedDTO(f, true))
	return nil
}

// scanFeed runs one full proactive-feed scan (git/github/memory + suggester),
// caches it, and pushes it to every surface via eigen:feed. Slow by nature, so
// it always runs off the request path (the feedLoop ticker or a RescanFeed
// goroutine), never inline in a bound method.
//
// It single-flights on feedScanning: if a scan is already in flight the CAS
// fails and this call returns immediately (coalesced), so spamming the palette
// "Refresh feed" verb or a ticker tick that lands mid-scan can't stack racing
// scans over the same fixed feed.json / feed-suggest.json paths.
func (b *Bridge) scanFeed() {
	if !feedScanning.CompareAndSwap(false, true) {
		return // a scan is already running; coalesce this trigger into it
	}
	defer feedScanning.Store(false)

	dirs := b.projectDirs()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	f := feed.Scan(ctx, dirs, b.suggest)
	cancel()
	b.mu.Lock()
	b.lastFeed = f
	b.mu.Unlock()
	b.emit(eventFeed, feedDTO(f, true))
}

// RescanFeed triggers a fresh scan now without blocking the caller — the slow
// scan runs in a goroutine and its result arrives via the eigen:feed push the
// feed store already rides. This is the user-triggerable "Refresh feed" verb
// (command palette); the daemon also rescans on its own feedScanEvery cadence.
//
// Spam-safe: scanFeed single-flights on feedScanning, so a refresh that fires
// while a scan is already in flight coalesces into a no-op rather than starting
// another ~2-min scan racing the same feed.json / feed-suggest.json paths.
func (b *Bridge) RescanFeed() error {
	go b.scanFeed()
	return nil
}

// feedLoop scans the proactive feed on startup and every feedScanEvery, caching
// the result and pushing it to the frontend. The scan (git/github/memory +
// suggester) is slow; it runs off the request path entirely.
func (b *Bridge) feedLoop(stop chan struct{}) {
	// Emit the cache immediately so the UI isn't empty while the first scan runs.
	if f, ok := feed.Load(); ok {
		b.mu.Lock()
		b.lastFeed = f
		b.mu.Unlock()
		b.emit(eventFeed, feedDTO(f, true))
	}
	b.scanFeed()
	t := time.NewTicker(feedScanEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			b.scanFeed()
		}
	}
}

// projectDirs returns the dirs the feed scans; falls back to the injected dirs
// provider, else empty (the feed then yields nothing rather than erroring).
func (b *Bridge) projectDirs() []string {
	if b.dirs != nil {
		return b.dirs()
	}
	return nil
}
