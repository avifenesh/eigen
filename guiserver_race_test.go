package main

import (
	"sync"
	"testing"
	"time"
)

// TestSocketEmitterRace verifies that concurrent Emit + unregister operations
// do not trigger a send-on-closed-channel panic. This is the regression test
// for the critical race fixed in guiserver.go: the original code copied the
// subscription slice under lock then iterated WITHOUT holding e.mu, so
// unregister() could close sub.queue while Emit was mid-iteration. The fix
// holds sub.mu for the entire channel-check + send decision and guards queue
// sends with the closed flag.
func TestSocketEmitterRace(t *testing.T) {
	e := newSocketEmitter()

	// Create 10 subscriptions
	var subs []*subscription
	for i := 0; i < 10; i++ {
		sub := newSubscription(256, func(v any) error { return nil })
		sub.subscribe("test-channel")
		e.register(sub)
		subs = append(subs, sub)
	}

	// Emit storm + concurrent unregister: 100 emits on 10 goroutines while
	// concurrently unregistering all subs on separate goroutines. If the race
	// exists, this will panic with "send on closed channel".
	var wg sync.WaitGroup

	// Emitters: 10 goroutines each emitting 100 times
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				e.Emit("test-channel", map[string]int{"seq": j})
				time.Sleep(1 * time.Microsecond) // small delay to spread the storm
			}
		}()
	}

	// Unregisterers: concurrently unregister all subs (triggers queue close)
	for _, sub := range subs {
		wg.Add(1)
		go func(s *subscription) {
			defer wg.Done()
			time.Sleep(50 * time.Microsecond) // let some emits happen first
			e.unregister(s)
		}(sub)
	}

	wg.Wait()
	// If we reach here without panic, the race is fixed.
}

// TestSubscriptionDoubleClose verifies that subscription.close() is idempotent
// (double-close does not panic).
func TestSubscriptionDoubleClose(t *testing.T) {
	sub := newSubscription(10, func(v any) error { return nil })

	// First close should work
	sub.close()

	// Second close should be a no-op (not panic)
	sub.close()

	// Emit after close should be a no-op (not panic)
	sub.emit("test", "data")
}
