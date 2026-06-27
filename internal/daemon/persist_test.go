package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
	"github.com/avifenesh/eigen/internal/transcript"
)

func persistentBuilder() Builder {
	return func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
}

// runTurn drives one full turn over the socket and waits for done.
func runTurn(t *testing.T, sock, id, text string) {
	t.Helper()
	conn, sc := dialAndScan(t, sock)
	defer conn.Close()
	send(t, conn, Request{Op: "attach", ID: id})
	send(t, conn, Request{Op: "input", ID: id, Text: text})
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for sc.Scan() {
		var r Response
		json.Unmarshal(sc.Bytes(), &r)
		if r.Type == "event" && r.Event != nil && r.Event.Kind == "done" && !r.Replay {
			return
		}
	}
	t.Fatal("turn did not complete")
}

// waitTranscript blocks until the transcript file at path is readable and
// contains want, or fails after a bounded wait. Closes the gap between a turn's
// `done` event (which the test observes over the dispatch channel) and the
// agent goroutine's disk write landing — the source of the restart flake.
func waitTranscript(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("transcript %s never contained %q", path, want)
}

func TestPersistAcrossDaemonRestart(t *testing.T) {
	// THE core property: kill the daemon, start a new one, the session is
	// there — same id, same history, same goal/perm — and keeps working.
	persistDir := t.TempDir()
	sock := filepath.Join(t.TempDir(), "d.sock")

	// Daemon #1: create a session, run a turn, set a goal.
	h1 := NewPersistentHost(persistDir)
	srv1, err := Listen(sock, h1, persistentBuilder())
	if err != nil {
		t.Fatal(err)
	}
	go srv1.Serve()
	c1, _ := Dial(sock)
	id, err := c1.New("/tmp/proj", "echo-model")
	if err != nil {
		t.Fatal(err)
	}
	runTurn(t, sock, id, "remember me")
	if err := c1.SetGoal(id, "finish the persistence work"); err != nil {
		t.Fatal(err)
	}
	// The `done` event the test waited on is delivered to the view over the
	// dispatch channel; the agent's transcript Persist hook runs in the agent
	// goroutine. Those are ordered (persist before emit) on the happy path, but
	// under CI load the disk write can still be a beat behind the event arrival —
	// which made this test flaky (restored 0 sessions / "history lost"). Before
	// tearing daemon #1 down, wait until the transcript is durably readable with
	// the turn's content, so the restart reads a complete file deterministically.
	waitTranscript(t, transcriptPath(persistDir, id), "remember me")
	c1.Close()
	h1.Shutdown()
	if err := srv1.Close(); err != nil {
		t.Fatal(err)
	}

	// Daemon #2: restore.
	h2 := NewPersistentHost(persistDir)
	n := h2.Restore(persistentBuilder())
	if n != 1 {
		t.Fatalf("restored %d sessions, want 1", n)
	}
	srv2, err := Listen(sock, h2, persistentBuilder())
	if err != nil {
		t.Fatal(err)
	}
	go srv2.Serve()
	defer h2.Shutdown()
	defer srv2.Close()

	c2, _ := Dial(sock)
	defer c2.Close()
	st, err := c2.State(id) // SAME id works
	if err != nil {
		t.Fatalf("state after restart: %v", err)
	}
	if len(st.Messages) < 2 {
		t.Fatalf("history lost: %d messages", len(st.Messages))
	}
	if st.Messages[0].Text != "remember me" {
		t.Fatalf("first message: %q", st.Messages[0].Text)
	}
	if st.Goal != "finish the persistence work" {
		t.Fatalf("goal lost: %q", st.Goal)
	}
	// And the restored session still RUNS turns.
	runTurn(t, sock, id, "still alive?")
	st, _ = c2.State(id)
	if len(st.Messages) < 4 {
		t.Fatalf("restored session did not continue: %d messages", len(st.Messages))
	}
	// New sessions continue the id sequence (no collision with restored s1).
	id2, err := c2.New("/tmp/other", "")
	if err != nil {
		t.Fatal(err)
	}
	if id2 == id {
		t.Fatalf("new session id collides with restored: %q", id2)
	}
}

func TestInactivePersistentSessionUnloadsAndHydratesOnDemand(t *testing.T) {
	persistDir := t.TempDir()
	sock := filepath.Join(t.TempDir(), "d.sock")
	var builds, closes atomic.Int32
	build := func(_, _ string) (*agent.Agent, func(), error) {
		builds.Add(1)
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}, func() { closes.Add(1) }, nil
	}

	h := NewPersistentHost(persistDir)
	srv, err := Listen(sock, h, build)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.NewSession("/tmp/proj", "m", "auto", []llm.Message{{Role: llm.RoleUser, Text: "cold hello"}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && closes.Load() < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	if builds.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("new idle session should build then unload once, builds=%d closes=%d", builds.Load(), closes.Load())
	}
	s := h.Get(id)
	if s == nil {
		t.Fatal("session missing after creation")
	}
	isCold := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.agent == nil && s.sess == nil
	}
	isLive := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.agent != nil && s.sess != nil
	}
	if !isCold() {
		t.Fatal("idle session should be cold after creation")
	}
	infos, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Turns != 1 || infos[0].Title != "cold hello" {
		t.Fatalf("cold row should list from summary metadata, got %+v", infos)
	}

	st, err := c.State(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Messages) != 1 || st.Messages[0].Text != "cold hello" {
		t.Fatalf("hydrate-on-state should reload transcript, got %+v", st.Messages)
	}
	if builds.Load() != 2 || closes.Load() != 1 || !isLive() {
		t.Fatalf("state should hydrate without closing active resources, builds=%d closes=%d live=%v", builds.Load(), closes.Load(), isLive())
	}
	if !h.UnloadIfInactive(id) {
		t.Fatal("manual inactive unload should close the hydrated session")
	}
	if builds.Load() != 2 || closes.Load() != 2 || !isCold() {
		t.Fatalf("manual unload mismatch: builds=%d closes=%d live=%v", builds.Load(), closes.Load(), isLive())
	}

	if err := c.Attach(id, func(WireEvent, bool) {}); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 3 || closes.Load() != 2 || !isLive() {
		t.Fatalf("attach should rehydrate and keep resources while view is attached: builds=%d closes=%d live=%v", builds.Load(), closes.Load(), isLive())
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if closes.Load() == 3 && isCold() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("closing the last view should unload resources, builds=%d closes=%d live=%v", builds.Load(), closes.Load(), isLive())
}

func TestColdSessionConcurrentUseAndUnload(t *testing.T) {
	persistDir := t.TempDir()
	sock := filepath.Join(t.TempDir(), "d.sock")
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	h := NewPersistentHost(persistDir)
	srv, err := Listen(sock, h, build)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.NewSession("/tmp/proj", "m", "auto", []llm.Message{{Role: llm.RoleUser, Text: "seed"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				switch (worker + i) % 4 {
				case 0:
					cc, err := Dial(sock)
					if err == nil {
						_, _ = cc.State(id)
						_ = cc.Close()
					}
				case 1:
					cc, err := Dial(sock)
					if err == nil {
						_ = cc.Attach(id, func(WireEvent, bool) {})
						_ = cc.Close()
					}
				case 2:
					cc, err := Dial(sock)
					if err == nil {
						_ = cc.Input(id, "turn", nil, nil) // busy is fine; race-free is the assertion
						_ = cc.Close()
					}
				case 3:
					h.UnloadIfInactive(id)
				}
			}
		}()
	}
	wg.Wait()
	waitIdle := func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && h.AnyRunning() {
			time.Sleep(10 * time.Millisecond)
		}
		if h.AnyRunning() {
			t.Fatal("session stayed running after stress")
		}
	}
	waitIdle()

	cc, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	st, err := cc.State(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Messages) == 0 || st.Messages[0].Text != "seed" {
		t.Fatalf("session should remain rehydratable after concurrent use/unload, got %+v", st.Messages)
	}
}

func TestRemoveDeletesPersistedFiles(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	// Write something durable, including backup generations.
	_ = transcript_save_probe(persistDir, s.ID)
	_ = os.WriteFile(transcriptPath(persistDir, s.ID)+".bak", []byte("backup"), 0o644)
	_ = os.WriteFile(transcriptPath(persistDir, s.ID)+".bak.1", []byte("older backup"), 0o644)
	h.Remove(s.ID)
	if _, err := os.Stat(metaPath(persistDir, s.ID)); !os.IsNotExist(err) {
		t.Fatal("meta should be deleted on remove")
	}
	if _, err := os.Stat(transcriptPath(persistDir, s.ID)); !os.IsNotExist(err) {
		t.Fatal("transcript should be deleted on remove")
	}
	if backups, _ := filepath.Glob(transcriptPath(persistDir, s.ID) + ".bak*"); len(backups) != 0 {
		t.Fatalf("backup transcripts should be deleted on remove, got %v", backups)
	}
}

// transcript_save_probe writes a minimal transcript file (helper).
func transcript_save_probe(dir, id string) error {
	return os.WriteFile(transcriptPath(dir, id), []byte(`{"Role":"user","Text":"x"}`+"\n"), 0o644)
}

func TestRestoreDoesNotBuildUntilHydrate(t *testing.T) {
	persistDir := t.TempDir()
	saveMeta(persistDir, persistMeta{ID: "s1", Dir: "/gone", Model: "m"})
	h := NewPersistentHost(persistDir)
	var builds atomic.Int32
	n := h.Restore(func(_, _ string) (*agent.Agent, func(), error) {
		builds.Add(1)
		return nil, nil, context.DeadlineExceeded // any error
	})
	if n != 1 {
		t.Fatalf("restore should keep the cold row without building, got %d", n)
	}
	if builds.Load() != 0 {
		t.Fatalf("restore should not build inactive sessions, builds=%d", builds.Load())
	}
	if s := h.Get("s1"); s == nil || s.agent != nil {
		t.Fatalf("restored session should be present but cold, got %+v", s)
	}
	if err := h.Hydrate("s1"); err == nil {
		t.Fatal("hydrate should surface the deferred build failure")
	}
	if builds.Load() != 1 {
		t.Fatalf("hydrate should build exactly once, builds=%d", builds.Load())
	}
	// Files stay on disk for a later successful hydrate attempt.
	if _, err := os.Stat(metaPath(persistDir, "s1")); err != nil {
		t.Fatal("failed hydrate must keep the persisted files")
	}
}

func TestShutdownKeepsPersistedState(t *testing.T) {
	// Daemon shutdown must NOT delete persisted sessions (that's Remove, the
	// user-facing delete). Regression: shutdown used Remove and wiped disk.
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	h.Shutdown()
	if _, err := os.Stat(metaPath(persistDir, s.ID)); err != nil {
		t.Fatal("shutdown must keep meta on disk")
	}
	if h.Get(s.ID) != nil {
		t.Fatal("shutdown should drop live sessions")
	}
	// And a fresh host restores it.
	h2 := NewPersistentHost(persistDir)
	if n := h2.Restore(persistentBuilder()); n != 1 {
		t.Fatalf("restore after shutdown: %d, want 1", n)
	}
}

func TestAutoTitleOnFirstMessage(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	titled := make(chan string, 1)
	h.SetTitler(func(_ context.Context, head string) (string, error) {
		titled <- head
		return "Test Title", nil
	})
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	// Simulate the agent's persist hook after the first user message.
	s.agent.Persist([]llm.Message{{Role: llm.RoleUser, Text: "make me a parser"}})
	select {
	case head := <-titled:
		if head != "make me a parser" {
			t.Fatalf("titler got %q", head)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("titler not invoked")
	}
	// Title lands asynchronously; wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.info().Title == "Test Title" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.info().Title != "Test Title" {
		t.Fatalf("title = %q", s.info().Title)
	}
	// And it persisted to meta (survives restart).
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ps := loadPersisted(persistDir)
		if len(ps) == 1 && ps[0].meta.Title == "Test Title" {
			h.waitTitles() // ensure no title write is still in flight vs TempDir cleanup
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("title not persisted to meta")
}

func TestNoRetitleOnceTitled(t *testing.T) {
	h := NewPersistentHost(t.TempDir())
	calls := 0
	h.SetTitler(func(_ context.Context, _ string) (string, error) {
		calls++
		return "T", nil
	})
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	s.SetTitle("already named")
	s.agent.Persist([]llm.Message{{Role: llm.RoleUser, Text: "hello"}})
	time.Sleep(100 * time.Millisecond)
	if calls != 0 {
		t.Fatal("titled session must not be re-titled")
	}
}

// TestRestoreBackfillsTitle: a session persisted WITHOUT a title (the titler
// failed or the daemon died before the async title landed) gets titled on
// restore — the bug was untitled sessions staying nameless forever.
func TestRestoreBackfillsTitle(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir) // no titler: session persists untitled
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	s.agent.Persist([]llm.Message{{Role: llm.RoleUser, Text: "fix the parser bug"}})
	if got := loadPersisted(persistDir); len(got) != 1 || got[0].meta.Title != "" {
		t.Fatalf("precondition: want one untitled persisted session, got %+v", got)
	}

	h2 := NewPersistentHost(persistDir)
	titled := make(chan string, 1)
	h2.SetTitler(func(_ context.Context, head string) (string, error) {
		titled <- head
		return "Parser Bug", nil
	})
	if n := h2.Restore(persistentBuilder()); n != 1 {
		t.Fatalf("restore: %d, want 1", n)
	}
	select {
	case head := <-titled:
		if head != "fix the parser bug" {
			t.Fatalf("titler got %q", head)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restore did not backfill the title")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ps := loadPersisted(persistDir)
		if len(ps) == 1 && ps[0].meta.Title == "Parser Bug" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("backfilled title not persisted to meta")
}

// TestInfoTitleFallsBackToSnippet: while no model title exists, listings show
// a snippet of the first user message instead of "(untitled)".
func TestInfoTitleFallsBackToSnippet(t *testing.T) {
	h := NewHost()
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	s.mu.Lock()
	s.sess = s.agent.Resume([]llm.Message{{Role: llm.RoleUser, Text: "refactor the layout engine\nand more"}})
	s.mu.Unlock()
	if got := s.info().Title; got != "refactor the layout engine" {
		t.Fatalf("info title fallback = %q", got)
	}
	s.SetTitle("Real Title")
	if got := s.info().Title; got != "Real Title" {
		t.Fatalf("explicit title wins: %q", got)
	}
}

// TestTitleInFlightGuard: Persist fires after every message; a slow titler
// must not stack duplicate title calls.
func TestTitleInFlightGuard(t *testing.T) {
	h := NewPersistentHost(t.TempDir())
	var calls atomic.Int32
	release := make(chan struct{})
	h.SetTitler(func(ctx context.Context, _ string) (string, error) {
		calls.Add(1)
		<-release
		return "T", nil
	})
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	msgs := []llm.Message{{Role: llm.RoleUser, Text: "hello"}}
	for i := 0; i < 5; i++ {
		s.agent.Persist(msgs)
	}
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("titler called %d times while one call was in flight, want 1", n)
	}
	close(release)
	// Wait for the in-flight title goroutine to finish writing its meta file
	// before the test returns — otherwise t.TempDir() RemoveAll races the
	// write ("directory not empty").
	h.waitTitles()
}

func TestListPersistedPrefersLastAttached(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sd := SessionsDir()
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two sessions: s1 transcript is NEWER on disk, but s2 was attached more
	// recently. Last-used ordering must put s2 ahead of s1.
	for _, id := range []string{"s1", "s2"} {
		if err := transcript.Save(transcriptPath(sd, id), []llm.Message{{Role: llm.RoleUser, Text: "hello from " + id}}); err != nil {
			t.Fatal(err)
		}
	}
	saveMeta(sd, persistMeta{ID: "s1", Dir: "/p", Model: "m", LastAttached: 1000})
	saveMeta(sd, persistMeta{ID: "s2", Dir: "/p", Model: "m", LastAttached: 9999})

	var s1, s2 PersistedInfo
	for _, p := range ListPersisted() {
		switch p.ID {
		case "s1":
			s1 = p
		case "s2":
			s2 = p
		}
	}
	if s1.Updated != 1000 || s2.Updated != 9999 {
		t.Fatalf("LastAttached should drive Updated: s1=%d s2=%d", s1.Updated, s2.Updated)
	}
}

func TestPruneEmptyRemovesOnlyEmptySessions(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()

	// One session with history, two empty. Seed history the way restore does
	// (Resume populates the session), since PruneEmpty checks the live session.
	full := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	full.sess = full.agent.Resume([]llm.Message{{Role: llm.RoleUser, Text: "real work"}})
	full.agent.Persist([]llm.Message{{Role: llm.RoleUser, Text: "real work"}})
	empty1 := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	empty2 := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})

	pruned := h.PruneEmpty()
	if len(pruned) != 2 {
		t.Fatalf("should prune 2 empty sessions, got %d: %v", len(pruned), pruned)
	}
	if h.Get(full.ID) == nil {
		t.Fatal("session with history must survive prune")
	}
	if h.Get(empty1.ID) != nil || h.Get(empty2.ID) != nil {
		t.Fatal("empty sessions should be gone from the host")
	}
	// Files gone for empties, kept for the full one.
	if _, err := os.Stat(metaPath(persistDir, empty1.ID)); err == nil {
		t.Fatal("empty session meta should be deleted")
	}
	if _, err := os.Stat(metaPath(persistDir, full.ID)); err != nil {
		t.Fatal("full session meta must remain")
	}
}

func TestPrunePersistedFilesWhenDaemonDown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sd := SessionsDir()
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	// s1 has history, s2 is empty (meta only, no/empty transcript).
	if err := transcript.Save(transcriptPath(sd, "s1"), []llm.Message{{Role: llm.RoleUser, Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	saveMeta(sd, persistMeta{ID: "s1", Dir: "/p", Model: "m"})
	saveMeta(sd, persistMeta{ID: "s2", Dir: "/p", Model: "m"})

	pruned := PrunePersisted()
	if len(pruned) != 1 || pruned[0] != "s2" {
		t.Fatalf("should prune only s2, got %v", pruned)
	}
	if _, err := os.Stat(metaPath(sd, "s1")); err != nil {
		t.Fatal("s1 must remain")
	}
}

func TestRestoreReappliesAddedRootsAndDropsInvalid(t *testing.T) {
	persistDir := t.TempDir()
	primary := t.TempDir()
	good := t.TempDir()                            // exists at restore → re-applied
	gone := filepath.Join(t.TempDir(), "vanished") // never created → dropped

	saveMeta(persistDir, persistMeta{
		ID: "s1", Dir: primary, Model: "m",
		AddedRoots: []string{good, gone},
	})
	// A Policy-aware builder (rooted at the session dir) so AddDir works.
	build := func(dir, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{
			Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto,
			Policy: tool.NewPolicy(dir),
		}, func() {}, nil
	}
	h := NewPersistentHost(persistDir)
	if n := h.Restore(build); n != 1 {
		t.Fatalf("restore: %d, want 1", n)
	}
	s := h.Get("s1")
	if s == nil {
		t.Fatal("session not restored")
	}
	if s.agent != nil {
		t.Fatal("restored idle sessions should start cold/unloaded")
	}
	if err := h.Hydrate("s1"); err != nil {
		t.Fatal(err)
	}
	roots := s.agent.Roots()
	// primary + good (gone is dropped on re-validation).
	if len(roots) != 2 {
		t.Fatalf("expected primary + the surviving added root, got %v", roots)
	}
	wantGood, _ := filepath.EvalSymlinks(good)
	found := false
	for _, r := range roots {
		if r == filepath.Clean(wantGood) {
			found = true
		}
		if r == filepath.Clean(gone) {
			t.Fatalf("a vanished added root must be dropped on restore, got %v", roots)
		}
	}
	if !found {
		t.Fatalf("the surviving added root should be re-applied, got %v", roots)
	}
}

// THE fix for the silent data-loss bug: Shutdown must flush each session's
// in-memory transcript to disk. The agent loop's persist() only fires at its
// own save points, so a turn in flight (or state added since the last save,
// like a /model switch applied in memory) would be DROPPED on a stop/restart.
// This pins that Shutdown is lossless: messages present in memory but never
// persisted survive via the flush.
func TestShutdownFlushesInMemoryTranscript(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}
	s := h.Add("/tmp", "m", a)

	// Drive a turn so the session's sess + Persist hook are wired and one
	// message lands on disk (the normal save-point path).
	s.mu.Lock()
	s.sess = a.NewSession()
	s.mu.Unlock()
	s.flush()
	// Seed one message and flush it to disk.
	s.mu.Lock()
	s.sess = a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "first turn"}})
	s.mu.Unlock()
	s.flush()
	before, _ := transcript.Load(transcriptPath(persistDir, s.ID))
	if len(before) != 1 {
		t.Fatalf("precondition: want 1 persisted msg, got %d", len(before))
	}

	// Now simulate work that has NOT reached a save point: add more messages in
	// memory only (no flush), as a turn in flight would. Resume replaces the
	// session's message list in memory.
	s.mu.Lock()
	s.sess = a.Resume([]llm.Message{
		{Role: llm.RoleUser, Text: "first turn"},
		{Role: llm.RoleUser, Text: "in-flight work that must not be lost"},
	})
	s.mu.Unlock()
	// (deliberately do NOT call s.flush() — this is the loss window)

	// Shutdown — must flush the in-memory tail to disk.
	h.Shutdown()

	// Reload from disk: BOTH messages must be present (the fix).
	got, err := transcript.Load(transcriptPath(persistDir, s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Shutdown must flush in-memory work: want 2 msgs on disk, got %d (DATA LOSS)", len(got))
	}
}

func TestShutdownFlushesAfterInterruptedTurnUnwinds(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}
	s := h.Add("/tmp", "m", a)

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.sess = a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "before interrupt"}})
	s.running = true
	s.cancel = cancel
	s.mu.Unlock()
	s.flush()

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		s.mu.Lock()
		s.sess = a.Resume([]llm.Message{
			{Role: llm.RoleUser, Text: "before interrupt"},
			{Role: llm.RoleAssistant, Text: "saved while unwinding"},
		})
		s.running = false
		s.cancel = nil
		s.mu.Unlock()
		close(done)
	}()

	h.Shutdown()
	<-done

	got, err := transcript.Load(transcriptPath(persistDir, s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Shutdown must flush after interrupted turn unwinds: want 2 msgs, got %d", len(got))
	}
}

func TestShutdownFlushesPendingSteer(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}
	s := h.Add("/tmp", "m", a)

	s.mu.Lock()
	s.sess = a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "already persisted"}})
	s.mu.Unlock()
	s.flush()

	// Simulate user input steered into a running turn but not yet drained by the
	// agent loop. Shutdown must not lose it.
	s.sess.Steer("queued follow-up", nil)
	h.Shutdown()

	got, err := transcript.Load(transcriptPath(persistDir, s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Text != "queued follow-up" {
		t.Fatalf("shutdown should persist pending steer, got %#v", got)
	}
}

// TestCumulativeTokensSurviveRestart: the lifetime token tallies (used for the
// prompt-cache hit ratio in daemon stats) must be persisted in meta and reloaded
// on restore. The bug was that they rebuilt from 0 after a restart, so the
// cache-hit ratio collapsed to 0% and re-climbed (read as a regression).
func TestCumulativeTokensSurviveRestart(t *testing.T) {
	persistDir := t.TempDir()

	// Daemon #1: a live session accrues token usage, then persists meta.
	h1 := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	s := h1.Add("/tmp/proj", "echo-model", &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto})
	s.mu.Lock()
	s.cumIn, s.cumOut, s.cumCacheRead, s.cumCacheWrite = 1000, 200, 800, 50
	s.mu.Unlock()
	h1.saveSessionMeta(s)

	// It landed in the sidecar meta.
	if got := loadPersisted(persistDir); len(got) != 1 ||
		got[0].meta.CumIn != 1000 || got[0].meta.CumOut != 200 ||
		got[0].meta.CumCacheRead != 800 || got[0].meta.CumCacheWrite != 50 {
		t.Fatalf("cumulative tokens not persisted to meta: %+v", got)
	}

	// Daemon #2: restore — the cold row carries the tallies forward, so Stats
	// reports the same totals instead of resetting to 0.
	h2 := NewPersistentHost(persistDir)
	if n := h2.Restore(persistentBuilder()); n != 1 {
		t.Fatalf("restore: %d, want 1", n)
	}
	st := h2.Stats()
	if st.InputTokens != 1000 || st.OutputTokens != 200 ||
		st.CacheReadTokens != 800 || st.CacheWriteTokens != 50 {
		t.Fatalf("restored stats lost token tallies: in=%d out=%d cacheRead=%d cacheWrite=%d",
			st.InputTokens, st.OutputTokens, st.CacheReadTokens, st.CacheWriteTokens)
	}
}

// TestTurnDonePersistsCumulativeTokens: an EventDone with token usage bumps the
// session's lifetime tallies AND persists them (via the onTokens hook) so a
// restart immediately after a turn doesn't lose the just-finished turn's tokens.
func TestTurnDonePersistsCumulativeTokens(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto})

	s.dispatch(agent.Event{Kind: agent.EventDone, InTokens: 500, OutTokens: 120, CacheReadTokens: 400, CacheWriteTokens: 30})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ps := loadPersisted(persistDir)
		if len(ps) == 1 && ps[0].meta.CumIn == 500 && ps[0].meta.CumCacheRead == 400 {
			if ps[0].meta.CumOut != 120 || ps[0].meta.CumCacheWrite != 30 {
				t.Fatalf("partial token persist: %+v", ps[0].meta)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("EventDone did not persist cumulative tokens to meta")
}

func TestClearPersistsEmptyTranscript(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: echoProvider{}, Tools: reg, Perm: agent.PermAuto}
	s := h.Add("/tmp", "m", a)

	s.mu.Lock()
	s.sess = a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "old conversation"}})
	s.mu.Unlock()
	s.flush()

	s.clear()
	got, err := transcript.Load(transcriptPath(persistDir, s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("clear must persist an empty transcript immediately, got %#v", got)
	}
}
