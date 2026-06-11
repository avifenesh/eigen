package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
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

func (echoProvider) Name() string    { return "echo" }
func (echoProvider) ModelID() string { return "echo" }
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

func TestClientEndToEnd(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, err := Listen(sock, NewHost(), testBuilder)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Ping(); err != nil {
		t.Fatal(err)
	}
	id, err := c.New("/tmp/p", "")
	if err != nil || id == "" {
		t.Fatalf("new: %v %q", err, id)
	}
	infos, err := c.List()
	if err != nil || len(infos) != 1 {
		t.Fatalf("list: %v %d", err, len(infos))
	}

	events := make(chan WireEvent, 64)
	if err := c.Attach(id, func(e WireEvent, replay bool) { events <- e }); err != nil {
		t.Fatal(err)
	}
	if err := c.Input(id, "hi", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == "done" {
				if e.Text != "hello from echo" {
					t.Fatalf("done text: %q", e.Text)
				}
				return
			}
		case <-deadline:
			t.Fatal("no done event via client")
		}
	}
}

func TestListenRemovesStaleSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	// Create a stale socket file (a plain file at the path, nothing listening).
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := Listen(sock, NewHost(), testBuilder)
	if err != nil {
		t.Fatalf("Listen should remove a stale socket and bind, got %v", err)
	}
	srv.Close()
}

func TestPIDLifecycle(t *testing.T) {
	pid := filepath.Join(t.TempDir(), "daemon.pid")
	if RunningPID(pid) != 0 {
		t.Fatal("no pid file → not running")
	}
	if err := WritePID(pid); err != nil {
		t.Fatal(err)
	}
	if RunningPID(pid) != os.Getpid() {
		t.Fatalf("should report this process as running")
	}
	// A pid file pointing at a dead process is treated as not-running.
	if err := os.WriteFile(pid, []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	if RunningPID(pid) != 0 {
		t.Fatal("dead pid should be reported as not running")
	}
	RemovePID(pid)
	if RunningPID(pid) != 0 {
		t.Fatal("removed pid file → not running")
	}
}

func TestInterruptEmitsTerminalNote(t *testing.T) {
	// A session whose turn is interrupted must emit a terminal note so views
	// leave the "working" state (the freeze bug).
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: blockingProvider{}, Tools: reg}
	s := newSession("x", "/tmp", "m", a)
	_, live, detach := s.attach()
	defer detach()
	if !s.send("go", nil) {
		t.Fatal("send should start")
	}
	time.Sleep(50 * time.Millisecond)
	s.interrupt()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-live:
			if e.Kind == agent.EventNote && e.Text == "interrupted" {
				return
			}
		case <-deadline:
			t.Fatal("no terminal note after interrupt")
		}
	}
}

// blockingProvider hangs until ctx is cancelled (to test interrupt).
type blockingProvider struct{}

func (blockingProvider) Name() string    { return "block" }
func (blockingProvider) ModelID() string { return "block" }
func (blockingProvider) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// toolCallProvider calls a mutating tool once, then finishes.
type toolCallProvider struct{ step int }

func (p *toolCallProvider) Name() string    { return "tc" }
func (p *toolCallProvider) ModelID() string { return "tc" }
func (p *toolCallProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.step++
	if p.step == 1 {
		return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "mutate", Arguments: json.RawMessage(`{}`)}}}, nil
	}
	return &llm.Response{Text: "finished"}, nil
}

func TestApprovalRoundTripOverSocket(t *testing.T) {
	// A GATED daemon session must broadcast a blocked tool call as an approval
	// event; a view answering y over the socket lets it run.
	ran := false
	mut := tool.Definition{
		Name:       "mutate",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { ran = true; return "did it", nil },
	}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry(mut)
		a := &agent.Agent{Provider: &toolCallProvider{}, Tools: reg, Perm: agent.PermGated}
		return a, func() {}, nil
	}
	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, err := Listen(sock, NewHost(), build)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	id, err := c.New("/tmp", "")
	if err != nil {
		t.Fatal(err)
	}
	approvals := make(chan WireEvent, 4)
	dones := make(chan WireEvent, 4)
	if err := c.Attach(id, func(e WireEvent, _ bool) {
		switch e.Kind {
		case "approval":
			approvals <- e
		case "done":
			dones <- e
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Input(id, "do the thing", nil); err != nil {
		t.Fatal(err)
	}
	// The blocked tool call must surface as an approval event.
	var ap WireEvent
	select {
	case ap = <-approvals:
	case <-time.After(3 * time.Second):
		t.Fatal("no approval event broadcast")
	}
	if ap.ToolName != "mutate" || ap.Result == "" {
		t.Fatalf("approval event malformed: %+v", ap)
	}
	// Answer y over the socket → the tool runs → the turn finishes.
	if err := c.Approve(id, ap.Result, true); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-dones:
		if d.Text != "finished" {
			t.Fatalf("done text: %q", d.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish after approval")
	}
	if !ran {
		t.Fatal("approved tool should have run")
	}
}

func TestApprovalDenied(t *testing.T) {
	ran := false
	mut := tool.Definition{
		Name:       "mutate",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { ran = true; return "did it", nil },
	}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry(mut)
		a := &agent.Agent{Provider: &toolCallProvider{}, Tools: reg, Perm: agent.PermGated}
		return a, func() {}, nil
	}
	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, _ := Listen(sock, NewHost(), build)
	go srv.Serve()
	defer srv.Close()
	c, _ := Dial(sock)
	defer c.Close()
	id, _ := c.New("/tmp", "")
	approvals := make(chan WireEvent, 4)
	dones := make(chan WireEvent, 4)
	c.Attach(id, func(e WireEvent, _ bool) {
		switch e.Kind {
		case "approval":
			approvals <- e
		case "done":
			dones <- e
		}
	})
	c.Input(id, "go", nil)
	var ap WireEvent
	select {
	case ap = <-approvals:
	case <-time.After(3 * time.Second):
		t.Fatal("no approval event")
	}
	c.Approve(id, ap.Result, false) // deny
	select {
	case <-dones:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish after denial")
	}
	if ran {
		t.Fatal("denied tool must NOT run")
	}
}

// TestWireEventCarriesEverything pins wire fidelity: every agent.Event field
// the TUI renders must survive the socket round-trip. Regression: ToolArgs
// were dropped, so daemon-attached views showed bare "bash" tool blocks with
// no command and no expandable body.
func TestWireEventCarriesEverything(t *testing.T) {
	in := agent.Event{
		Kind:     agent.EventToolStart,
		Step:     3,
		Text:     "txt",
		ToolName: "bash",
		ToolID:   "tc_1",
		ToolArgs: json.RawMessage(`{"command":"echo hello"}`),
		Result:   "res",
		IsError:  true,
	}
	w := wireEvent(in)
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var back WireEvent
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ToolName != "bash" || back.ToolID != "tc_1" || back.Step != 3 ||
		back.Text != "txt" || back.Result != "res" || !back.IsError {
		t.Fatalf("lost fields: %+v", back)
	}
	if string(back.ToolArgs) != `{"command":"echo hello"}` {
		t.Fatalf("ToolArgs lost: %q", back.ToolArgs)
	}
}
