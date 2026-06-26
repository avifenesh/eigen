package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// echoProv answers one line per turn.
type echoProv struct{}

func (echoProv) Name() string    { return "echo-model" }
func (echoProv) ModelID() string { return "echo-model" }
func (echoProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "echo says hi"}, nil
}

func startDaemon(t *testing.T, build daemon.Builder) (*daemon.Client, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, err := daemon.Listen(sock, daemon.NewHost(), build)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	c, err := daemon.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	id, err := c.New("/tmp/proj", "echo-model")
	if err != nil {
		t.Fatal(err)
	}
	return c, id
}

func TestRemoteSendAndState(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermGated, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)

	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// State snapshot before any turn.
	if r.ModelID() != "echo-model" || r.Perm() != agent.PermGated || r.MaxContextTokens() != 9000 {
		t.Fatalf("state: model=%q perm=%q max=%d", r.ModelID(), r.Perm(), r.MaxContextTokens())
	}

	var events []agent.EventKind
	r.Wire(func(e agent.Event) { events = append(events, e.Kind) }, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.Send(ctx, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "echo says hi" {
		t.Fatalf("final answer: %q", out)
	}
	// History synced after the turn.
	msgs := r.Messages()
	if len(msgs) < 2 {
		t.Fatalf("messages not synced: %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleUser || msgs[0].Text != "hello" {
		t.Fatalf("first message: %+v", msgs[0])
	}

	// Mutations round-trip through the daemon.
	r.SetGoal("finish the thing")
	if r.Goal() != "finish the thing" {
		t.Fatalf("goal: %q", r.Goal())
	}
	r.SetPerm(agent.PermAuto)
	if r.Perm() != agent.PermAuto {
		t.Fatalf("perm: %q", r.Perm())
	}
	// Rename round-trips and does not disturb the goal (both are *string in
	// the set op — the switch must pick the right one).
	r.SetTitle("my session")
	if r.Title() != "my session" {
		t.Fatalf("title: %q", r.Title())
	}
	if r.Goal() != "finish the thing" {
		t.Fatalf("rename clobbered the goal: %q", r.Goal())
	}
	// Clearing reverts to empty (the app falls back to a derived preview).
	r.SetTitle("")
	if r.Title() != "" {
		t.Fatalf("title not cleared: %q", r.Title())
	}
}

// TestRemoteSteerIdleStartsTurnReturnsFalse pins the APP-103 contract: when the
// daemon session is IDLE, Steer delivers the message by STARTING A NEW TURN and
// returns false (the input op is atomic — it never rejects). The message was
// still delivered, so steerOrQueue must NOT re-queue on this false (which would
// run it twice). The turn must have started (history gains the user message).
func TestRemoteSteerIdleStartsTurnReturnsFalse(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Wire(func(agent.Event) {}, nil)

	// Steer against an idle session: the daemon starts a fresh turn and reports
	// steered=false (not "running, injected").
	if r.Steer("type into the void", nil) {
		t.Fatal("Steer on an idle session must return false (it started a new turn, not steered)")
	}

	// The message WAS delivered — the daemon ran it as a turn, so it appears in
	// history. Re-queueing on the false above would send a SECOND copy.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.Refresh()
		msgs := r.Messages()
		count := 0
		for _, msg := range msgs {
			if msg.Role == llm.RoleUser && msg.Text == "type into the void" {
				count++
			}
		}
		if count == 1 {
			return // exactly one copy: delivered once, not double-sent
		}
		if count > 1 {
			t.Fatalf("message sent %d times (double-send): %+v", count, msgs)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("Steer on idle session never started a turn: %+v", r.Messages())
}

// gateRemoteProv triggers one gated tool call then finishes.
type gateRemoteProv struct{ step int }

func (p *gateRemoteProv) Name() string    { return "gate" }
func (p *gateRemoteProv) ModelID() string { return "gate" }
func (p *gateRemoteProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.step++
	if p.step == 1 {
		return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "mutate", Arguments: json.RawMessage(`{}`)}}}, nil
	}
	return &llm.Response{Text: "all done"}, nil
}

func TestRemoteApprovalRoundTrip(t *testing.T) {
	ran := false
	mut := tool.Definition{
		Name:       "mutate",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { ran = true; return "ok", nil },
	}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry(mut)
		return &agent.Agent{Provider: &gateRemoteProv{}, Tools: reg, Perm: agent.PermGated}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// Approvals arrive as EventApproval; answer through the backend like the
	// TUI does.
	r.Wire(func(e agent.Event) {
		if e.Kind == agent.EventApproval {
			r.Answer(e.Result, true)
		}
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.Send(ctx, "do it", nil)
	if err != nil || out != "all done" {
		t.Fatalf("send: %v %q", err, out)
	}
	if !ran {
		t.Fatal("approved tool should have run in the daemon")
	}
}

func TestRemoteClearAndResend(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Wire(func(agent.Event) {}, nil)
	ctx := context.Background()

	if _, err := r.Send(ctx, "first", nil); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages()) < 2 {
		t.Fatal("turn should add messages")
	}
	// /clear empties the daemon session.
	r.Reset(nil)
	if len(r.Messages()) != 0 {
		t.Fatalf("clear should empty history, got %d", len(r.Messages()))
	}
	// /resend after a fresh turn retries the last user message.
	if _, err := r.Send(ctx, "again", nil); err != nil {
		t.Fatal(err)
	}
	n := len(r.Messages())
	if _, err := r.Resend(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages()) <= n-2 {
		t.Fatalf("resend should re-run the last turn, msgs=%d (was %d)", len(r.Messages()), n)
	}
}

// namedProv reports a fixed name (used to verify model switching).
type namedProv struct{ name string }

func (p namedProv) Name() string    { return p.name + " (test backend)" } // decorated, like real providers
func (p namedProv) ModelID() string { return p.name }
func (p namedProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}

func TestRemoteModelSwitch(t *testing.T) {
	reg, _ := tool.NewRegistry()
	build := func(_, _ string) (*agent.Agent, func(), error) {
		return &agent.Agent{Provider: namedProv{"model-a"}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	sock := filepath.Join(t.TempDir(), "d.sock")
	host := daemon.NewHost()
	// The daemon rebuilds the provider for a switch — name it after the id.
	var gotSwitchID string
	host.SetModelSwitcher(func(_, modelID string) (llm.Provider, llm.Compactor, int, error) {
		gotSwitchID = modelID // must be the clean id, not "model-b (test backend)"
		return namedProv{modelID}, nil, 0, nil
	})
	srv, err := daemon.Listen(sock, host, build)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	c, err := daemon.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	id, _ := c.New("/tmp", "model-a")

	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.ModelID() != "model-a" {
		t.Fatalf("initial model: %q", r.ModelID())
	}
	r.SetModel(namedProv{"model-b"}, nil, 0)
	if gotSwitchID != "model-b" {
		t.Fatalf("daemon got switch id %q, want clean \"model-b\" (Remote.SetModel must send ModelID, not Name)", gotSwitchID)
	}
	if r.ModelID() != "model-b" {
		t.Fatalf("after switch: %q", r.ModelID())
	}
}

// effortProv supports reasoning effort (used to verify effort over the socket).
type effortProv struct{ effort string }

func (p *effortProv) Name() string    { return "effort-model" }
func (p *effortProv) ModelID() string { return "effort-model" }
func (p *effortProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}
func (p *effortProv) SetEffort(l string) bool { p.effort = l; return l == "low" || l == "high" }
func (p *effortProv) Effort() string          { return p.effort }

func TestRemoteEffortOverSocket(t *testing.T) {
	prov := &effortProv{effort: "high"}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: prov, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// Effort travels in the state snapshot — the status bar shows it without
	// a provider handle.
	if r.Effort() != "high" {
		t.Fatalf("effort = %q, want high", r.Effort())
	}
	if !r.SetEffort("low") {
		t.Fatal("SetEffort(low) should succeed")
	}
	if r.Effort() != "low" {
		t.Fatalf("after set: %q", r.Effort())
	}
	if r.SetEffort("bogus") {
		t.Fatal("SetEffort(bogus) should fail")
	}
	// A provider with no search setting reports "" (segment hidden).
	if r.SearchMode() != "" {
		t.Fatalf("search should be unsupported, got %q", r.SearchMode())
	}
}

func TestRemoteResetToHistory(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// /resume: import a transcript into the daemon session.
	hist := []llm.Message{
		{Role: llm.RoleUser, Text: "imported question"},
		{Role: llm.RoleAssistant, Text: "imported answer"},
	}
	r.Reset(hist)
	msgs := r.Messages()
	if len(msgs) != 2 || msgs[0].Text != "imported question" {
		t.Fatalf("resume over socket failed: %d msgs", len(msgs))
	}
	// And the resumed conversation continues.
	r.Wire(func(agent.Event) {}, nil)
	if _, err := r.Send(context.Background(), "follow-up", nil); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages()) < 4 {
		t.Fatalf("resumed session should continue, got %d msgs", len(r.Messages()))
	}
}

// failingProv always errors (to verify daemon-side errors surface in Send).
type failingProv struct{}

func (failingProv) Name() string    { return "fail" }
func (failingProv) ModelID() string { return "fail" }
func (failingProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("ThrottlingException: too many tokens")
}

func TestRemoteSendSurfacesDaemonError(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: failingProv{}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Wire(func(agent.Event) {}, nil)
	_, serr := r.Send(context.Background(), "hi", nil)
	if serr == nil || !strings.Contains(serr.Error(), "ThrottlingException") {
		t.Fatalf("daemon-side error should surface from Send, got %v", serr)
	}
}

// slowProv blocks until released, simulating a long-running daemon turn.
type slowProv struct{ release chan struct{} }

func (p *slowProv) Name() string    { return "slow" }
func (p *slowProv) ModelID() string { return "slow" }
func (p *slowProv) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	select {
	case <-p.release:
		return &llm.Response{Text: "finally done"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRemoteDetachLeavesTurnRunning(t *testing.T) {
	prov := &slowProv{release: make(chan struct{})}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: prov, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Wire(func(agent.Event) {}, nil)

	// Send blocks (provider is held); cancel the ctx AFTER detaching — the
	// view leaving must NOT interrupt the daemon-side turn.
	ctx, cancel := context.WithCancel(context.Background())
	sendDone := make(chan error, 1)
	go func() {
		_, err := r.Send(ctx, "long task", nil)
		sendDone <- err
	}()
	time.Sleep(100 * time.Millisecond) // let the input land
	r.Detach()
	cancel()
	select {
	case <-sendDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Send must return promptly after Detach")
	}

	// The daemon turn is still RUNNING (status working, not interrupted).
	infos, _ := c.List()
	if len(infos) != 1 || string(infos[0].Status) != "working" {
		t.Fatalf("detach interrupted the turn: %+v", infos)
	}

	// Release the provider; the turn completes daemon-side.
	close(prov.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		infos, _ = c.List()
		if len(infos) == 1 && string(infos[0].Status) == "idle" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("turn never finished after release: %+v", infos)
}

func TestRemoteSessionsList(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	if _, err := c.New("/tmp/other", "echo-model"); err != nil {
		t.Fatal(err)
	}
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.SessionID() != id {
		t.Fatalf("SessionID = %q, want %q", r.SessionID(), id)
	}
	entries := r.Sessions()
	if len(entries) != 2 {
		t.Fatalf("want 2 sessions, got %+v", entries)
	}
	dirs := map[string]bool{}
	for _, e := range entries {
		dirs[e.Dir] = true
		if e.Status == "" {
			t.Fatalf("entry missing status: %+v", e)
		}
	}
	if !dirs["/tmp/proj"] || !dirs["/tmp/other"] {
		t.Fatalf("dirs: %+v", dirs)
	}
}

// TestConcurrentRequestsDoNotCrossReplies pins the client request-serialization
// fix: many goroutines issuing different ops on ONE client must each get its
// OWN op's reply (replies carry no id — one request in flight at a time). Run
// with -race.
func TestConcurrentRequestsDoNotCrossReplies(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)

	var wg sync.WaitGroup
	errc := make(chan error, 64)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				infos, err := c.List()
				if err != nil {
					errc <- err
					return
				}
				// List must answer with sessions, never some other op's payload.
				found := false
				for _, in := range infos {
					if in.ID == id {
						found = true
					}
				}
				if !found {
					errc <- fmt.Errorf("List() reply missing session %s (crossed reply?)", id)
				}
			} else {
				st, err := c.State(id)
				if err != nil {
					errc <- err
					return
				}
				if st.Model != "echo-model" {
					errc <- fmt.Errorf("State() reply had model %q (crossed reply?)", st.Model)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}
}

// TestMultiWindowStress simulates real multi-window daemon use: several views
// attach to the same session and to sibling sessions, polling state/list while
// a turn runs — the scenario that surfaced the original history race. Run with
// -race.
func TestMultiWindowStress(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)

	var wg sync.WaitGroup
	// Pollers: hammer list/state like the rail + status bar do across windows.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = c.List()
				_, _ = c.State(id)
			}
		}()
	}
	// A driver: run a couple of turns concurrently with the polling.
	wg.Add(1)
	go func() {
		defer wg.Done()
		r, err := NewRemote(c, id)
		if err != nil {
			t.Error(err)
			return
		}
		r.Wire(func(agent.Event) {}, nil)
		for j := 0; j < 3; j++ {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = r.Send(ctx, "hi", nil)
			cancel()
		}
	}()
	wg.Wait()
}

// TestShellInfoTimesRoundTrip pins the shell Started/Finished carry over the
// wire (APP-068): the daemon snapshots tool.ShellInfo's times as unix-millis
// and Remote decodes them back, so the shells panel can show elapsed/finished.
// Both 0 (unknown / still running) and a real stamp must survive intact.
func TestShellInfoTimesRoundTrip(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000) // a fixed, non-zero instant
	// A running shell: Finished is unknown -> the wire carries 0 -> decodes to
	// the zero time (not a bogus 1970 instant).
	wire := daemon.ShellInfo{
		ID: "shell-1", Command: "npm run dev", Status: "running",
		StartedMs: started.UnixMilli(), FinishedMs: 0,
		LastLine: "listening",
	}
	if wire.StartedMs != started.UnixMilli() {
		t.Fatalf("StartedMs not carried: got %d want %d", wire.StartedMs, started.UnixMilli())
	}
	if wire.FinishedMs != 0 {
		t.Fatalf("zero Finished must serialize to 0, got %d", wire.FinishedMs)
	}
	gotStarted := msToTime(wire.StartedMs)
	if !gotStarted.Equal(started) {
		t.Fatalf("Started round-trip: got %v want %v", gotStarted, started)
	}
	if gotFinished := msToTime(wire.FinishedMs); !gotFinished.IsZero() {
		t.Fatalf("0 must decode to zero time, got %v", gotFinished)
	}
}

// TestWireToEventDoneAttribution covers the EventDone attribution carried over
// the socket: provider/model and prompt-cache hits/writes must survive the
// WireEvent round-trip so daemon sessions get per-turn model attribution and a
// cache-hit signal (not empty strings and zeroed cache tokens).
func TestWireToEventDoneAttribution(t *testing.T) {
	in := daemon.WireEvent{
		Kind:             "done",
		Step:             3,
		Text:             "final answer",
		InTokens:         120,
		OutTokens:        45,
		Provider:         "anthropic",
		Model:            "claude-opus",
		CacheReadTokens:  90,
		CacheWriteTokens: 30,
	}
	got := wireToEvent(in)
	if got.Kind != agent.EventDone {
		t.Fatalf("kind: got %v want EventDone", got.Kind)
	}
	if got.Provider != "anthropic" || got.Model != "claude-opus" {
		t.Fatalf("attribution lost: provider=%q model=%q", got.Provider, got.Model)
	}
	if got.CacheReadTokens != 90 || got.CacheWriteTokens != 30 {
		t.Fatalf("cache tokens lost: read=%d write=%d", got.CacheReadTokens, got.CacheWriteTokens)
	}
	if got.InTokens != 120 || got.OutTokens != 45 {
		t.Fatalf("usage tokens lost: in=%d out=%d", got.InTokens, got.OutTokens)
	}
}
