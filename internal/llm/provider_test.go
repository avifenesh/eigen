package llm

import "testing"

// TestNewStripsNameSuffix guards the daemon model-switch regression: a model
// id that accidentally carried a Name() suffix like "… (bedrock converse)"
// (an old Remote.SetModel sent Name() instead of ModelID(), and the daemon
// persisted it) must still resolve to the right backend instead of falling
// through to the default provider with the wrong auth.
func TestNewStripsNameSuffix(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")

	clean := "us.anthropic.claude-haiku-4-5-20251001-v1:0"
	poisoned := clean + " (bedrock converse)"

	p, err := New("", poisoned)
	if err != nil {
		t.Fatalf("New with suffixed id: %v", err)
	}
	if p.ModelID() != clean {
		t.Fatalf("ModelID = %q, want %q (suffix not stripped)", p.ModelID(), clean)
	}
	// And it routed to converse (the catalog's backend for this id), not the
	// default mantle provider — Name() carries the backend label.
	if got := p.Name(); got != clean+" (bedrock converse)" {
		t.Fatalf("Name = %q, want converse backend", got)
	}
}

// TestProvidersExposeModelID checks every constructible provider returns a
// suffix-free ModelID equal to the requested id (the value SetModel sends over
// the daemon socket).
func TestProvidersExposeModelID(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "tok")
	t.Setenv("XAI_API_KEY", "xai")
	t.Setenv("GLM_API_KEY", "glm")
	t.Setenv("ZHIPUAI_API_KEY", "glm")

	cases := []struct{ provider, model string }{
		{"converse", "us.anthropic.claude-opus-4-8"},
		{"mantle", "openai.gpt-5.5"},
		{"grok", "grok-build"},
	}
	for _, tc := range cases {
		p, err := New(tc.provider, tc.model)
		if err != nil {
			t.Logf("skip %s/%s: %v", tc.provider, tc.model, err)
			continue
		}
		if p.ModelID() != tc.model {
			t.Errorf("%s: ModelID = %q, want %q", tc.provider, p.ModelID(), tc.model)
		}
	}
}
