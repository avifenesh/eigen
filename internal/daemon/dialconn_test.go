package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
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

func TestClientInputSendsImages(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	reqCh := make(chan Request, 1)
	serverErr := make(chan error, 1)

	go func() {
		defer srvConn.Close()
		line, err := bufio.NewReader(srvConn).ReadBytes('\n')
		if err != nil {
			serverErr <- err
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			serverErr <- err
			return
		}
		reqCh <- req
		_, _ = srvConn.Write([]byte(`{"type":"ok"}` + "\n"))
	}()

	c := DialConn(cliConn)
	defer c.Close()

	raw := []byte{0x89, 0x50, 0x4e, 0x47}
	done := make(chan error, 1)
	go func() {
		done <- c.Input("s1", "look at this", []llm.Image{{MediaType: "image/png", Data: raw}}, []string{"inspect"})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Input: %v", err)
		}
	case err := <-serverErr:
		t.Fatalf("fake server: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Input timed out")
	}

	select {
	case req := <-reqCh:
		if req.Op != "input" || req.ID != "s1" || req.Text != "look at this" {
			t.Fatalf("input request fields wrong: %+v", req)
		}
		if len(req.AllowTools) != 1 || req.AllowTools[0] != "inspect" {
			t.Fatalf("allow tools lost: %+v", req.AllowTools)
		}
		if len(req.Images) != 1 {
			t.Fatalf("got %d images want 1", len(req.Images))
		}
		if req.Images[0].MediaType != "image/png" || !bytes.Equal(req.Images[0].Data, raw) {
			t.Fatalf("image lost in request: %+v", req.Images[0])
		}
	case err := <-serverErr:
		t.Fatalf("fake server: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for captured request")
	}
}
