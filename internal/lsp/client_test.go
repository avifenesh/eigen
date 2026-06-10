package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// newFakeClient wires a client (newClient) to an in-process fake LSP server
// over io.Pipe, so the client can be tested without spawning a real language
// server. The test drives the server by registering a method handler.
func newFakeClient(t *testing.T, handle func(method string, params json.RawMessage) (any, bool)) *Client {
	t.Helper()
	// client writes → server reads
	cReader, cWriter := io.Pipe()
	// server writes → client reads
	sReader, sWriter := io.Pipe()

	c := newClient(cWriter, sReader, func() error {
		_ = cWriter.Close()
		_ = sWriter.Close()
		return nil
	})

	// Server goroutine: read framed requests, dispatch to handle, write framed
	// responses for requests that have an ID.
	go func() {
		br := bufio.NewReader(cReader)
		for {
			body, err := readFrame(br)
			if err != nil {
				return
			}
			var msg rpcMessage
			if json.Unmarshal(body, &msg) != nil {
				continue
			}
			if msg.ID == nil {
				continue // notification: no reply
			}
			result, ok := handle(msg.Method, msg.Params)
			resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
			if !ok {
				resp.Error = &rpcError{Code: -32601, Message: "method not found"}
			} else {
				raw, _ := json.Marshal(result)
				resp.Result = raw
			}
			writeServerFrame(sWriter, resp)
		}
	}()
	return c
}

func writeServerFrame(w io.Writer, v any) {
	body, _ := json.Marshal(v)
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body))
	w.Write(body)
}

func TestInitializeAndDefinition(t *testing.T) {
	c := newFakeClient(t, func(method string, _ json.RawMessage) (any, bool) {
		switch method {
		case "initialize":
			return map[string]any{"capabilities": map[string]any{}}, true
		case "textDocument/definition":
			return []Location{{
				URI:   "file:///proj/main.go",
				Range: Range{Start: Position{Line: 41, Character: 5}},
			}}, true
		}
		return nil, false
	})
	defer c.Close()

	ctx := context.Background()
	if err := c.initialize(ctx, "/proj"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	locs, err := c.Definition(ctx, "file:///proj/main.go", Position{Line: 10, Character: 2})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 41 {
		t.Fatalf("unexpected definition result: %+v", locs)
	}
}

func TestDefinitionLocationLinks(t *testing.T) {
	// Servers may return LocationLink[] instead of Location[].
	c := newFakeClient(t, func(method string, _ json.RawMessage) (any, bool) {
		if method == "textDocument/definition" {
			return []map[string]any{{
				"targetUri":   "file:///proj/x.go",
				"targetRange": Range{Start: Position{Line: 3}},
			}}, true
		}
		return nil, false
	})
	defer c.Close()
	locs, err := c.Definition(context.Background(), "file:///proj/x.go", Position{})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if len(locs) != 1 || locs[0].URI != "file:///proj/x.go" || locs[0].Range.Start.Line != 3 {
		t.Fatalf("LocationLink not decoded: %+v", locs)
	}
}

func TestHoverVariants(t *testing.T) {
	cases := []struct {
		name     string
		contents any
		want     string
	}{
		{"string", "func F()", "func F()"},
		{"markup", map[string]any{"kind": "markdown", "value": "**F**"}, "**F**"},
		{"marked", map[string]any{"language": "go", "value": "func F()"}, "func F()"},
		{"array", []any{"a", map[string]any{"value": "b"}}, "a\nb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient(t, func(method string, _ json.RawMessage) (any, bool) {
				if method == "textDocument/hover" {
					return map[string]any{"contents": tc.contents}, true
				}
				return nil, false
			})
			defer c.Close()
			got, err := c.Hover(context.Background(), "file:///x", Position{})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("hover %s = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestDocumentSymbolsHierarchical(t *testing.T) {
	c := newFakeClient(t, func(method string, _ json.RawMessage) (any, bool) {
		if method == "textDocument/documentSymbol" {
			return []DocumentSymbol{{
				Name:  "T",
				Kind:  23, // struct
				Range: Range{Start: Position{Line: 1}},
				Children: []DocumentSymbol{
					{Name: "M", Kind: 6, Range: Range{Start: Position{Line: 2}}},
				},
			}}, true
		}
		return nil, false
	})
	defer c.Close()
	syms, err := c.DocumentSymbols(context.Background(), "file:///x")
	if err != nil {
		t.Fatal(err)
	}
	// Flattened: parent + child.
	if len(syms) != 2 || syms[0].Name != "T" || syms[1].Name != "M" {
		t.Fatalf("symbols not flattened: %+v", syms)
	}
}

func TestDiagnosticsCapture(t *testing.T) {
	c := newFakeClient(t, func(string, json.RawMessage) (any, bool) { return nil, false })
	defer c.Close()
	// Simulate a publishDiagnostics notification by invoking the handler.
	params, _ := json.Marshal(PublishDiagnosticsParams{
		URI: "file:///proj/main.go",
		Diagnostics: []Diagnostic{
			{Range: Range{Start: Position{Line: 9}}, Severity: 1, Source: "compiler", Message: "undefined: x"},
		},
	})
	c.handleDiagnostics(params)
	got := c.Diagnostics("file:///proj/main.go")
	if len(got) != 1 || got[0].Message != "undefined: x" {
		t.Fatalf("diagnostics not captured: %+v", got)
	}
	if waitDiagnostics(context.Background(), c, "file:///proj/main.go") == nil {
		t.Fatal("waitDiagnostics should return captured diagnostics immediately")
	}
}

func TestWaitDiagnosticsTimesOut(t *testing.T) {
	c := newFakeClient(t, func(string, json.RawMessage) (any, bool) { return nil, false })
	defer c.Close()
	start := time.Now()
	if d := waitDiagnostics(context.Background(), c, "file:///none"); len(d) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", d)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("waitDiagnostics blocked too long")
	}
}

func TestURIRoundTrip(t *testing.T) {
	uri := PathToURI("/proj/src/main.go")
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("bad uri: %q", uri)
	}
	if p := URIToPath(uri); p != "/proj/src/main.go" {
		t.Fatalf("round-trip path = %q", p)
	}
}
