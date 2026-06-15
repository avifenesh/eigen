package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
)

// Client is a view's connection to the daemon. One client = one socket
// connection; Attach streams one session's events while control ops
// (List/New/Input/Interrupt) remain usable.
type Client struct {
	conn io.ReadWriteCloser

	mu      sync.Mutex // guards conn writes + onEvent handler swap
	reqMu   sync.Mutex // serializes request/reply pairs (one in flight at a time)
	scanner *bufio.Scanner

	// replies receives non-event responses (ok/error/sessions/attached) in
	// request order; events queue to eventLoop for the attached handler.
	replies chan Response
	events  chan Response
	onEvent func(WireEvent, bool) // bool = replay
	done    chan struct{}
}

// Dial connects to the daemon socket. Returns an error when no daemon is
// running (the caller can start one or fall back to standalone mode).
func Dial(sockPath string) (*Client, error) {
	if sockPath == "" {
		sockPath = SocketPath()
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("no daemon at %s (start one with `eigen daemon`)", sockPath)
	}
	return DialConn(conn), nil
}

// DialConn wraps an already-connected stream (a unix socket, an ssh stdio pipe,
// a WebSocket adapter) as a daemon Client. The protocol is transport-agnostic
// line-JSON, so any io.ReadWriteCloser works; Dial is just the unix-socket case.
func DialConn(conn io.ReadWriteCloser) *Client {
	c := &Client{
		conn:    conn,
		replies: make(chan Response, 16),
		events:  make(chan Response, 1024),
		done:    make(chan struct{}),
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	c.scanner = sc
	go c.readLoop()
	go c.eventLoop()
	return c
}

// eventLoop delivers events to the handler OFF the read loop, so a handler
// that issues requests (e.g. answering an approval) cannot deadlock the
// connection (the read loop must stay free to route the reply).
func (c *Client) eventLoop() {
	for r := range c.events {
		c.mu.Lock()
		h := c.onEvent
		c.mu.Unlock()
		if h != nil && r.Event != nil {
			h(*r.Event, r.Replay)
		}
	}
}

// readLoop routes incoming lines: events to the handler, everything else to
// the replies channel.
func (c *Client) readLoop() {
	defer close(c.done)
	defer close(c.events)
	for c.scanner.Scan() {
		var r Response
		if err := json.Unmarshal(c.scanner.Bytes(), &r); err != nil {
			continue
		}
		if r.Type == "event" {
			select {
			case c.events <- r:
			default: // a slow handler must not stall the connection
			}
			continue
		}
		select {
		case c.replies <- r:
		default: // drop if the caller stopped reading replies
		}
	}
}

// Done is closed when the connection ends (daemon stopped / socket closed).
func (c *Client) Done() <-chan struct{} { return c.done }

// Close terminates the connection.
func (c *Client) Close() error { return c.conn.Close() }

// request sends a request and waits for the next non-event reply.
// request sends one request and waits for its reply with the default timeout.
func (c *Client) request(req Request) (Response, error) {
	return c.requestWithin(req, requestTimeoutFor(req.Op))
}

// requestWithin sends one request and waits up to d for its reply. reqMu
// serializes the whole send→receive cycle so concurrent callers (e.g. the rail
// poll's List() racing a turn's State()) can't read each other's reply off the
// shared channel — replies carry no id, so pairing relies on one in flight.
func (c *Client) requestWithin(req Request, d time.Duration) (Response, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	// Drain any stale reply a prior timed-out request may have left behind so
	// it can't be mistaken for this one's answer.
	for {
		select {
		case <-c.replies:
			continue
		default:
		}
		break
	}
	c.mu.Lock()
	_, err = c.conn.Write(append(b, '\n'))
	c.mu.Unlock()
	if err != nil {
		return Response{}, err
	}
	select {
	case r := <-c.replies:
		if r.Type == "error" {
			return r, fmt.Errorf("%s", r.Error)
		}
		return r, nil
	case <-c.done:
		return Response{}, fmt.Errorf("daemon connection closed")
	case <-time.After(d):
		return Response{}, fmt.Errorf("daemon request %q timed out", req.Op)
	}
}

// requestTimeoutFor returns the deadline for an op, read LAZILY from
// EIGEN_DAEMON_TIMEOUT each call so a config-driven override (which sets the
// env at startup, after this package's init) takes effect without a re-exec.
//   - normal ops: EIGEN_DAEMON_TIMEOUT or 30s
//   - "set"/model switch: max(90s, override) — provider construction is slow
//   - "compact": max(6m, override) — a summary over the largest context, kept
//     above the provider's own 5m HTTP timeout so the daemon always resolves
func requestTimeoutFor(op string) time.Duration {
	base := envTimeout("EIGEN_DAEMON_TIMEOUT", 30*time.Second)
	switch op {
	case "compact":
		return maxDur(6*time.Minute, base)
	case "new":
		// Creating a session builds the whole agent: provider + MCP servers +
		// LSP + plugins. A cold start with several MCP servers can take a while.
		return maxDur(2*time.Minute, base)
	case "set":
		return maxDur(90*time.Second, base)
	default:
		return base
	}
}

// envTimeout reads a whole-seconds duration from an env var, falling back to def
// when unset, non-numeric, or non-positive.
func envTimeout(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// Ping checks the daemon is alive.
func (c *Client) Ping() error {
	_, err := c.request(Request{Op: "ping"})
	return err
}

// List returns the daemon's sessions.
func (c *Client) List() ([]SessionInfo, error) {
	r, err := c.request(Request{Op: "list"})
	if err != nil {
		return nil, err
	}
	return r.Sessions, nil
}

// New creates a session rooted at dir (daemon default model when model == "").
func (c *Client) New(dir, model string) (string, error) {
	r, err := c.request(Request{Op: "new", Dir: dir, Model: model})
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// Attach subscribes to a session's events: handler receives the replay (with
// replay=true) then live events. Only one attachment per client.
func (c *Client) Attach(id string, handler func(e WireEvent, replay bool)) error {
	c.mu.Lock()
	c.onEvent = handler
	c.mu.Unlock()
	_, err := c.request(Request{Op: "attach", ID: id})
	return err
}

// Input sends a user message (with optional images) to a session.
func (c *Client) Input(id, text string, images []llm.Image) error {
	_, err := c.request(Request{Op: "input", ID: id, Text: text, Images: images})
	return err
}

// Interrupt cancels a session's in-flight turn.
func (c *Client) Interrupt(id string) error {
	_, err := c.request(Request{Op: "interrupt", ID: id})
	return err
}

// Remove deletes a hosted session.
func (c *Client) Remove(id string) error {
	_, err := c.request(Request{Op: "remove", ID: id})
	return err
}

// Prune removes hosted sessions with no conversation and returns their ids.
func (c *Client) Prune() ([]string, error) {
	r, err := c.request(Request{Op: "prune"})
	if err != nil {
		return nil, err
	}
	return r.Pruned, nil
}

// Approve answers a pending approval on a session.
func (c *Client) Approve(sessionID, approvalID string, allow bool) error {
	_, err := c.request(Request{Op: "approve", ID: sessionID, Approval: approvalID, Allow: allow})
	return err
}

// State fetches a session's full snapshot (history + model/perm/goal/tools).
func (c *Client) State(sessionID string) (*SessionState, error) {
	r, err := c.request(Request{Op: "state", ID: sessionID})
	if err != nil {
		return nil, err
	}
	if r.State == nil {
		return nil, fmt.Errorf("empty state")
	}
	return r.State, nil
}

// SetPerm changes a session's permission posture.
func (c *Client) SetPerm(sessionID, perm string) error {
	_, err := c.request(Request{Op: "set", ID: sessionID, Perm: perm})
	return err
}

// SetGoal sets (or clears, with "") a session's goal.
func (c *Client) SetGoal(sessionID, goal string) error {
	_, err := c.request(Request{Op: "set", ID: sessionID, Goal: &goal})
	return err
}

// SetTitle renames a session (or clears, with "" → revert to derived).
func (c *Client) SetTitle(sessionID, title string) error {
	_, err := c.request(Request{Op: "set", ID: sessionID, Title: &title})
	return err
}

// AddDir extends a session's tool sandbox with an additional allowed directory
// (the user-invoked /add-dir grant). Returns the normalized root that was added.
func (c *Client) AddDir(sessionID, path string) (string, error) {
	resp, err := c.request(Request{Op: "add-dir", ID: sessionID, AddDir: path})
	if err != nil {
		return "", err
	}
	return resp.Root, nil
}

// Compact summarizes a session's conversation toward target tokens.
func (c *Client) Compact(sessionID string, target int) (before, after int, err error) {
	r, err := c.requestWithin(Request{Op: "compact", ID: sessionID, Target: target}, requestTimeoutFor("compact"))
	if err != nil {
		return 0, 0, err
	}
	return r.Before, r.After, nil
}

// Clear resets a session's conversation to empty.
func (c *Client) Clear(sessionID string) error {
	_, err := c.request(Request{Op: "clear", ID: sessionID})
	return err
}

// ResetTo replaces a session's conversation with imported history (/resume).
func (c *Client) ResetTo(sessionID string, history []llm.Message) error {
	_, err := c.request(Request{Op: "clear", ID: sessionID, History: history})
	return err
}

// Resend retries a session's last user turn (runs in the daemon).
func (c *Client) Resend(sessionID string) error {
	_, err := c.request(Request{Op: "resend", ID: sessionID})
	return err
}

// SetModel switches a session's model live (the daemon rebuilds the provider).
// Uses the longer "set" timeout: building a provider can resolve
// credentials / probe an endpoint and outlast a normal op.
func (c *Client) SetModel(sessionID, modelID string) error {
	_, err := c.requestWithin(Request{Op: "set", ID: sessionID, Model: modelID}, requestTimeoutFor("set"))
	return err
}

// NewSession creates a daemon session with full options: rooted at dir, an
// optional model, an optional permission posture, and optional resumed
// history (the --resume path).
func (c *Client) NewSession(dir, model, perm string, history []llm.Message) (string, error) {
	r, err := c.request(Request{Op: "new", Dir: dir, Model: model, Perm: perm, History: history})
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// SetEffort sets the session's reasoning effort; error = unsupported.
func (c *Client) SetEffort(sessionID, level string) error {
	_, err := c.request(Request{Op: "set", ID: sessionID, Effort: level})
	return err
}

// SetSearch sets the session's live-search mode; error = unsupported.
func (c *Client) SetSearch(sessionID, mode string) error {
	_, err := c.request(Request{Op: "set", ID: sessionID, Search: mode})
	return err
}
