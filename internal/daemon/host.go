package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
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
	// builder rebuilds a cold/unloaded session's agent on demand. The socket
	// server installs it from Listen, and Restore stores it for resurrected rows.
	builder Builder
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

// SetBuilder installs the session builder used to rehydrate unloaded sessions.
func (h *Host) SetBuilder(b Builder) { h.builder = b }

// SetBgCount injects a reporter for the in-memory background-task record count
// (the BgRegistry lives in package main / the agent layer).
func (h *Host) SetBgCount(f func() int) { h.bgCount = f }

var buildIdentity struct {
	once        sync.Once
	executable  string
	binarySHA   string
	vcsRevision string
	vcsModified bool
}

func daemonBuildIdentity() (exe, sha, rev string, modified bool) {
	buildIdentity.once.Do(func() {
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			buildIdentity.executable = exe
			if f, err := os.Open(exe); err == nil {
				h := sha256.New()
				_, _ = io.Copy(h, f)
				_ = f.Close()
				buildIdentity.binarySHA = hex.EncodeToString(h.Sum(nil))
			}
		}
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					buildIdentity.vcsRevision = s.Value
				case "vcs.modified":
					buildIdentity.vcsModified = s.Value == "true"
				}
			}
		}
	})
	return buildIdentity.executable, buildIdentity.binarySHA, buildIdentity.vcsRevision, buildIdentity.vcsModified
}

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
	exe, sha, rev, modified := daemonBuildIdentity()
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
		Version:          llm.FullVersion(),
		Executable:       exe,
		BinarySHA256:     sha,
		VCSRevision:      rev,
		VCSModified:      modified,
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
	if a != nil {
		s.coldPerm = string(a.CurrentPerm())
		s.coldGoal = a.CurrentGoal()
	}
	h.sessions[id] = s
	h.mu.Unlock()
	h.enablePersist(s)
	return s
}

// enablePersist hooks continuous transcript + meta saving onto a session.
func (h *Host) enablePersist(s *Session) {
	if h.persistDir == "" || s.agent == nil {
		return
	}
	dir := h.persistDir
	_ = os.MkdirAll(dir, 0o755)
	// The agent's Persist hook runs in the agent goroutine after every
	// appended message — the same continuous autosave as the local chat.
	s.agent.Persist = func(msgs []llm.Message) {
		s.persistMu.Lock()
		err := transcript.Save(transcriptPath(dir, s.ID), msgs)
		s.persistMu.Unlock()
		if err != nil {
			fmt.Fprintf(os.Stderr, "eigen daemon: persist %s: %v\n", s.ID, err)
			return
		}
		h.maybeTitle(s, msgs)
		s.rememberHistorySummary(msgs)
	}
	s.onAttach = func() { h.saveSessionMeta(s) } // persist LastAttached
	s.onTokens = func() { h.saveSessionMeta(s) } // persist cumulative tokens on turn done
	// onClear deliberately empties the transcript: a plain Save([]) is refused as
	// an accidental truncation, so force the empty write, then purge the rotated
	// backups or Load's recovery would resurrect the just-cleared conversation.
	s.onClear = func() {
		p := transcriptPath(dir, s.ID)
		s.persistMu.Lock()
		err := transcript.SaveForce(p, nil)
		s.persistMu.Unlock()
		if err != nil {
			fmt.Fprintf(os.Stderr, "eigen daemon: clear %s: %v\n", s.ID, err)
		}
		transcript.ClearBackups(p)
	}
	s.onInactive = func() { h.UnloadIfInactive(s.ID) }
	h.saveSessionMeta(s)
}

// rememberHistorySummary stores just enough transcript metadata to list a cold
// session without keeping its full []llm.Message in heap.
func (s *Session) rememberHistorySummary(msgs []llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = len(msgs)
	if s.fallbackTitle == "" {
		for _, m := range msgs {
			if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
				s.fallbackTitle = snippet(m.Text, 48)
				break
			}
		}
	}
}

// hydrateLocked reopens an unloaded session. s.loadMu must be held by the
// caller so a racing detach/finish cannot unload between hydration and the
// operation that needs the live agent.
func (h *Host) hydrateLocked(s *Session) error {
	s.mu.Lock()
	if s.agent != nil && s.sess != nil {
		s.mu.Unlock()
		return nil
	}
	dir, model := s.Dir, s.Model
	perm, goal := s.coldPerm, s.coldGoal
	roots := append([]string(nil), s.coldRoots...)
	s.mu.Unlock()
	if h.builder == nil {
		return fmt.Errorf("session %s is unloaded and no builder is installed", s.ID)
	}
	a, closeFn, err := h.builder(dir, model)
	if err != nil {
		return err
	}
	if model == "" && a.Provider != nil {
		model = a.Provider.ModelID()
	}
	if perm != "" {
		a.SetPerm(agent.Permission(perm))
	}
	if goal != "" {
		a.SetGoal(goal)
	}
	for _, root := range roots {
		if _, err := a.AddDir(root); err != nil {
			fmt.Fprintf(os.Stderr, "eigen daemon: hydrate %s: drop added dir %s: %v\n", s.ID, root, err)
		}
	}
	var history []llm.Message
	if h.persistDir != "" {
		history, _ = transcript.Load(transcriptPath(h.persistDir, s.ID))
	}
	var as *agent.Session
	if len(history) > 0 {
		as = a.Resume(history)
	} else {
		as = a.NewSession()
	}
	s.mu.Lock()
	s.Model = model
	s.bindAgent(a, as)
	s.onClose = closeFn
	s.status = StatusIdle
	s.running = false
	s.cancel = nil
	s.approvals = nil
	s.events = nil
	s.turns = len(history)
	s.coldRoots = nil
	s.mu.Unlock()
	s.rememberHistorySummary(history)
	h.enablePersist(s)
	h.maybeTitle(s, history)
	return nil
}

// Hydrate ensures a session has a live agent. Most callers should hold the
// session's loadMu across their immediate operation; this helper is for tests
// and low-risk control paths.
func (h *Host) Hydrate(id string) error {
	s := h.Get(id)
	if s == nil {
		return fmt.Errorf("no such session")
	}
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	return h.hydrateLocked(s)
}

// UnloadIfInactive closes a session's heavyweight resources when it has no
// attached views and no turn in flight. The durable transcript/meta stay on
// disk, and Hydrate reopens the session on demand.
func (h *Host) UnloadIfInactive(id string) bool {
	s := h.Get(id)
	if s == nil {
		return false
	}
	s.loadMu.Lock()
	defer s.loadMu.Unlock()

	s.mu.Lock()
	if s.agent == nil || s.sess == nil || s.running || len(s.subs) > 0 || sessionHasRunningShells(s.agent) {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	s.flush()

	s.mu.Lock()
	if s.agent == nil || s.sess == nil || s.running || len(s.subs) > 0 || sessionHasRunningShells(s.agent) {
		s.mu.Unlock()
		return false
	}
	history := s.sess.Messages()
	s.turns = len(history)
	s.fallbackTitle = ""
	for _, m := range history {
		if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
			s.fallbackTitle = snippet(m.Text, 48)
			break
		}
	}
	s.coldPerm = string(s.agent.CurrentPerm())
	s.coldGoal = s.agent.CurrentGoal()
	s.coldRoots = nil
	if roots := s.agent.Roots(); len(roots) > 1 {
		s.coldRoots = append([]string(nil), roots[1:]...)
	}
	closeFn := s.onClose
	s.agent = nil
	s.sess = nil
	s.onClose = nil
	s.events = nil
	s.approvals = nil
	s.cancel = nil
	s.status = StatusIdle
	s.mu.Unlock()
	if closeFn != nil {
		closeFn()
	}
	return true
}

func sessionHasRunningShells(a *agent.Agent) bool {
	return a != nil && a.Shells != nil && a.Shells.RunningCount() > 0
}

// saveSessionMeta snapshots a session's resurrect state (call after mutations).
func (h *Host) saveSessionMeta(s *Session) {
	if h.persistDir == "" {
		return
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	s.mu.Lock()
	perm, goal := s.coldPerm, s.coldGoal
	var roots []string
	if s.agent != nil {
		perm = string(s.agent.CurrentPerm())
		goal = s.agent.CurrentGoal()
		if rs := s.agent.Roots(); len(rs) > 1 {
			roots = append([]string(nil), rs[1:]...)
		}
	} else {
		roots = append([]string(nil), s.coldRoots...)
	}
	m := persistMeta{
		ID:    s.ID,
		Dir:   s.Dir,
		Model: s.Model,
		Title: s.title,
		Perm:  perm,
		Goal:  goal,
		// Carry the lifetime token tallies so the cache-hit ratio in stats
		// survives a restart instead of resetting to 0.
		CumIn:         s.cumIn,
		CumOut:        s.cumOut,
		CumCacheRead:  s.cumCacheRead,
		CumCacheWrite: s.cumCacheWrite,
	}
	// Persist user-granted extra roots (everything past the primary, which is
	// the session Dir that build() already roots at).
	m.AddedRoots = roots
	if !s.lastAttached.IsZero() {
		m.LastAttached = s.lastAttached.Unix()
	}
	s.mu.Unlock()
	saveMeta(h.persistDir, m)
}

// Restore resurrects persisted sessions as cold rows: it loads only metadata
// plus a tiny transcript summary for listing/title backfill, but does NOT build
// providers, tools, MCP servers, LSPs, or full live agents. Attach/input/state
// rehydrates the session under the SAME id when needed. Returns how many rows
// were restored.
func (h *Host) Restore(build Builder) int {
	if h.persistDir == "" {
		return 0
	}
	h.builder = build
	restored := 0
	for _, p := range loadPersisted(h.persistDir) {
		s := newColdSession(p.meta.ID, p.meta.Dir, p.meta.Model)
		s.notify = h.notify
		s.title = p.meta.Title
		s.coldPerm = p.meta.Perm
		s.coldGoal = p.meta.Goal
		s.coldRoots = append([]string(nil), p.meta.AddedRoots...)
		// Restore lifetime token tallies so the cache-hit ratio in stats picks up
		// where it left off rather than collapsing to 0% after a restart.
		s.cumIn = p.meta.CumIn
		s.cumOut = p.meta.CumOut
		s.cumCacheRead = p.meta.CumCacheRead
		s.cumCacheWrite = p.meta.CumCacheWrite
		if p.meta.LastAttached > 0 {
			s.lastAttached = time.Unix(p.meta.LastAttached, 0) // survives restart
		}
		s.onAttach = func() { h.saveSessionMeta(s) } // persist LastAttached
		s.onInactive = func() { h.UnloadIfInactive(s.ID) }
		s.rememberHistorySummary(p.history)
		h.mu.Lock()
		h.sessions[s.ID] = s
		if n := idNum(s.ID); n > h.seq {
			h.seq = n // new sessions continue after the restored ids
		}
		h.mu.Unlock()
		// Backfill titles for sessions that never got one (titler failed or
		// the daemon died before the async title landed): maybeTitle no-ops
		// when already titled, so this is cheap for the rest.
		h.maybeTitle(s, p.history)
		restored++
	}
	if restored > 0 {
		runtime.GC()
	}
	return restored
}

// Get returns a session by id (nil if absent).
func (h *Host) Get(id string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

func (h *Host) isCurrent(id string, s *Session) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id] == s
}

// Shutdown releases every session's resources (interrupting in-flight turns,
// closing MCP/LSP) WITHOUT touching persisted state — the next daemon start
// restores them. This is daemon shutdown; Remove is user deletion.
func (h *Host) Shutdown() {
	// Drain in-flight background titler goroutines first: each ends in a meta
	// save (maybeTitle → saveSessionMeta), and a save that lands AFTER shutdown
	// returns races a fresh daemon's Restore or a TempDir cleanup. waitTitles
	// makes the "lossless shutdown" promise below actually hold for meta too.
	h.waitTitles()
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.sessions = map[string]*Session{}
	h.mu.Unlock()
	for _, s := range sessions {
		s.loadMu.Lock()
		// Lossless shutdown: first persist the current in-memory transcript,
		// then cancel any in-flight turn, wait briefly for it to unwind, and
		// persist again. The agent loop's normal persist() only fires at its
		// save points; without this shutdown flush, a stop/restart can silently
		// drop a turn in flight or a /model switch applied only in memory.
		if s.agent != nil && s.sess != nil {
			s.flush()
			s.interrupt()
			s.waitUntilIdle(2 * time.Second)
			s.flush()
		}
		if s.onClose != nil {
			s.onClose()
		}
		s.loadMu.Unlock()
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
	candidates := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		candidates = append(candidates, s)
	}
	h.mu.Unlock()
	var pruned []string
	for _, s := range candidates {
		s.loadMu.Lock()
		s.mu.Lock()
		running := s.running
		turns := s.turns
		if s.sess != nil {
			turns = len(s.sess.Messages())
		}
		s.mu.Unlock()
		if running || turns > 0 {
			s.loadMu.Unlock()
			continue // never prune a non-empty session or one mid-turn
		}
		h.mu.Lock()
		if h.sessions[s.ID] != s {
			h.mu.Unlock()
			s.loadMu.Unlock()
			continue
		}
		delete(h.sessions, s.ID)
		h.mu.Unlock()
		if s.agent != nil {
			s.interrupt()
		}
		if s.onClose != nil {
			s.onClose()
		}
		s.loadMu.Unlock()
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
	h.mu.Unlock()
	if s == nil {
		return false
	}
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	h.mu.Lock()
	if h.sessions[id] != s {
		h.mu.Unlock()
		return false
	}
	delete(h.sessions, id)
	h.mu.Unlock()
	if s.agent != nil {
		s.interrupt()
	}
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
