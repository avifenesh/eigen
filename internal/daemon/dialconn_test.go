package daemon

import (
	"net"
	"testing"
	"time"
)

// TestDialConnOverPipe proves the Client works over ANY io.ReadWriteCloser
// (here an in-memory net.Pipe), not just a unix socket — the Wave 0 transport
// seam for remote use (ssh stdio, websocket). A fake server speaks the line-
// JSON protocol over its pipe end; the client's request() must round-trip.
func TestDialConnOverPipe(t *testing.T) {
	cliConn, srvConn := net.Pipe()

	// Fake daemon: read one request line, reply with a sessions response.
	go func() {
		buf := make([]byte, 4096)
		n, _ := srvConn.Read(buf)
		if n == 0 {
			return
		}
		_, _ = srvConn.Write([]byte(`{"type":"sessions"}` + "\n"))
	}()

	c := DialConn(cliConn)
	defer c.Close()

	done := make(chan error, 1)
	go func() {
		_, err := c.List()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("List over pipe: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("List over pipe timed out — transport seam broken")
	}
}
