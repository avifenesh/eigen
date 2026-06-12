package voice

import (
	"context"
	"testing"
	"time"
)

func TestRecordVADNoFramesTimesOut(t *testing.T) {
	// A recorder that produces NO output (mic missing/busy) must not hang:
	// the maxWait deadline fires from the heartbeat, not from frame reads.
	p := defaultVAD()
	p.maxWait = 300 * time.Millisecond
	done := make(chan struct{})
	var pcm []byte
	go func() {
		pcm, _ = recordVAD(context.Background(), []string{"sleep", "30"}, p)
		close(done)
	}()
	select {
	case <-done:
		if pcm != nil {
			t.Fatalf("no speech expected, got %d bytes", len(pcm))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recordVAD hung with a silent recorder")
	}
}

func TestRecordVADCancelStops(t *testing.T) {
	// Caller cancel (the stop button) must end a recording promptly even when
	// the recorder streams silence forever.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// /dev/zero = endless silence frames.
		recordVAD(ctx, []string{"cat", "/dev/zero"}, defaultVAD())
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recordVAD did not stop on cancel")
	}
}
