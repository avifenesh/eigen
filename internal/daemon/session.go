// Package daemon is the long-lived session host: the REAL eigen app. It owns
// agent sessions (each a whole chat rooted at its own directory), keeps them
// running whether or not any window is attached, and serves views over a Unix
// socket. Terminal windows are thin clients that attach, mirror events, and
// send input; a session's lifetime is independent of any view.
//
// This package is transport + lifecycle only. The actual agent for a session
// is built by the caller (package main's buildSession) and handed in via
// NewSession, so daemon need not know how tools/providers are wired.
package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
)

// Status is a session's live state, shown in the app rail.
type Status string

const (
	StatusIdle     Status = "idle"
	StatusWorking  Status = "working"
	StatusApproval Status = "approval" // blocked awaiting an approval answer
	StatusError    Status = "error"
)

// SessionInfo is the metadata a view needs to list/choose sessions.
type SessionInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Dir     string `json:"dir"`
	Model   string `json:"model"`
	Status  Status `json:"status"`
	Turns   int    `json:"turns"`
	Updated int64  `json:"updated"` // unix nano
}

// Session is one hosted chat: an agent session plus the bookkeeping the daemon
// needs to multiplex views onto it (event fan-out, status, replay buffer).
type Session struct {
	ID    string
	Dir   string
	Model string

	mu      sync.Mutex
	agent   *agent.Agent
	sess    *agent.Session
	status  Status
	title   string
	updated time.Time

	// events is the append-only log of this session's events, so a view that
	// attaches mid-run can replay history and then follow live.
	events  []agent.Event
	subs    map[int]chan agent.Event // attached views
	nextSub int

	cancel  context.CancelFunc // cancels the in-flight turn (interrupt)
	running bool
	onClose func() // releases the session's external resources (MCP/LSP/observe)
}

// newSession wraps a built agent as a hosted session.
func newSession(id, dir, model string, a *agent.Agent) *Session {
	s := &Session{
		ID:      id,
		Dir:     dir,
		Model:   model,
		agent:   a,
		sess:    a.NewSession(),
		status:  StatusIdle,
		updated: time.Now(),
		subs:    map[int]chan agent.Event{},
	}
	// Fan out agent events to all attached views + record for replay.
	a.OnEvent = s.dispatch
	return s
}

// dispatch records an event and fans it out to attached views.
func (s *Session) dispatch(e agent.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	switch e.Kind {
	case agent.EventToolStart, agent.EventTextDelta, agent.EventReasoningDelta:
		s.status = StatusWorking
	case agent.EventDone:
		s.status = StatusIdle
	}
	s.updated = time.Now()
	subs := make([]chan agent.Event, 0, len(s.subs))
	for _, ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default: // a slow view must not stall the agent loop; it can resync
		}
	}
}

// attach registers a view: returns a replay of events so far plus a live
// channel and an unsubscribe func.
func (s *Session) attach() (replay []agent.Event, live <-chan agent.Event, detach func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replay = append(replay, s.events...)
	ch := make(chan agent.Event, 256)
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	return replay, ch, func() {
		s.mu.Lock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
		s.mu.Unlock()
	}
}

// send runs a turn on the session (one at a time). It returns immediately;
// progress arrives via events. A turn already running is rejected.
func (s *Session) send(task string) bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.status = StatusWorking
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		_, err := s.sess.Send(ctx, task)
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		if err != nil && ctx.Err() == nil {
			s.status = StatusError
		} else {
			s.status = StatusIdle
		}
		s.mu.Unlock()
	}()
	return true
}

// interrupt cancels the in-flight turn, if any.
func (s *Session) interrupt() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// info snapshots the session for listing.
func (s *Session) info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionInfo{
		ID:      s.ID,
		Title:   s.title,
		Dir:     s.Dir,
		Model:   s.Model,
		Status:  s.status,
		Turns:   len(s.sess.Messages()),
		Updated: s.updated.UnixNano(),
	}
}

// SetTitle updates the session's display title.
func (s *Session) SetTitle(t string) {
	s.mu.Lock()
	s.title = t
	s.mu.Unlock()
}

var _ = llm.RoleUser // keep llm imported for future message-typed protocol
