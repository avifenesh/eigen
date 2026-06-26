package app

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
)

// TestLiveNewUsesConfiguredModel guards APP-079: starting a session from the
// live page ("n") must root it on the user's configured default model, not let
// the daemon silently pick its own default. A fake daemon over a pipe captures
// the wire request and we assert its model field equals data.Config.Model.
func TestLiveNewUsesConfiguredModel(t *testing.T) {
	cliConn, srvConn := net.Pipe()

	gotModel := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := srvConn.Read(buf)
		if n == 0 {
			gotModel <- "<no request>"
			return
		}
		var req daemon.Request
		_ = json.Unmarshal(buf[:n], &req)
		gotModel <- req.Model
		// Reply so the client's New() returns without timing out.
		_, _ = srvConn.Write([]byte(`{"type":"ok","id":"new-1"}` + "\n"))
	}()

	d := testData()
	d.Config.Model = "claude-opus-4-8"
	d.Daemon = daemon.DialConn(cliConn)
	defer d.Daemon.Close()

	m := NewAt(d, PageLive)
	m.width, m.height = 110, 32

	done := make(chan struct{})
	go func() {
		m.live.update(m, key("n"))
		close(done)
	}()

	select {
	case got := <-gotModel:
		if got != "claude-opus-4-8" {
			t.Fatalf("daemon New got model %q, want configured default %q", got, "claude-opus-4-8")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no daemon request observed — 'n' did not call New")
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("live.update did not return after New replied")
	}
}
