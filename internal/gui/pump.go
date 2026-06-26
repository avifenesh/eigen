package gui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/avifenesh/eigen/internal/daemon"
)

// sessionPump owns ONE dedicated daemon connection streaming a single session's
// events to the frontend. A control client cannot multiplex a blocking Attach
// stream, so each subscribed session gets its own connection. The daemon
// releases the session view automatically when this connection closes
// ("one connection = one view"), so Close() is the entire detach contract — no
// protocol-level Detach op is needed.
type sessionPump struct {
	id        string
	client    *daemon.Client
	stop      chan struct{}
	stopOnce  sync.Once // guards close(stop) against Shutdown/stopPump/watchdog races
	closeOnce sync.Once // guards client.Close() against the same races
}

// Subscribe attaches a streaming pump for the session. Idempotent: a second
// Subscribe for the same id is a no-op. A placeholder slot is reserved under
// the lock BEFORE dialing so two concurrent Subscribes can't both open a
// connection (TOCTOU), then replaced with the live pump once Attach succeeds.
func (b *Bridge) Subscribe(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("session id required")
	}

	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return fmt.Errorf("bridge shutting down")
	}
	if _, ok := b.pumps[id]; ok {
		b.mu.Unlock()
		return nil // already subscribed or reserved
	}
	placeholder := &sessionPump{id: id, stop: make(chan struct{})}
	b.pumps[id] = placeholder
	b.mu.Unlock()

	fail := func(e error) error {
		b.mu.Lock()
		if b.pumps[id] == placeholder {
			delete(b.pumps, id)
		}
		b.mu.Unlock()
		return e
	}

	c, err := b.ensure() // dedicated connection for this stream
	if err != nil {
		return fail(err)
	}

	evName := sessionEvent(id)
	closeName := sessionClosed(id)

	// The Attach handler runs on the client's event-loop goroutine — a single
	// goroutine, so a plain counter here increments in strict emit order. v3
	// Event.Emit is non-blocking (dispatches via go func), so the handler can
	// never stall the daemon connection. Each event carries that ordinal (Seq)
	// so the frontend can reassemble despite Wails' per-event-goroutine dispatch
	// reordering arrival at the webview.
	var seq uint64
	if err := c.Attach(id, func(e daemon.WireEvent, replay bool) {
		if b.app != nil {
			seq++
			b.app.Event.Emit(evName, StreamEventDTO{Event: toWireEventDTO(e), Replay: replay, Seq: seq})
		}
	}); err != nil {
		_ = c.Close()
		return fail(err)
	}

	p := &sessionPump{id: id, client: c, stop: placeholder.stop}
	b.mu.Lock()
	if b.closing || b.pumps[id] != placeholder {
		// Shut down or superseded while we were attaching: drop this one.
		b.mu.Unlock()
		_ = c.Close()
		return nil
	}
	b.pumps[id] = p // replace placeholder with the live pump
	b.mu.Unlock()

	// Watchdog: dies on explicit Unsubscribe (stop) OR daemon death (Done).
	go func() {
		select {
		case <-p.stop:
		case <-c.Done():
			if b.app != nil {
				b.app.Event.Emit(closeName, struct{}{})
			}
		}
		b.mu.Lock()
		if b.pumps[id] == p {
			delete(b.pumps, id)
		}
		b.mu.Unlock()
		p.closeOnce.Do(func() { _ = c.Close() })
	}()
	return nil
}

// Unsubscribe stops the session's streaming pump.
func (b *Bridge) Unsubscribe(id string) error {
	b.stopPump(id)
	return nil
}

// stopPump removes and tears down a pump by id, guarding every close with a
// sync.Once so concurrent Shutdown / Unsubscribe / watchdog paths are
// panic-free.
func (b *Bridge) stopPump(id string) {
	b.mu.Lock()
	p := b.pumps[id]
	if p != nil {
		delete(b.pumps, id)
	}
	b.mu.Unlock()
	if p == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stop) })
	if p.client != nil {
		p.closeOnce.Do(func() { _ = p.client.Close() })
	}
}

// ---- frontend event names ----

func sessionEvent(id string) string  { return "eigen:session:" + id + ":event" }
func sessionClosed(id string) string { return "eigen:session:" + id + ":closed" }
