package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// PathToURI converts an absolute filesystem path to a file:// URI.
func PathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	// Windows drive paths need a leading slash: /C:/x.
	if runtime.GOOS == "windows" && !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	u := url.URL{Scheme: "file", Path: abs}
	return u.String()
}

// URIToPath converts a file:// URI back to a filesystem path. Non-file URIs are
// returned unchanged.
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
	}
	return filepath.FromSlash(p)
}

// hoverContents flattens the several shapes textDocument/hover may return into
// a single plain-text string.
type hoverContents struct {
	text string
}

func (h *hoverContents) UnmarshalJSON(b []byte) error {
	h.text = flattenMarkup(b)
	return nil
}

// flattenMarkup turns any hover-contents JSON (string | MarkedString |
// MarkupContent | array of those) into joined plain text.
func flattenMarkup(b []byte) string {
	// string
	var s string
	if json.Unmarshal(b, &s) == nil {
		return strings.TrimSpace(s)
	}
	// object: {value: "..."} (MarkupContent or MarkedString)
	var obj struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(b, &obj) == nil && obj.Value != "" {
		return strings.TrimSpace(obj.Value)
	}
	// array of string|object
	var arr []json.RawMessage
	if json.Unmarshal(b, &arr) == nil {
		var parts []string
		for _, el := range arr {
			if p := flattenMarkup(el); p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// decodeLocations normalizes a Location | []Location | LocationLink[] | null
// result into a slice.
func decodeLocations(raw json.RawMessage) []Location {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// []Location
	var list []Location
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 && list[0].URI != "" {
		return list
	}
	// single Location
	var one Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return []Location{one}
	}
	// LocationLink[] (definition may return these): {targetUri, targetRange}
	var links []struct {
		TargetURI            string `json:"targetUri"`
		TargetRange          Range  `json:"targetRange"`
		TargetSelectionRange Range  `json:"targetSelectionRange"`
	}
	if json.Unmarshal(raw, &links) == nil {
		out := make([]Location, 0, len(links))
		for _, l := range links {
			if l.TargetURI != "" {
				out = append(out, Location{URI: l.TargetURI, Range: l.TargetRange})
			}
		}
		return out
	}
	return nil
}

// decodeSymbols normalizes a DocumentSymbol[] (hierarchical) or
// SymbolInformation[] (flat) result into a flat list of DocumentSymbol.
func decodeSymbols(raw json.RawMessage) []DocumentSymbol {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var syms []DocumentSymbol
	if json.Unmarshal(raw, &syms) != nil {
		return nil
	}
	var flat []DocumentSymbol
	var walk func(s DocumentSymbol)
	walk = func(s DocumentSymbol) {
		// SymbolInformation carries its span in Location.Range; copy it up so
		// callers always have a usable Range.
		if s.Range == (Range{}) && s.Location.URI != "" {
			s.Range = s.Location.Range
		}
		children := s.Children
		s.Children = nil
		flat = append(flat, s)
		for _, ch := range children {
			walk(ch)
		}
	}
	for _, s := range syms {
		walk(s)
	}
	return flat
}

// SymbolKindName maps an LSP SymbolKind number to a readable name.
func SymbolKindName(kind int) string {
	names := map[int]string{
		1: "file", 2: "module", 3: "namespace", 4: "package", 5: "class",
		6: "method", 7: "property", 8: "field", 9: "constructor", 10: "enum",
		11: "interface", 12: "function", 13: "variable", 14: "constant",
		15: "string", 16: "number", 17: "boolean", 18: "array", 19: "object",
		20: "key", 21: "null", 22: "enum-member", 23: "struct", 24: "event",
		25: "operator", 26: "type-parameter",
	}
	if n, ok := names[kind]; ok {
		return n
	}
	return "symbol"
}

// SeverityName maps an LSP DiagnosticSeverity to a readable label.
func SeverityName(sev int) string {
	switch sev {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "diagnostic"
	}
}
