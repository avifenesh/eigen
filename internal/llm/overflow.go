package llm

import "strings"

// IsContextOverflow reports whether err looks like the provider rejecting a
// request because the prompt exceeds the model's context window (HTTP 413 or a
// "prompt is too long" / "maximum context length" style message). This is
// distinct from a token-per-minute rate limit (429): the fix is to shrink the
// conversation and retry, not to wait. Matching is by message substring because
// providers don't share a typed error for this.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	// Avoid false positives on rate-limit phrasing ("too many tokens").
	if strings.Contains(s, "too many tokens") {
		return false
	}
	return strings.Contains(s, "413") ||
		strings.Contains(s, "prompt is too long") ||
		strings.Contains(s, "prompt too long") ||
		strings.Contains(s, "context length") ||
		strings.Contains(s, "context window") ||
		strings.Contains(s, "maximum context") ||
		strings.Contains(s, "input is too long") ||
		strings.Contains(s, "too many total text bytes") ||
		(strings.Contains(s, "context") && strings.Contains(s, "exceed")) ||
		(strings.Contains(s, "tokens") && strings.Contains(s, "maximum") && strings.Contains(s, "exceed"))
}
