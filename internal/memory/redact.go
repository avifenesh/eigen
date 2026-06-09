package memory

import (
	"regexp"
)

// Redacted is the placeholder substituted for secret-looking tokens.
const Redacted = "[REDACTED_SECRET]"

// tokenPatterns match well-known credential prefixes (AWS, GitHub,
// OpenAI/Anthropic-style sk-, xAI, Slack, Google). Memory is plaintext on disk
// and injected into every future prompt, so false positives (over-redaction)
// are cheaper than leaks.
var tokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bxai-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{30,}\b`),
}

// assignPattern matches assignments/fields whose name implies a secret
// (api_key=..., TOKEN: "...", password=...). The value must be long enough to
// look like a credential, not a flag ("token=on"). The name is preserved so
// the note stays meaningful; only the value is scrubbed.
var assignPattern = regexp.MustCompile(`(?i)\b([A-Z0-9_]*?(?:api_?key|apikey|secret|token|passwd|password|credential)[A-Z0-9_]*)(\s*[:=]\s*)["']?([A-Za-z0-9+/_.~-]{12,})["']?`)

// authHeaderPattern matches Authorization header values (Bearer/Basic …).
var authHeaderPattern = regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)[A-Za-z0-9+/_.=-]{16,}`)

// pemBlock matches inline PEM private keys.
var pemBlock = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[^-]*-----END [A-Z ]*PRIVATE KEY-----`)

// Redact replaces secret-looking tokens in s with the Redacted placeholder.
// Surrounding text (key names, header schemes) is preserved so the note stays
// meaningful — only the credential value is scrubbed.
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = pemBlock.ReplaceAllString(s, Redacted)
	for _, re := range tokenPatterns {
		s = re.ReplaceAllString(s, Redacted)
	}
	s = assignPattern.ReplaceAllString(s, "$1$2"+Redacted)
	s = authHeaderPattern.ReplaceAllString(s, "$1$2"+Redacted)
	return s
}
