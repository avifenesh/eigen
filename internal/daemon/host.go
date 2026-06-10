package daemon

import (
	"fmt"
	"sync"

	"github.com/avifenesh/eigen/internal/agent"
)

// Host owns all live sessions. It is the in-memory core of the daemon; the
// socket server (server.go) exposes it to views.
type Host struct {
	mu       sync.Mutex
	sessions map[string]*Session
	seq      int
}

// NewHost creates an empty session host.
func NewHost() *Host {
	return &Host{sessions: map[string]*Session{}}
}

// Add registers a freshly built agent as a hosted session and returns it. dir
// and model are recorded for listing; the caller built the agent rooted at dir.
func (h *Host) Add(dir, model string, a *agent.Agent) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	id := fmt.Sprintf("s%d", h.seq)
	s := newSession(id, dir, model, a)
	h.sessions[id] = s
	return s
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
