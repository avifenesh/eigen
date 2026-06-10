package llm

import "testing"

func TestVendorOf(t *testing.T) {
	cases := map[string]Vendor{
		"us.anthropic.claude-opus-4-8": VendorAnthropic,
		"claude-fable-5":               VendorAnthropic,
		"openai.gpt-5.5":               VendorOpenAI,
		"gpt-5":                        VendorOpenAI,
		"grok-build":                   VendorXAI,
		"glm-5.1":                      VendorZhipu,
		"local":                        VendorUnknown,
	}
	for id, want := range cases {
		if got := VendorOf(id); got != want {
			t.Errorf("VendorOf(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestCrossReviewerNeverSelfVendor(t *testing.T) {
	all := []string{}
	for _, m := range Catalog {
		all = append(all, m.ID)
	}
	// Claude author → GPT reviewer.
	rev := CrossReviewer("us.anthropic.claude-opus-4-8", all)
	if VendorOf(rev) != VendorOpenAI {
		t.Fatalf("Claude should be reviewed by GPT, got %s (%v)", rev, VendorOf(rev))
	}
	// GPT author → Claude reviewer.
	rev = CrossReviewer("openai.gpt-5.5", all)
	if VendorOf(rev) != VendorAnthropic {
		t.Fatalf("GPT should be reviewed by Claude, got %s (%v)", rev, VendorOf(rev))
	}
	// Grok author → GPT (strict) reviewer (other vendor, not self).
	rev = CrossReviewer("grok-build", all)
	if VendorOf(rev) == VendorXAI {
		t.Fatalf("grok must not review itself, got %s", rev)
	}
}

func TestCrossReviewerPicksStrongest(t *testing.T) {
	// Among GPT candidates, the strongest (highest tier+rank) is chosen.
	rev := CrossReviewer("claude-fable-5", []string{"openai.gpt-5", "openai.gpt-5.5", "openai.gpt-5.4"})
	if rev != "openai.gpt-5.5" {
		t.Fatalf("should pick the strongest GPT reviewer (gpt-5.5), got %s", rev)
	}
}

func TestCrossReviewerNoneAvailable(t *testing.T) {
	// Only same-vendor candidates → no cross-vendor reviewer.
	if rev := CrossReviewer("us.anthropic.claude-opus-4-8", []string{"claude-fable-5", "us.anthropic.claude-haiku-4-5"}); rev != "" {
		t.Fatalf("no cross-vendor reviewer should be \"\", got %s", rev)
	}
}
