package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/tool"
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
