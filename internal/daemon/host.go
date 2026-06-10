package daemon

import (
	"fmt"
	"os"
	"sync"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

// Host owns all live sessions. It is the in-memory core of the daemon; the
// socket server (server.go) exposes it to views. When persistDir is set,
// every session streams its transcript + meta there and Restore resurrects
// them on daemon start — killing the daemon loses nothing.
type Host struct {
	mu         sync.Mutex
	sessions   map[string]*Session
	seq        int
	persistDir string
}

// NewHost creates an empty session host (no persistence — tests).
func NewHost() *Host {
	return &Host{sessions: map[string]*Session{}}
}

// NewPersistentHost creates a host that persists sessions under dir.
func NewPersistentHost(dir string) *Host {
	return &Host{sessions: map[string]*Session{}, persistDir: dir}
}

// Add registers a freshly built agent as a hosted session and returns it. dir
// and model are recorded for listing; the caller built the agent rooted at dir.
func (h *Host) Add(dir, model string, a *agent.Agent) *Session {
	h.mu.Lock()
	h.seq++
	id := fmt.Sprintf("s%d", h.seq)
	s := newSession(id, dir, model, a)
	h.sessions[id] = s
	h.mu.Unlock()
	h.enablePersist(s)
	return s
}

// enablePersist hooks continuous transcript + meta saving onto a session.
func (h *Host) enablePersist(s *Session) {
	if h.persistDir == "" {
		return
	}
	dir := h.persistDir
	_ = os.MkdirAll(dir, 0o755)
	// The agent's Persist hook runs in the agent goroutine after every
	// appended message — the same continuous autosave as the local chat.
	s.agent.Persist = func(msgs []llm.Message) {
		_ = transcript.Save(transcriptPath(dir, s.ID), msgs)
	}
	h.saveSessionMeta(s)
}

// saveSessionMeta snapshots a session's resurrect state (call after mutations).
func (h *Host) saveSessionMeta(s *Session) {
	if h.persistDir == "" {
		return
	}
	s.mu.Lock()
	m := persistMeta{
		ID:    s.ID,
		Dir:   s.Dir,
		Model: s.Model,
		Title: s.title,
		Perm:  string(s.agent.Perm),
		Goal:  s.agent.CurrentGoal(),
	}
	s.mu.Unlock()
	saveMeta(h.persistDir, m)
}

// Restore resurrects persisted sessions: for each saved meta it rebuilds the
// agent (rooted at the session's dir) with build, resumes the saved history,
// and re-registers under the SAME id so views reattach where they were. A
// session whose agent fails to build (e.g. credentials gone) is skipped and
// kept on disk. Returns how many sessions were restored.
func (h *Host) Restore(build Builder) int {
	if h.persistDir == "" {
		return 0
	}
	restored := 0
	for _, p := range loadPersisted(h.persistDir) {
		a, closeFn, err := build(p.meta.Dir, p.meta.Model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "eigen daemon: restore %s: %v\n", p.meta.ID, err)
			continue
		}
		if p.meta.Perm != "" {
			a.SetPerm(agent.Permission(p.meta.Perm))
		}
		if p.meta.Goal != "" {
			a.SetGoal(p.meta.Goal)
		}
		s := newSession(p.meta.ID, p.meta.Dir, p.meta.Model, a)
		s.title = p.meta.Title
		s.onClose = closeFn
		if len(p.history) > 0 {
			s.sess = a.Resume(p.history)
		}
		h.mu.Lock()
		h.sessions[s.ID] = s
		if n := idNum(s.ID); n > h.seq {
			h.seq = n // new sessions continue after the restored ids
		}
		h.mu.Unlock()
		h.enablePersist(s)
		restored++
	}
	return restored
}

// Get returns a session by id (nil if absent).
func (h *Host) Get(id string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

// Remove stops hosting a session (interrupting any in-flight turn).
func (h *Host) Remove(id string) bool {
	h.mu.Lock()
	s := h.sessions[id]
	delete(h.sessions, id)
	h.mu.Unlock()
	if s == nil {
		return false
	}
	s.interrupt()
	if s.onClose != nil {
		s.onClose()
	}
	if h.persistDir != "" {
		removePersisted(h.persistDir, id)
	}
	return true
}

// List snapshots all sessions for the rail (most recently updated first).
func (h *Host) List() []SessionInfo {
	h.mu.Lock()
	out := make([]SessionInfo, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s.info())
	}
	h.mu.Unlock()
	// newest first
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Updated > out[i].Updated {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Count returns the number of hosted sessions.
func (h *Host) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}
