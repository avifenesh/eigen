package gui

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/feed"
)

// resetFeedScanning clears the package-level single-flight guard so each test
// starts from a known state (the guard lives at package scope — see feed.go).
func resetFeedScanning() { feedScanning.Store(false) }

// TestScanFeedSingleFlight proves scanFeed is a no-op while a scan is already in
// flight: with the guard held, a second scanFeed must not run feed.Scan (which
// would clobber lastFeed and emit), so the cached feed stays untouched. This is
// the guarantee that lets the palette "Refresh feed" verb be spammed safely —
// each extra RescanFeed coalesces instead of starting another ~2-min scan
// racing the same fixed feed.json / feed-suggest.json paths.
func TestScanFeedSingleFlight(t *testing.T) {
	resetFeedScanning()
	defer resetFeedScanning()

	b := NewBridge(func() (*daemon.Client, error) { return nil, nil }, nil, nil)
	sentinel := feed.Feed{Items: []feed.Item{{Kind: "git", Title: "in-flight scan owns this"}}}
	b.mu.Lock()
	b.lastFeed = sentinel
	b.mu.Unlock()

	// Simulate a scan already in flight.
	if !feedScanning.CompareAndSwap(false, true) {
		t.Fatal("guard was already held at start")
	}

	// This call must coalesce into a no-op: it must not invoke feed.Scan (which
	// would overwrite lastFeed) and must not emit.
	b.scanFeed()

	b.mu.Lock()
	got := b.lastFeed
	b.mu.Unlock()
	if len(got.Items) != 1 || got.Items[0].Title != sentinel.Items[0].Title {
		t.Fatalf("scanFeed ran while a scan was in flight: lastFeed mutated to %+v", got)
	}
}

// TestScanFeedGuardAdmitsOne proves the CAS guard admits exactly one scanner
// under a concurrent stampede — the property RescanFeed relies on so spamming
// the palette verb can't stack racing scans. Every racer attempts the guard the
// same way scanFeed does; none release it until all have attempted, so only the
// single CAS winner can ever get through.
func TestScanFeedGuardAdmitsOne(t *testing.T) {
	resetFeedScanning()
	defer resetFeedScanning()

	const racers = 16
	var acquired int32
	start := make(chan struct{})
	var attempted, done sync.WaitGroup
	attempted.Add(racers)
	done.Add(racers)

	for i := 0; i < racers; i++ {
		go func() {
			defer done.Done()
			<-start // line every racer up so they truly contend
			won := feedScanning.CompareAndSwap(false, true)
			if won {
				atomic.AddInt32(&acquired, 1)
			}
			attempted.Done()
			if won {
				// Hold the guard until every racer has attempted its CAS, so no
				// loser can sneak in after a release and double-acquire.
				attempted.Wait()
				feedScanning.Store(false)
			}
		}()
	}

	close(start)
	done.Wait()

	if got := atomic.LoadInt32(&acquired); got != 1 {
		t.Fatalf("guard admitted %d scanners, want exactly 1", got)
	}
}
