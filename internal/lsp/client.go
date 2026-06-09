// Package lsp is a minimal Language Server Protocol client: it speaks JSON-RPC
// 2.0 over Content-Length-framed stdio (the LSP wire format, distinct from
// MCP's newline-delimited messages), performs the initialize handshake, opens
// documents, and issues the navigation requests eigen exposes as tools —
// definition, references, hover, and document symbols — plus it captures the
// diagnostics a server pushes via textDocument/publishDiagnostics.
//
// The transport is abstracted (io.Reader / io.Writer) so the client is testable
// without spawning a real language server.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// rpcMessage is a JSON-RPC 2.0 envelope used for both requests and responses;
// the fields present depend on the direction.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("lsp error %d: %s", e.Code, e.Message) }

// Client is a connected LSP session.
type Client struct {
	w  io.Writer
	wm sync.Mutex // serializes frame writes

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcMessage

	// diagnostics holds the latest publishDiagnostics per document URI.
	diags map[string][]Diagnostic

	closeFn func() error
}

// newClient starts the read loop over r and writes framed requests to w.
func newClient(w io.Writer, r io.Reader, closeFn func() error) *Client {
	c := &Client{
		w:       w,
		pending: map[int]chan rpcMessage{},
		diags:   map[string][]Diagnostic{},
		closeFn: closeFn,
	}
	go c.readLoop(r)
	return c
}

// readLoop reads Content-Length-framed messages and dispatches responses to
// waiters and notifications to their handlers.
func (c *Client) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		body, err := readFrame(br)
		if err != nil {
			break
		}
		var msg rpcMessage
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		// Server→client request (e.g. workspace/configuration): we don't answer
		// those; only responses (have an ID + result/error and no method) and
		// notifications (have a method, no ID) matter.
		if msg.ID != nil && msg.Method == "" {
			c.mu.Lock()
			ch := c.pending[*msg.ID]
			delete(c.pending, *msg.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Method == "textDocument/publishDiagnostics" {
			c.handleDiagnostics(msg.Params)
		}
	}
	// Stream closed: fail any in-flight calls.
	c.mu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// readFrame reads one LSP message: headers terminated by a blank line, then a
// Content-Length-byte body.
func readFrame(br *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("lsp: missing/zero Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}

// writeFrame serializes v and writes it with a Content-Length header.
func (c *Client) writeFrame(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wm.Lock()
	defer c.wm.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

// call sends a request and waits for the matching response.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.writeFrame(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return fmt.Errorf("lsp: connection closed during %s", method)
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify sends a notification (no response expected).
func (c *Client) notify(method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.writeFrame(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}

func (c *Client) handleDiagnostics(raw json.RawMessage) {
	var p PublishDiagnosticsParams
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	c.mu.Lock()
	c.diags[p.URI] = p.Diagnostics
	c.mu.Unlock()
}

// Diagnostics returns the latest diagnostics captured for a document URI.
func (c *Client) Diagnostics(uri string) []Diagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Diagnostic, len(c.diags[uri]))
	copy(out, c.diags[uri])
	return out
}

// Close shuts down the connection and the underlying process.
func (c *Client) Close() error {
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}
