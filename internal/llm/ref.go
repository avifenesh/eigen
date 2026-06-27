package llm

import "strings"

// Model refs: ONE user-facing field names both the backend and the model.
//
//	us.anthropic.claude-opus-4-8          → catalog self-tags it (converse)
//	mantle:us.openai.gpt-5.5              → explicit provider tag
//	converse:us.anthropic.claude-opus-4-8  → explicit backend tag
//
// Most ids are unambiguous — the catalog knows their provider — so the tag is
// only needed for ids the catalog hasn't met (or to force a backend). The
// split triggers ONLY when the prefix is a known provider name: a model id
// that itself contains a colon (e.g. "…-v1:0") is never mis-split.

// ParseRef splits an optional "provider:model" ref. When the string carries
// no recognized provider tag it returns ("", s) — the id self-tags via the
// catalog (or the caller's own default).
func ParseRef(s string) (provider, model string) {
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return "", s
	}
	if p := s[:i]; knownProvider(p) {
		return p, s[i+1:]
	}
	return "", s
}

// knownProvider reports whether name is a recognized provider or alias.
func knownProvider(name string) bool {
	if name == "" {
		return false // canonicalProvider("") defaults to mantle; not a tag
	}
	switch canonicalProvider(name) {
	case "mantle", "converse", "anthropic", "codex", "llama", "grok", "glm", "moa":
		return true
	}
	_, ok := customProviderByName(name)
	return ok
}

// Ref renders the one-field form: just the model when the id self-tags (the
// catalog knows it — its provider wins at use time, so a tag would add noise
// or, worse, force a stale backend), "provider:model" only for ids the
// catalog doesn't know, where the provider field is the only signal.
func Ref(provider, model string) string {
	if provider == "" || model == "" {
		return model
	}
	if _, ok := Lookup(model); ok {
		return model // self-tags via the catalog
	}
	return provider + ":" + model
}
