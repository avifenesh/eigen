package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

// Client is a view's connection to the daemon. One client = one socket
// connection; Attach streams one session's events while control ops
// (List/New/Input/Interrupt) remain usable.
type Client struct {
	conn net.Conn

	mu      sync.Mutex // guards writes + pending reply routing
	scanner *bufio.Scanner

	// replies receives non-event responses (ok/error/sessions/attached) in
	// request order; events fan out to the attached handler.
	replies chan Response
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
	c := &Client{
		conn:    conn,
		replies: make(chan Response, 16),
		done:    make(chan struct{}),
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	c.scanner = sc
	go c.readLoop()
	return c, nil
}

// readLoop routes incoming lines: events to the handler, everything else to
// the replies channel.
func (c *Client) readLoop() {
	defer close(c.done)
	for c.scanner.Scan() {
		var r Response
		if err := json.Unmarshal(c.scanner.Bytes(), &r); err != nil {
			continue
		}
		if r.Type == "event" {
			c.mu.Lock()
			h := c.onEvent
			c.mu.Unlock()
			if h != nil && r.Event != nil {
				h(*r.Event, r.Replay)
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
func (c *Client) request(req Request) (Response, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
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
	}
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

// Input sends a user message to a session (its turn runs in the daemon).
func (c *Client) Input(id, text string) error {
	_, err := c.request(Request{Op: "input", ID: id, Text: text})
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

// Approve answers a pending approval on a session.
func (c *Client) Approve(sessionID, approvalID string, allow bool) error {
	_, err := c.request(Request{Op: "approve", ID: sessionID, Approval: approvalID, Allow: allow})
	return err
}
