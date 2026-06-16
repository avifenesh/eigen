package daemon

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

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
	// switchModel rebuilds a provider+compactor+budget for a live /model
	// switch, injected by package main (which owns provider construction).
	switchModel ModelSwitcher
	// titler names untitled sessions from their first user message on a
	// small model (injected by main; nil = no auto-titles).
	titler func(ctx context.Context, head string) (string, error)
	// notify fires a desktop notification for a backgrounded turn that
	// finishes with no views attached (injected by main from notify_cmd).
	notify func(title, body string)
	// titleWG tracks in-flight background titling goroutines so they can be
	// awaited (tests; clean shutdown) — a fire-and-forget goroutine that writes
	// the meta file must not outlive a RemoveAll/teardown.
	titleWG sync.WaitGroup
	// started is the host creation time (daemon uptime for the stats op).
	started time.Time
	// bgCount reports the in-memory background-task record count (injected by
	// main, which owns the BgRegistry); nil = unknown.
	bgCount func() int
}

// SetTitler installs the small-model session titler.
func (h *Host) SetTitler(t func(ctx context.Context, head string) (string, error)) { h.titler = t }

// SetNotifier installs the desktop notifier used to alert the user when a
// BACKGROUNDED turn (no views attached) finishes. nil = no notifications.
func (h *Host) SetNotifier(n func(title, body string)) { h.notify = n }

// maybeTitle titles an untitled session from its first user message, in the
// background, persisting the result. Cheap no-ops: already titled, no titler,
// or a title request already in flight (Persist fires after every message —
// without the guard a slow titler stacks duplicate calls).
func (h *Host) maybeTitle(s *Session, msgs []llm.Message) {
	if h.titler == nil {
		return
	}
	s.mu.Lock()
	busy := s.title != "" || s.titling
	if !busy {
		s.titling = true
	}
	s.mu.Unlock()
	if busy {
		return
	}
	done := func() {
		s.mu.Lock()
		s.titling = false
		s.mu.Unlock()
	}
	var head string
	for _, m := range msgs {
		if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
			head = m.Text
			break
		}
	}
	if head == "" {
		done()
		return
	}
	h.titleWG.Add(1)
	go func() {
		defer h.titleWG.Done()
		defer done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		title, err := h.titler(ctx, head)
		if err != nil || strings.TrimSpace(title) == "" {
			// Stay diagnosable: silent failures left sessions untitled forever
			// with no trace. The next Persist (or daemon restart) retries.
			if err != nil {
				fmt.Fprintf(os.Stderr, "eigen daemon: title %s: %v\n", s.ID, err)
			}
			return
		}
		s.SetTitle(title)
		h.saveSessionMeta(s)
	}()
}

// waitTitles blocks until all in-flight background titling goroutines finish
// (and have written their meta files). Used by tests and clean teardown so a
// title write never races a directory removal.
func (h *Host) waitTitles() { h.titleWG.Wait() }

// ModelSwitcher builds the live-switch inputs for a model id rooted at dir.
type ModelSwitcher func(dir, modelID string) (provider llm.Provider, compactor llm.Compactor, budget int, err error)

// SetModelSwitcher installs the live-model-switch builder.
func (h *Host) SetModelSwitcher(s ModelSwitcher) { h.switchModel = s }

// NewHost creates an empty session host (no persistence — tests).
func NewHost() *Host {
	return &Host{sessions: map[string]*Session{}, started: time.Now()}
}

// NewPersistentHost creates a host that persists sessions under dir.
func NewPersistentHost(dir string) *Host {
	return &Host{sessions: map[string]*Session{}, persistDir: dir, started: time.Now()}
}

// SetBgCount injects a reporter for the in-memory background-task record count
// (the BgRegistry lives in package main / the agent layer).
func (h *Host) SetBgCount(f func() int) { h.bgCount = f }

// Stats returns the daemon's resource-health snapshot for the `stats` op.
func (h *Host) Stats() DaemonStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	h.mu.Lock()
	sessions := len(h.sessions)
	views, running := 0, 0
	var cumIn, cumOut, cumCacheRead, cumCacheWrite int64
	for _, s := range h.sessions {
		s.mu.Lock()
		views += len(s.subs)
		if s.running {
			running++
		}
		cumIn += s.cumIn
		cumOut += s.cumOut
		cumCacheRead += s.cumCacheRead
		cumCacheWrite += s.cumCacheWrite
		s.mu.Unlock()
	}
	started := h.started
	bgCount := h.bgCount
	h.mu.Unlock()
	st := DaemonStats{
		Goroutines:       runtime.NumGoroutine(),
		HeapAllocB:       ms.HeapAlloc,
		HeapSysB:         ms.HeapSys,
		RSSB:             currentRSS(),
		NumGC:            ms.NumGC,
		Sessions:         sessions,
		Views:            views,
		RunningTurns:     running,
		GoVersion:        runtime.Version(),
		InputTokens:      cumIn,
		OutputTokens:     cumOut,
		CacheReadTokens:  cumCacheRead,
		CacheWriteTokens: cumCacheWrite,
	}
	if !started.IsZero() {
		st.UptimeSec = int64(time.Since(started).Seconds())
	}
	if bgCount != nil {
		st.BgTasks = bgCount()
	}
	return st
}

// Add registers a freshly built agent as a hosted session and returns it. dir
// and model are recorded for listing; the caller built the agent rooted at dir.
func (h *Host) Add(dir, model string, a *agent.Agent) *Session {
	h.mu.Lock()
	h.seq++
	id := fmt.Sprintf("s%d", h.seq)
	s := newSession(id, dir, model, a)
	s.notify = h.notify
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
		if err := transcript.Save(transcriptPath(dir, s.ID), msgs); err != nil {
			fmt.Fprintf(os.Stderr, "eigen daemon: persist %s: %v\n", s.ID, err)
			return
		}
		h.maybeTitle(s, msgs)
	}
	s.onAttach = func() { h.saveSessionMeta(s) } // persist LastAttached
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
	// Persist user-granted extra roots (everything past the primary, which is
	// the session Dir that build() already roots at).
	if roots := s.agent.Roots(); len(roots) > 1 {
		m.AddedRoots = append([]string(nil), roots[1:]...)
	}
	if !s.lastAttached.IsZero() {
		m.LastAttached = s.lastAttached.Unix()
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
		// Re-apply user-granted extra roots; AddDir re-validates (existence +
		// not-denied), so a vanished or now-sensitive path silently drops.
		for _, root := range p.meta.AddedRoots {
			if _, err := a.AddDir(root); err != nil {
				fmt.Fprintf(os.Stderr, "eigen daemon: restore %s: drop added dir %s: %v\n", p.meta.ID, root, err)
			}
		}
		s := newSession(p.meta.ID, p.meta.Dir, p.meta.Model, a)
		s.notify = h.notify
		s.title = p.meta.Title
		if p.meta.LastAttached > 0 {
			s.lastAttached = time.Unix(p.meta.LastAttached, 0) // survives restart
		}
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
		// Backfill titles for sessions that never got one (titler failed or
		// the daemon died before the async title landed): maybeTitle no-ops
		// when already titled, so this is cheap for the rest.
		h.maybeTitle(s, p.history)
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

// Shutdown releases every session's resources (interrupting in-flight turns,
// closing MCP/LSP) WITHOUT touching persisted state — the next daemon start
// restores them. This is daemon shutdown; Remove is user deletion.
func (h *Host) Shutdown() {
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.sessions = map[string]*Session{}
	h.mu.Unlock()
	for _, s := range sessions {
		// Lossless shutdown: first persist the current in-memory transcript,
		// then cancel any in-flight turn, wait briefly for it to unwind, and
		// persist again. The agent loop's normal persist() only fires at its
		// save points; without this shutdown flush, a stop/restart can silently
		// drop a turn in flight or a /model switch applied only in memory.
		s.flush()
		s.interrupt()
		s.waitUntilIdle(2 * time.Second)
		s.flush()
		if s.onClose != nil {
			s.onClose()
		}
	}
}

// Remove stops hosting a session (interrupting any in-flight turn) and
// DELETES its persisted state — this is the user-facing "delete session".
// PruneEmpty removes hosted sessions that have no conversation (0 messages):
// interrupts them (no-op for idle), releases their resources, and deletes
// their durable files. Returns the pruned ids. The CURRENT in-memory empties
// are dropped too, so a running daemon won't re-persist a ghost. Never touches
// a session with any history or a running turn.
func (h *Host) PruneEmpty() []string {
	h.mu.Lock()
	var victims []*Session
	for id, s := range h.sessions {
		if s.running {
			continue // never prune a session mid-turn
		}
		if s.sess != nil && len(s.sess.Messages()) > 0 {
			continue
		}
		delete(h.sessions, id)
		victims = append(victims, s)
	}
	h.mu.Unlock()
	var pruned []string
	for _, s := range victims {
		s.interrupt()
		if s.onClose != nil {
			s.onClose()
		}
		if h.persistDir != "" {
			removePersisted(h.persistDir, s.ID)
		}
		pruned = append(pruned, s.ID)
	}
	return pruned
}

// Remove stops hosting a session (interrupting any in-flight turn) and DELETES
// its persisted state — this is the user-facing "delete session".
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

// AnyRunning reports whether any hosted session has a turn in flight. Used by
// the nightly dreamer to only reflect when the machine is idle (never compete
// with a live turn for the model).
func (h *Host) AnyRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		s.mu.Lock()
		r := s.running
		s.mu.Unlock()
		if r {
			return true
		}
	}
	return false
}
