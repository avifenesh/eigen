package daemon

import (
	"runtime"

	"github.com/avifenesh/eigen/internal/agent"
	"testing"
	"time"
)

// TestSoakSessionChurnNoLeak hammers the host with session create/remove and
// attach/detach churn, then asserts goroutines + the sessions map settle rather
// than climbing — the regression guard for daemon leaks (Tier 23).
//
// It exercises the real growth-prone paths (sessions map, per-session subs, the
// replay buffer, attach goroutines) without needing a live model.
func TestSoakSessionChurnNoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("soak: skipped in -short")
	}
	host := NewHost()

	// Warm up: one cycle so steady-state structures exist, then snapshot.
	churn(t, host, 5)
	settle()
	gBase := runtime.NumGoroutine()

	// Many cycles of create → attach → detach → remove.
	const cycles = 300
	churn(t, host, cycles)
	settle()

	gAfter := runtime.NumGoroutine()
	// The host must be empty (every created session removed).
	host.mu.Lock()
	left := len(host.sessions)
	host.mu.Unlock()
	if left != 0 {
		t.Fatalf("sessions map should drain to 0 after churn, got %d", left)
	}
	// Goroutines must not grow with the number of cycles. Allow generous slack
	// for runtime/test scheduler noise, but a per-cycle leak (≥1 goroutine each)
	// would blow far past this.
	if gAfter > gBase+20 {
		t.Fatalf("goroutine leak: base=%d after %d cycles=%d (+%d)", gBase, cycles, gAfter, gAfter-gBase)
	}
}

// churn runs n cycles of create-a-session → attach → detach → remove directly
// on the host (no socket needed — exercises the leak-prone host internals).
func churn(t *testing.T, host *Host, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		a, _, _ := testBuilder("", "")
		s := host.Add("/tmp/p", "", a)
		// Attach a view, push a few events into the replay buffer, then detach.
		_, _, detach := s.attach()
		for j := 0; j < 10; j++ {
			s.dispatch(agent.Event{Kind: agent.EventTextDelta, Text: "x"})
		}
		detach()
		host.Remove(s.ID)
	}
}

func settle() {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	runtime.GC()
}
