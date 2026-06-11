// Package lsp — tools: expose a language server's navigation features as eigen
// tools (lsp_definition, lsp_references, lsp_hover, lsp_symbols,
// lsp_diagnostics). All are read-only, so they auto-run even in gated mode.
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/tool"
)

// maxResultBytes caps a tool's textual output so a huge references list can't
// blow the context budget.
const maxResultBytes = 16 * 1024

// LoadTools reads an lsp.json config and returns the LSP tools bound to a fresh
// Manager, plus the manager (which the caller must Close on exit). A missing
// config yields no tools and no error.
func LoadTools(root, path string) (defs []tool.Definition, mgr *Manager, errs []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, []error{err}
	}
	var cfg struct {
		Servers []ServerConfig `json:"servers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, []error{fmt.Errorf("%s: %w", path, err)}
	}
	var valid []ServerConfig
	for _, s := range cfg.Servers {
		if s.Disabled {
			continue
		}
		if s.Name == "" || len(s.Command) == 0 || len(s.Extensions) == 0 {
			errs = append(errs, fmt.Errorf("lsp server with empty name, command, or extensions"))
			continue
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return nil, nil, errs
	}
	mgr = NewManager(root, valid)
	return Tools(mgr), mgr, errs
}

// Tools returns the tool definitions backed by a manager.
func Tools(mgr *Manager) []tool.Definition {
	return []tool.Definition{
		definitionTool(mgr),
		referencesTool(mgr),
		hoverTool(mgr),
		symbolsTool(mgr),
		diagnosticsTool(mgr),
	}
}

// posArgs is the common {path,line,character} argument shape (1-based line, and
// 1-based character for humans; converted to LSP's 0-based internally).
type posArgs struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

func (p posArgs) position() Position {
	line := p.Line - 1
	if line < 0 {
		line = 0
	}
	ch := p.Character - 1
	if ch < 0 {
		ch = 0
	}
	return Position{Line: line, Character: ch}
}

const posSchema = `{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "File path (relative to the project or absolute)." },
    "line": { "type": "integer", "description": "1-based line number of the symbol." },
    "character": { "type": "integer", "description": "1-based column of the symbol on that line." }
  },
  "required": ["path", "line", "character"],
  "additionalProperties": false
}`

func definitionTool(mgr *Manager) tool.Definition {
	return tool.Definition{
		Name:        "lsp_definition",
		Description: "Go to definition: resolve where the symbol at a file:line:character is defined, using the language server. Returns file:line locations.",
		ReadOnly:    true,
		Parameters:  json.RawMessage(posSchema),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			in, err := parsePos(args)
			if err != nil {
				return "", err
			}
			client, uri, err := mgr.clientFor(ctx, in.Path)
			if err != nil {
				return "", err
			}
			locs, err := client.Definition(ctx, uri, in.position())
			if err != nil {
				return "", err
			}
			if len(locs) == 0 {
				return "(no definition found)", nil
			}
			return formatLocations(locs), nil
		},
	}
}

func referencesTool(mgr *Manager) tool.Definition {
	return tool.Definition{
		Name:        "lsp_references",
		Description: "Find all references to the symbol at a file:line:character, using the language server. Returns file:line locations.",
		ReadOnly:    true,
		Parameters:  json.RawMessage(posSchema),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			in, err := parsePos(args)
			if err != nil {
				return "", err
			}
			client, uri, err := mgr.clientFor(ctx, in.Path)
			if err != nil {
				return "", err
			}
			locs, err := client.References(ctx, uri, in.position(), true)
			if err != nil {
				return "", err
			}
			if len(locs) == 0 {
				return "(no references found)", nil
			}
			return formatLocations(locs), nil
		},
	}
}

func hoverTool(mgr *Manager) tool.Definition {
	return tool.Definition{
		Name:        "lsp_hover",
		Description: "Hover: get the type signature and documentation for the symbol at a file:line:character, using the language server.",
		ReadOnly:    true,
		Parameters:  json.RawMessage(posSchema),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			in, err := parsePos(args)
			if err != nil {
				return "", err
			}
			client, uri, err := mgr.clientFor(ctx, in.Path)
			if err != nil {
				return "", err
			}
			text, err := client.Hover(ctx, uri, in.position())
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(text) == "" {
				return "(no hover information)", nil
			}
			return tool.TruncateUTF8(text, maxResultBytes), nil
		},
	}
}

func symbolsTool(mgr *Manager) tool.Definition {
	return tool.Definition{
		Name:        "lsp_symbols",
		Description: "List the symbols (functions, types, methods, …) declared in a file, using the language server. Returns name, kind, and line.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "File path (relative to the project or absolute)." }
  },
  "required": ["path"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			client, uri, err := mgr.clientFor(ctx, in.Path)
			if err != nil {
				return "", err
			}
			syms, err := client.DocumentSymbols(ctx, uri)
			if err != nil {
				return "", err
			}
			if len(syms) == 0 {
				return "(no symbols found)", nil
			}
			var b strings.Builder
			for _, s := range syms {
				fmt.Fprintf(&b, "%s %s  (line %d)\n", SymbolKindName(s.Kind), s.Name, s.Range.Start.Line+1)
			}
			return tool.TruncateUTF8(strings.TrimRight(b.String(), "\n"), maxResultBytes), nil
		},
	}
}

func diagnosticsTool(mgr *Manager) tool.Definition {
	return tool.Definition{
		Name:        "lsp_diagnostics",
		Description: "Report compiler/linter diagnostics (errors and warnings) for a file from the language server.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "File path (relative to the project or absolute)." }
  },
  "required": ["path"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				return "", fmt.Errorf("path is required")
			}
			client, uri, err := mgr.clientFor(ctx, in.Path)
			if err != nil {
				return "", err
			}
			// Diagnostics arrive asynchronously after didOpen; give the server a
			// brief moment to publish them.
			diags := waitDiagnostics(ctx, client, uri)
			if len(diags) == 0 {
				return "(no diagnostics)", nil
			}
			sort.SliceStable(diags, func(i, j int) bool {
				return diags[i].Range.Start.Line < diags[j].Range.Start.Line
			})
			rel := displayPath(in.Path)
			var b strings.Builder
			for _, d := range diags {
				src := d.Source
				if src != "" {
					src = " [" + src + "]"
				}
				fmt.Fprintf(&b, "%s:%d:%d %s%s: %s\n",
					rel, d.Range.Start.Line+1, d.Range.Start.Character+1,
					SeverityName(d.Severity), src, d.Message)
			}
			return tool.TruncateUTF8(strings.TrimRight(b.String(), "\n"), maxResultBytes), nil
		},
	}
}

func parsePos(args json.RawMessage) (posArgs, error) {
	var in posArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return posArgs{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Path == "" {
		return posArgs{}, fmt.Errorf("path is required")
	}
	if in.Line < 1 {
		return posArgs{}, fmt.Errorf("line must be >= 1 (1-based)")
	}
	return in, nil
}

// formatLocations renders LSP locations as compact project-relative
// file:line:col lines.
func formatLocations(locs []Location) string {
	var b strings.Builder
	for _, l := range locs {
		p := displayPath(URIToPath(l.URI))
		fmt.Fprintf(&b, "%s:%d:%d\n", p, l.Range.Start.Line+1, l.Range.Start.Character+1)
	}
	return tool.TruncateUTF8(strings.TrimRight(b.String(), "\n"), maxResultBytes)
}

// displayPath shortens an absolute path to be relative to the working directory
// when possible, for readable output.
func displayPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}
