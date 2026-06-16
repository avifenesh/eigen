package daemon

import (
	"encoding/json"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
)

// BenchmarkWireEventEncode measures the per-event hot path: every streamed token
// becomes a WireEvent and is JSON-encoded to the socket. Watch ns/op + allocs/op
// for regressions (Tier 23 turn-latency).
func BenchmarkWireEventEncode(b *testing.B) {
	e := agent.Event{Kind: agent.EventTextDelta, Text: "a chunk of streamed assistant text, typical token batch length"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		we := wireEvent(e)
		if _, err := json.Marshal(Response{Type: "event", Event: we}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHostStats measures the stats snapshot cost (runs runtime.ReadMemStats
// + walks sessions) — must stay cheap enough to poll.
func BenchmarkHostStats(b *testing.B) {
	host := NewHost()
	for i := 0; i < 10; i++ {
		a, _, _ := testBuilder("", "")
		host.Add("/tmp/p", "", a)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = host.Stats()
	}
}
