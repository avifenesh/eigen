package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/avifenesh/eigen/internal/llm"
)

// --- LSP protocol types (the subset eigen uses) ----------------------------

// Position is a zero-based line/character offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range within a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic is one problem reported by the server.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=error 2=warning 3=info 4=hint
	Code     any    `json:"code"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// PublishDiagnosticsParams is the payload of textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// DocumentSymbol is a hierarchical symbol (newer servers); SymbolInformation
// is the flat fallback. We accept either when decoding documentSymbol results.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children"`
	// SymbolInformation fallback fields:
	Location Location `json:"location"`
}

// hoverResult is the (variant) shape of a textDocument/hover response. The
// contents field may be a string, a {language,value} object, a {kind,value}
// MarkupContent, or an array of those — decodeHoverContents flattens it.
type hoverResult struct {
	Contents hoverContents `json:"contents"`
	Range    *Range        `json:"range"`
}

// Connect spawns a language-server process and performs the initialize
// handshake rooted at rootDir.
func Connect(ctx context.Context, command []string, env []string, rootDir string) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("lsp: empty command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Dir = rootDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := newClient(stdin, stdout, func() error {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil
	})
	if err := c.initialize(ctx, rootDir); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) initialize(ctx context.Context, rootDir string) error {
	rootURI := PathToURI(rootDir)
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"rootPath":  rootDir,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": filepath.Base(rootDir)},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"publishDiagnostics": map[string]any{},
			},
		},
		"clientInfo": map[string]any{"name": "eigen", "version": llm.Version},
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

// DidOpen tells the server about a document's content so position-based
// requests resolve. languageID may be empty (the server usually infers it).
func (c *Client) DidOpen(uri, languageID, text string) error {
	return c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID,
			"version":    1,
			"text":       text,
		},
	})
}

// Definition resolves the definition location(s) of the symbol at pos.
func (c *Client) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/definition", uri, pos, nil)
}

// References finds all references to the symbol at pos.
func (c *Client) References(ctx context.Context, uri string, pos Position, includeDecl bool) ([]Location, error) {
	return c.locations(ctx, "textDocument/references", uri, pos, map[string]any{"includeDeclaration": includeDecl})
}

// locations issues a position request whose result is a Location, []Location,
// or null, normalizing all three to a slice.
func (c *Client) locations(ctx context.Context, method, uri string, pos Position, extra map[string]any) ([]Location, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}
	if extra != nil {
		params["context"] = extra
	}
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	return decodeLocations(raw), nil
}

// Hover returns the hover text for the symbol at pos ("" when none).
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (string, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}
	var res hoverResult
	if err := c.call(ctx, "textDocument/hover", params, &res); err != nil {
		return "", err
	}
	return res.Contents.text, nil
}

// DocumentSymbols returns the symbols declared in a document.
func (c *Client) DocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := map[string]any{"textDocument": map[string]any{"uri": uri}}
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/documentSymbol", params, &raw); err != nil {
		return nil, err
	}
	return decodeSymbols(raw), nil
}
