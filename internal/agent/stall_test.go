package agent

import (
	"context"
	"testing"
	"time"
)

// A model call in flight must be measured against the larger modelMaxWait
// budget, not the tight between-actions stallIdle — this is the false-positive
// that cancelled healthy non-streaming subtasks ("converse: context canceled" /
// "stalled (no tool activity for 2m)"). A slow inference is not a hang.
func TestHeartbeatModelInFlightUsesLargerBudget(t *testing.T) {
	hb := newHeartbeat()
	hb.last = time.Now().Add(-3 * time.Minute) // 3 min since last activity

	// Not in flight → that's a real between-actions stall (> stallIdle 2m).
	since, inFlight := hb.idle()
	if inFlight || since < 2*time.Minute {
		t.Fatalf("expected a real idle (>2m, not in flight), got since=%s inFlight=%v", since, inFlight)
	}

	// Model call begins → still 3 min elapsed, but now flagged in-flight, so the
	// watchdog will compare against modelMaxWait (5m) and NOT cancel at 3m.
	hb.modelStart()
	since, inFlight = hb.idle()
	if !inFlight {
		t.Fatal("modelStart must flag in-flight")
	}
	if since >= modelMaxWait {
		t.Fatalf("3m in flight must be under the %s model cap", modelMaxWait)
	}

	// Call ends → idle resumes from now.
	hb.modelEnd()
	since, inFlight = hb.idle()
	if inFlight || since > time.Second {
		t.Fatalf("after model end: not in flight, clock reset; got since=%s inFlight=%v", since, inFlight)
	}
}

// watchStall must NOT fire while a model call is in flight under modelMaxWait,
// but MUST fire on a real between-actions idle.
func TestWatchStallToleratesInFlightButCatchesIdle(t *testing.T) {
	t.Run("in-flight under cap: no fire", func(t *testing.T) {
		oldModel := modelMaxWait
		modelMaxWait = 5 * time.Second
		defer func() { modelMaxWait = oldModel }()

		hb := newHeartbeat()
		hb.modelStart()                            // in flight
		hb.last = time.Now().Add(-1 * time.Second) // 1s elapsed, well under 5s cap

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		fired := watchStall(ctx, hb, cancel, 100*time.Millisecond, 0)
		time.Sleep(400 * time.Millisecond) // way past stallIdle(100ms), still in flight
		if fired() {
			t.Fatal("must NOT fire on an in-flight model call under the model cap")
		}
	})

	t.Run("real idle: fires", func(t *testing.T) {
		hb := newHeartbeat()
		hb.last = time.Now().Add(-time.Hour) // long idle, NOT in flight
		done := make(chan struct{})
		cancelled := false
		cancel := func() { cancelled = true; close(done) }
		fired := watchStall(t.Context(), hb, cancel, 40*time.Millisecond, 0)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("watchStall should have fired on a real idle")
		}
		if !cancelled || !fired() {
			t.Fatalf("expected cancel+fired on real idle (cancelled=%v fired=%v)", cancelled, fired())
		}
	})
}

// activitySink: a real event ends the in-flight window and beats, and is
// forwarded to the chained sink. (The in-flight window is opened by the
// onModelCall hook = hb.modelStart, tested above.)
func TestActivitySinkEndsInFlightOnRealEvent(t *testing.T) {
	hb := newHeartbeat()
	hb.modelStart() // a model call is in flight
	var forwarded []EventKind
	sink := activitySink(hb, func(e Event) { forwarded = append(forwarded, e.Kind) })

	sink(Event{Kind: EventTextDelta, Text: "hi"})
	if _, inFlight := hb.idle(); inFlight {
		t.Fatal("a real event should end the in-flight window")
	}
	if len(forwarded) != 1 || forwarded[0] != EventTextDelta {
		t.Fatalf("expected the delta forwarded, got %v", forwarded)
	}
}
