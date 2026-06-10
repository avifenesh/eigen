package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// echoProvider answers each turn with a fixed line (no tools), so a hosted
// session produces a deterministic EventDone.
type echoProvider struct{}

func (echoProvider) Name() string { return "echo" }
func (echoProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "hello from echo"}, nil
}

func testBuilder(_, _ string) (*agent.Agent, func(), error) {
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg}
	return a, func() {}, nil
}

// dialAndScan connects to the daemon socket and returns a JSON-line decoder.
func dialAndScan(t *testing.T, sock string) (net.Conn, *bufio.Scanner) {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return conn, sc
}

func send(t *testing.T, conn net.Conn, req Request) {
	t.Helper()
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readResp(t *testing.T, sc *bufio.Scanner) Response {
	t.Helper()
	if !sc.Scan() {
		t.Fatal("no response")
	}
	var r Response
	if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func TestDaemonNewListInputAttach(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	host := NewHost()
	srv, err := Listen(sock, host, testBuilder)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()

	conn, sc := dialAndScan(t, sock)
	defer conn.Close()

	// new
	send(t, conn, Request{Op: "new", Dir: "/tmp/proj", Model: "m"})
	r := readResp(t, sc)
	if r.Type != "ok" || r.ID == "" {
		t.Fatalf("new failed: %+v", r)
	}
	id := r.ID

	// list shows it
	send(t, conn, Request{Op: "list"})
	r = readResp(t, sc)
	if r.Type != "sessions" || len(r.Sessions) != 1 || r.Sessions[0].ID != id {
		t.Fatalf("list wrong: %+v", r)
	}
	if r.Sessions[0].Dir != "/tmp/proj" {
		t.Fatalf("dir not recorded: %+v", r.Sessions[0])
	}

	// attach → expect "attached", then live events
	send(t, conn, Request{Op: "attach", ID: id})
	r = readResp(t, sc)
	if r.Type != "attached" {
		t.Fatalf("attach: %+v", r)
	}

	// input → triggers a turn; "ok" and streamed events race on one conn, so
	// just scan everything until we see the 'done' event.
	send(t, conn, Request{Op: "input", ID: id, Text: "hi"})
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	gotDone := false
	for sc.Scan() {
		var resp Response
		json.Unmarshal(sc.Bytes(), &resp)
		if resp.Type == "event" && resp.Event != nil && resp.Event.Kind == "done" {
			gotDone = true
			break
		}
	}
	if !gotDone {
		t.Fatal("expected a streamed 'done' event after input")
	}
}

func TestDaemonSecondListenFails(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	host := NewHost()
	srv, err := Listen(sock, host, testBuilder)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()
	// A second daemon on the same live socket must fail ("already running").
	if _, err := Listen(sock, NewHost(), testBuilder); err == nil {
		t.Fatal("second Listen should fail while the first is live")
	}
}
