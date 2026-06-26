package mcp

import (
	"context"
	"encoding/json"
)

// session is the transport-agnostic MCP connection the loader + lazyClient use.
// Two transports implement it: the stdio *Client (a spawned subprocess speaking
// newline-delimited JSON-RPC) and the *httpClient (a remote server over MCP
// Streamable HTTP / SSE). Keeping callers on this interface lets a remote
// connector and a local stdio server be wired identically — the only difference
// is how the connection is dialed.
type session interface {
	// Instructions is the server's self-description from initialize (may be "").
	Instructions() string
	// ServerName is the name the server reported at initialize (may be "").
	ServerName() string
	// ListTools returns the server's advertised tools.
	ListTools(ctx context.Context) ([]ToolSpec, error)
	// CallToolRich invokes a tool and returns its text + image content.
	CallToolRich(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
	// alive reports whether the connection can still carry calls; once false the
	// lazyClient owner drops it and re-dials on the next use.
	alive() bool
	// Close shuts down the connection (and, for stdio, the underlying process).
	Close() error
}
