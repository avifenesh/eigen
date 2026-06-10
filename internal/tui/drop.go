package tui

// Drag-and-drop file support. Terminals deliver a dropped file as a bracketed
// paste of its path — but encoded inconsistently: file:// URIs, shell-quoted
// or backslash-escaped paths, percent-encoding, and (on multi-file drops)
// several paths separated by spaces or newlines. normalizeDropped turns those
// into clean, space-free path tokens that the model can read like an @file
// mention. eigen already treats a bare path in the prompt as a file reference,
// so a dropped file just becomes text in the input.

import (
	"net/url"
	"strings"
)

// looksLikeDrop reports whether a pasted payload is (probably) one or more
// dropped file paths rather than ordinary text: it is when every whitespace-
// separated token is a file:// URI or an existing-looking absolute/home path.
// Plain prose (which contains spaces inside a token, or relative words) is left
// untouched so normal paste is unaffected.
func looksLikeDrop(s string) bool {
	fields := splitDropTokens(s)
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if !strings.HasPrefix(f, "file://") && !strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "~/") {
			return false
		}
	}
	return true
}

// normalizeDropped converts a dropped-file payload into a space-joined list of
// clean path tokens (paths containing spaces are single-quoted so they stay one
// token). Returns the input unchanged when it does not look like a drop.
func normalizeDropped(s string) string {
	if !looksLikeDrop(s) {
		return s
	}
	var out []string
	for _, tok := range splitDropTokens(s) {
		out = append(out, quoteIfSpaced(decodePath(tok)))
	}
	return strings.Join(out, " ")
}

// splitDropTokens splits a drop payload into candidate path tokens. file://
// URIs and newline separation are unambiguous; for space separation we only
// split when the payload has no quotes (a quoted path may legitimately contain
// spaces, handled by the caller leaving it as one token).
func splitDropTokens(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Newlines always separate multiple drops.
	if strings.ContainsAny(s, "\n\r") {
		return nonEmptyFields(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }))
	}
	// A single quoted token: keep as one.
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) {
		return []string{s}
	}
	// Multiple file:// URIs separated by spaces.
	if strings.HasPrefix(s, "file://") && strings.Contains(s, " file://") {
		return nonEmptyFields(strings.Split(s, " "))
	}
	return []string{s}
}

// decodePath resolves a single dropped token to a filesystem path: strips a
// file:// scheme (percent-decoding the rest), and removes shell quoting /
// backslash escaping.
func decodePath(tok string) string {
	tok = strings.TrimSpace(tok)
	if rest, ok := strings.CutPrefix(tok, "file://"); ok {
		// file://host/path — drop an optional host component.
		if i := strings.IndexByte(rest, '/'); i > 0 {
			rest = rest[i:]
		}
		if dec, err := url.PathUnescape(rest); err == nil {
			return dec
		}
		return rest
	}
	// Strip surrounding quotes.
	if len(tok) >= 2 {
		if (tok[0] == '\'' && tok[len(tok)-1] == '\'') || (tok[0] == '"' && tok[len(tok)-1] == '"') {
			return tok[1 : len(tok)-1]
		}
	}
	// Unescape "\ " style shell escaping.
	return strings.ReplaceAll(tok, `\ `, " ")
}

// quoteIfSpaced single-quotes a path that contains spaces so it stays one
// token in the input (and reads as one path to the model).
func quoteIfSpaced(p string) string {
	if strings.ContainsAny(p, " \t") {
		return "'" + p + "'"
	}
	return p
}

func nonEmptyFields(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
