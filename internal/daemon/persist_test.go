package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	c1.Close()
	srv1.Close() // "kill" the daemon (files stay)

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

func TestRemoveDeletesPersistedFiles(t *testing.T) {
	persistDir := t.TempDir()
	h := NewPersistentHost(persistDir)
	reg, _ := tool.NewRegistry()
	s := h.Add("/tmp", "m", &agent.Agent{Provider: echoProvider{}, Tools: reg})
	// Write something durable.
	_ = transcript_save_probe(persistDir, s.ID)
	h.Remove(s.ID)
	if _, err := os.Stat(metaPath(persistDir, s.ID)); !os.IsNotExist(err) {
		t.Fatal("meta should be deleted on remove")
	}
	if _, err := os.Stat(transcriptPath(persistDir, s.ID)); !os.IsNotExist(err) {
		t.Fatal("transcript should be deleted on remove")
	}
}

// transcript_save_probe writes a minimal transcript file (helper).
func transcript_save_probe(dir, id string) error {
	return os.WriteFile(transcriptPath(dir, id), []byte(`{"Role":"user","Text":"x"}`+"\n"), 0o644)
}

func TestRestoreSkipsBrokenBuilds(t *testing.T) {
	persistDir := t.TempDir()
	saveMeta(persistDir, persistMeta{ID: "s1", Dir: "/gone", Model: "m"})
	h := NewPersistentHost(persistDir)
	n := h.Restore(func(_, _ string) (*agent.Agent, func(), error) {
		return nil, nil, context.DeadlineExceeded // any error
	})
	if n != 0 {
		t.Fatal("broken build must not restore")
	}
	// Files stay on disk for a later attempt.
	if _, err := os.Stat(metaPath(persistDir, "s1")); err != nil {
		t.Fatal("failed restore must keep the persisted files")
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
