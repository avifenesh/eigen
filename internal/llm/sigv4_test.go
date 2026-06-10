package llm

import "testing"

func TestCanonicalURIDoubleEncodes(t *testing.T) {
	// A plain path is unchanged.
	if got := canonicalURI("/model/us.anthropic.claude-opus-4-8/converse"); got != "/model/us.anthropic.claude-opus-4-8/converse" {
		t.Errorf("plain path changed: %q", got)
	}
	// An already-escaped ':' (%3A) is re-encoded to %253A (AWS double-encoding),
	// which is what makes versioned Bedrock profile ids sign correctly.
	got := canonicalURI("/model/us.anthropic.claude-haiku-4-5-20251001-v1%3A0/converse")
	want := "/model/us.anthropic.claude-haiku-4-5-20251001-v1%253A0/converse"
	if got != want {
		t.Errorf("double-encode: got %q want %q", got, want)
	}
}

func TestAWSURIEncode(t *testing.T) {
	if awsURIEncode("abc-_.~") != "abc-_.~" {
		t.Error("unreserved chars must pass through")
	}
	if awsURIEncode("%3A") != "%253A" {
		t.Error("percent must be re-encoded")
	}
}
