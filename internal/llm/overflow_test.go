package llm

import (
	"errors"
	"testing"
)

func TestIsContextOverflow(t *testing.T) {
	overflow := []string{
		"HTTP 413: payload too large",
		"prompt is too long: 250000 tokens > 200000 maximum",
		"input is too long for requested model",
		"This model's maximum context length is 128000 tokens",
		"ValidationException: context window exceeded",
		"too many total text bytes",
	}
	for _, s := range overflow {
		if !IsContextOverflow(errors.New(s)) {
			t.Errorf("expected overflow for %q", s)
		}
	}

	notOverflow := []string{
		"",
		"HTTP 429: too many tokens per minute", // rate limit, not size
		"HTTP 503: service unavailable",
		"connection reset by peer",
		"invalid api key",
	}
	for _, s := range notOverflow {
		if s == "" {
			if IsContextOverflow(nil) {
				t.Error("nil error must not be overflow")
			}
			continue
		}
		if IsContextOverflow(errors.New(s)) {
			t.Errorf("did not expect overflow for %q", s)
		}
	}
}
