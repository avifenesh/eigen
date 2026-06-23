package gui

import (
	"context"
	"path/filepath"
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

// feedLoop scans the proactive feed on startup and every feedScanEvery, caching
// the result and pushing it to the frontend. The scan (git/github/memory +
// suggester) is slow; it runs off the request path entirely.
func (b *Bridge) feedLoop(stop chan struct{}) {
	scan := func() {
		dirs := b.projectDirs()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		f := feed.Scan(ctx, dirs, b.suggest)
		cancel()
		b.mu.Lock()
		b.lastFeed = f
		b.mu.Unlock()
		b.emit(eventFeed, feedDTO(f, true))
	}
	// Emit the cache immediately so the UI isn't empty while the first scan runs.
	if f, ok := feed.Load(); ok {
		b.mu.Lock()
		b.lastFeed = f
		b.mu.Unlock()
		b.emit(eventFeed, feedDTO(f, true))
	}
	scan()
	t := time.NewTicker(feedScanEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			scan()
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
