package gui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/feed"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Bridge is the Wails-bound service exposed to the frontend. Every method here
// becomes a generated TS binding. It holds ONE long-lived control client for
// request/response RPCs, and a map of streaming pumps (one dedicated daemon
// connection per subscribed session — see pump.go).
//
// All daemon IO (ensure/retry/sleep) is done OUTSIDE the mutex so a down daemon
// can never stall unrelated RPCs, and every teardown path is guarded by
// sync.Once so concurrent Shutdown/Unsubscribe/watchdog races can't double-close.
type Bridge struct {
	app    *application.App
	ensure func() (*daemon.Client, error)

	// Proactive-feed inputs (the home base's "act on" surface). suggest is the
	// LLM suggester (nil = suggestions off; git/github/memory signals still
	// flow); dirs supplies the project universe to scan. Both injected by main
	// so the bridge owns no model/provider construction.
	suggest feed.Suggester
	dirs    func() []string

	mu       sync.Mutex
	ctrl     *daemon.Client
	pumps    map[string]*sessionPump
	closing  bool
	pollStop chan struct{}
	feedStop chan struct{}
	lastFeed feed.Feed // most-recent scan, so DismissFeed can rebuild an Item from its key
}

// NewBridge constructs the bridge. ensure connects to (and lazily spawns) the
// daemon; suggest + dirs power the proactive feed (both may be nil/empty — the
// feed then yields only signal-derived items or nothing).
func NewBridge(ensure func() (*daemon.Client, error), suggest feed.Suggester, dirs func() []string) *Bridge {
	return &Bridge{ensure: ensure, suggest: suggest, dirs: dirs, pumps: map[string]*sessionPump{}}
}

// SetApp wires the Wails app for event emission. Called from the bootstrap
// before Run.
func (b *Bridge) SetApp(app *application.App) { b.app = app }

// ServiceStartup is the Wails v3 service lifecycle hook (optional interface).
// Verified signature at v3.0.0-alpha2.105: (context.Context, ServiceOptions) error.
func (b *Bridge) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	if _, err := b.control(); err != nil {
		b.emit(eventDaemonHealth, HealthDTO{OK: false, Error: err.Error()})
	}
	b.mu.Lock()
	b.pollStop = make(chan struct{})
	b.feedStop = make(chan struct{})
	stop := b.pollStop
	feedStop := b.feedStop
	b.mu.Unlock()
	go b.healthLoop(stop)
	go b.feedLoop(feedStop)
	return nil
}

// ServiceShutdown is the Wails v3 shutdown hook. Tears down every pump + the
// control client so no goroutine, connection, or daemon-side view leaks.
func (b *Bridge) ServiceShutdown() error {
	b.Shutdown()
	return nil
}

const (
	eventDaemonStats  = "eigen:daemon:stats"
	eventDaemonHealth = "eigen:daemon:health"
)

// healthLoop pushes a DaemonStats snapshot to the frontend at ~1Hz while
// online, backing off to 5s while the daemon is unreachable so a down daemon
// never becomes a busy reconnect loop.
func (b *Bridge) healthLoop(stop chan struct{}) {
	const fast, slow = time.Second, 5 * time.Second
	t := time.NewTicker(fast)
	defer t.Stop()
	fails := 0
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			c, err := b.control()
			if err == nil {
				if st, e := c.Stats(); e == nil {
					if fails != 0 {
						fails = 0
						t.Reset(fast)
					}
					b.emit(eventDaemonStats, st)
					continue
				} else {
					err = e
				}
			}
			b.emit(eventDaemonHealth, HealthDTO{OK: false, Error: err.Error()})
			if fails == 0 {
				t.Reset(slow)
			}
			fails++
		}
	}
}

func (b *Bridge) emit(name string, data any) {
	if b.app != nil {
		b.app.Event.Emit(name, data)
	}
}

// control returns the long-lived control client, (re)connecting on demand.
// IO (ensure/retry/sleep) runs OUTSIDE the lock; the stale client is Closed
// before replacement so its readLoop/eventLoop goroutines terminate.
func (b *Bridge) control() (*daemon.Client, error) {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return nil, fmt.Errorf("bridge shutting down")
	}
	if c := b.ctrl; c != nil {
		select {
		case <-c.Done(): // stale: drop + close, reconnect below
			b.ctrl = nil
			b.mu.Unlock()
			_ = c.Close()
		default:
			b.mu.Unlock()
			return c, nil
		}
	} else {
		b.mu.Unlock()
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		c, err := b.ensure()
		if err == nil {
			b.mu.Lock()
			if b.closing {
				b.mu.Unlock()
				_ = c.Close()
				return nil, fmt.Errorf("bridge shutting down")
			}
			if b.ctrl != nil { // a racing caller already reconnected
				existing := b.ctrl
				b.mu.Unlock()
				_ = c.Close()
				return existing, nil
			}
			b.ctrl = c
			b.mu.Unlock()
			return c, nil
		}
		lastErr = err
		time.Sleep(time.Duration(150*(1<<attempt)) * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon unavailable: %w", lastErr)
}

// Shutdown stops the health loop, tears down every pump, and closes the control
// client. Idempotent-safe via the closing flag + per-pump sync.Once guards.
func (b *Bridge) Shutdown() {
	b.mu.Lock()
	b.closing = true
	pumps := make([]*sessionPump, 0, len(b.pumps))
	for _, p := range b.pumps {
		pumps = append(pumps, p)
	}
	b.pumps = map[string]*sessionPump{}
	ctrl := b.ctrl
	b.ctrl = nil
	stop := b.pollStop
	b.pollStop = nil
	feedStop := b.feedStop
	b.feedStop = nil
	b.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if feedStop != nil {
		close(feedStop)
	}
	for _, p := range pumps {
		p.stopOnce.Do(func() { close(p.stop) })
		if p.client != nil {
			p.closeOnce.Do(func() { _ = p.client.Close() })
		}
	}
	if ctrl != nil {
		_ = ctrl.Close()
	}
}

// ---- health ----

// Ping verifies the daemon connection.
func (b *Bridge) Ping() error {
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Ping()
}

// Stats returns the daemon resource-health snapshot.
func (b *Bridge) Stats() (*daemon.DaemonStats, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	return c.Stats()
}

// ---- session lifecycle ----

// Sessions lists hosted sessions, newest-updated first.
func (b *Bridge) Sessions() ([]SessionInfoDTO, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	infos, err := c.List()
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfoDTO, 0, len(infos))
	for _, in := range infos {
		out = append(out, toSessionInfoDTO(in))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out, nil
}

// NewSession creates a session rooted at dir (default: cwd) and returns its id.
func (b *Bridge) NewSession(dir, model, perm string) (string, error) {
	c, err := b.control()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		if wd, e := os.Getwd(); e == nil {
			dir = wd
		}
	}
	return c.NewSession(dir, model, perm, nil)
}

// RemoveSession stops the session's pump and removes it from the daemon.
func (b *Bridge) RemoveSession(id string) error {
	b.stopPump(id)
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Remove(id)
}

// PruneSessions removes idle/empty sessions and stops their pumps.
func (b *Bridge) PruneSessions() ([]string, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	pruned, err := c.Prune()
	if err != nil {
		return nil, err
	}
	for _, id := range pruned {
		b.stopPump(id)
	}
	return pruned, nil
}

// State returns the full session snapshot (history + status).
func (b *Bridge) State(id string) (*SessionStateDTO, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	st, err := c.State(id)
	if err != nil {
		return nil, err
	}
	return toSessionStateDTO(st), nil
}

// ---- turn I/O ----

// SendInput delivers text+images via the daemon `input` op (carries allowTools).
// Returns only error; the UI derives running-state from the stream + State(),
// never a racy synthetic guess.
func (b *Bridge) SendInput(id, text string, images []ImageDTO, allowTools []string) error {
	c, err := b.control()
	if err != nil {
		return err
	}
	imgs, err := fromImageDTOs(images)
	if err != nil {
		return err
	}
	return c.Input(id, text, imgs, allowTools)
}

// Interrupt cancels the in-flight turn.
func (b *Bridge) Interrupt(id string) error {
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Interrupt(id)
}

// Resend retries the last turn.
func (b *Bridge) Resend(id string) error {
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Resend(id)
}

// Approve resolves a gated tool-call approval.
func (b *Bridge) Approve(id, approvalID string, allow bool) error {
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Approve(id, approvalID, allow)
}

// ---- maintenance ----

// Compact summarizes the conversation toward target tokens.
func (b *Bridge) Compact(id string, target int) (CompactResultDTO, error) {
	c, err := b.control()
	if err != nil {
		return CompactResultDTO{}, err
	}
	before, after, err := c.Compact(id, target)
	return CompactResultDTO{Before: before, After: after}, err
}

// Clear wipes the session conversation.
func (b *Bridge) Clear(id string) error {
	c, err := b.control()
	if err != nil {
		return err
	}
	return c.Clear(id)
}

// ---- settings (each returns the fresh state so the UI reconciles optimism) ----

func (b *Bridge) setThen(id string, fn func(*daemon.Client) error) (*SessionStateDTO, error) {
	c, err := b.control()
	if err != nil {
		return nil, err
	}
	if err := fn(c); err != nil {
		return nil, err
	}
	st, err := c.State(id)
	if err != nil {
		return nil, err
	}
	return toSessionStateDTO(st), nil
}

// SetModel switches the session's model.
func (b *Bridge) SetModel(id, model string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetModel(id, model) })
}

// SetPerm switches the permission posture (gated|auto).
func (b *Bridge) SetPerm(id, perm string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetPerm(id, perm) })
}

// SetGoal sets the session goal.
func (b *Bridge) SetGoal(id, goal string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetGoal(id, goal) })
}

// SetTitle renames the session.
func (b *Bridge) SetTitle(id, title string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetTitle(id, title) })
}

// SetEffort sets the reasoning-effort level.
func (b *Bridge) SetEffort(id, level string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetEffort(id, level) })
}

// SetSearch sets the provider search mode.
func (b *Bridge) SetSearch(id, mode string) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetSearch(id, mode) })
}

// SetFast toggles the fast/priority service tier.
func (b *Bridge) SetFast(id string, on bool) (*SessionStateDTO, error) {
	return b.setThen(id, func(c *daemon.Client) error { return c.SetFast(id, on) })
}

// ---- sandbox / shells / dirs ----

// AddDir grants the session's tools an extra working directory.
func (b *Bridge) AddDir(id, path string) (string, error) {
	c, err := b.control()
	if err != nil {
		return "", err
	}
	return c.AddDir(id, path)
}

// KillShell signals a backgrounded shell.
func (b *Bridge) KillShell(id, shellID string) (bool, error) {
	c, err := b.control()
	if err != nil {
		return false, err
	}
	return c.KillShell(id, shellID)
}

// DetachBash backgrounds the foreground bash, freeing the turn.
func (b *Bridge) DetachBash(id string) (bool, error) {
	c, err := b.control()
	if err != nil {
		return false, err
	}
	return c.DetachBash(id)
}
