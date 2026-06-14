package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func TestReplayBufferBoundedAndClearedAfterTurn(t *testing.T) {
	// A long-lived session must not accumulate events forever. Drive many
	// dispatches; the buffer stays bounded, and after a turn finishes it's
	// dropped (a post-turn attach reconstructs from Messages()).
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg}
	s := newSession("x", "/tmp", "m", a)

	for i := 0; i < maxReplayEvents+500; i++ {
		s.dispatch(agent.Event{Kind: agent.EventTextDelta, Text: "x"})
	}
	s.mu.Lock()
	n := len(s.events)
	s.mu.Unlock()
	if n > maxReplayEvents {
		t.Fatalf("replay buffer should be bounded at %d, got %d", maxReplayEvents, n)
	}

	// finishTurn drops the buffer entirely.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // mark interrupted so finishTurn emits a note then clears
	s.finishTurn(ctx, nil)
	s.mu.Lock()
	after := len(s.events)
	s.mu.Unlock()
	if after != 0 {
		t.Fatalf("replay buffer should be cleared after a turn, got %d", after)
	}
}

// panicProvider panics on Complete — simulating a provider/parse bug.
type panicProvider struct{}

func (panicProvider) Name() string    { return "panic" }
func (panicProvider) ModelID() string { return "panic" }
func (panicProvider) Complete(context.Context, llm.Request) (*llm.Response, error) {
	panic("boom in the turn")
}

func TestTurnPanicDoesNotCrashDaemonEmitsError(t *testing.T) {
	// A panic during a turn must be contained: the session emits a terminal
	// error note and stays usable, and (critically) the daemon process — every
	// OTHER hosted session — survives.
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: panicProvider{}, Tools: reg}
	s := newSession("x", "/tmp", "m", a)
	_, live, detach := s.attach()
	defer detach()
	if !s.send("go", nil) {
		t.Fatal("send should start")
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-live:
			if e.Kind == agent.EventNote && strings.HasPrefix(e.Text, "error: ") &&
				strings.Contains(e.Text, "panic") {
				// Session recovered: a follow-up turn can start.
				if s.running {
					t.Fatal("session should not be stuck running after a panic")
				}
				return
			}
		case <-deadline:
			t.Fatal("a panicking turn should emit a terminal error note")
		}
	}
}

// TestStateRaceWithModelSwitch pins the provider-field race fix: state() must
// read the provider/perm/budget through locked accessors because a /model
// switch (SetLive) swaps them from another goroutine. Run with -race.
func TestStateRaceWithModelSwitch(t *testing.T) {
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto, MaxContextTokens: 9000}
	s := newSession("x", "/tmp", "m", a)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.state() // reads provider/perm/budget
				_ = s.info()
			}
		}
	}()
	// Hammer live switches that mutate the same fields.
	for i := 0; i < 200; i++ {
		a.SetLive(echoProvider{}, nil, 8000+i)
		a.SetPerm(agent.PermGated)
		a.SetPerm(agent.PermAuto)
	}
	close(stop)
	<-done
}

// TestStateReportsRunning pins the re-attach fix: a session with a turn in
// flight must report Running:true in state(), so a view attaching mid-turn
// shows "working" and queues input instead of erroring "session busy". After
// the turn ends, Running flips back to false.
func TestStateReportsRunning(t *testing.T) {
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: blockingProvider{}, Tools: reg}
	s := newSession("x", "/tmp", "m", a)

	if s.state().Running {
		t.Fatal("a fresh session should not be running")
	}
	if !s.send("go", nil) {
		t.Fatal("send should start the turn")
	}
	// The blocking provider holds the turn open: state must report running.
	deadline := time.After(2 * time.Second)
	for {
		if s.state().Running {
			break
		}
		select {
		case <-deadline:
			t.Fatal("state().Running should be true while a turn is in flight")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	// Interrupt → the turn ends → Running flips false.
	s.interrupt()
	deadline = time.After(2 * time.Second)
	for {
		if !s.state().Running {
			return
		}
		select {
		case <-deadline:
			t.Fatal("state().Running should be false after the turn ends")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestCompactUsesLongerTimeout proves the per-op timeout fix: a compact reply
// that arrives slowly (longer than a short op timeout would allow, simulating a
// real summarizer call over a large context) still succeeds, while the same
// delay under a too-short timeout fails. This is the exact bug — the 30s
// default was killing legitimate, slow compactions mid-summary.
func TestCompactUsesLongerTimeout(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// Mock daemon: on a "compact" op, wait 200ms then reply "compacted".
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			var req Request
			if json.Unmarshal(sc.Bytes(), &req) != nil {
				continue
			}
			if req.Op == "compact" {
				time.Sleep(200 * time.Millisecond)
				b, _ := json.Marshal(Response{Type: "compacted", Before: 10, After: 4})
				conn.Write(append(b, '\n'))
			}
		}
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// A too-short timeout fails (the old 30s-vs-slow-summary bug, in miniature).
	if _, err := c.requestWithin(Request{Op: "compact", ID: "s1", Target: 8000}, 50*time.Millisecond); err == nil {
		t.Fatal("expected a timeout when the op outlasts the deadline")
	}
	// A generous timeout (like compactRequestTimeout) lets the slow reply land.
	r, err := c.requestWithin(Request{Op: "compact", ID: "s1", Target: 8000}, 5*time.Second)
	if err != nil {
		t.Fatalf("slow compact should succeed with a generous timeout: %v", err)
	}
	if r.Before != 10 || r.After != 4 {
		t.Fatalf("compact result not delivered: before=%d after=%d", r.Before, r.After)
	}
}
