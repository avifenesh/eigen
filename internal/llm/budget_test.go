package llm

import "testing"

func TestContextBudget(t *testing.T) {
	// A known 200k-window model with NO 1M beta: auto budget = 85% = 170k.
	const opus = "us.anthropic.claude-3-5-sonnet" // plain 200k window (no 1M beta)
	if got := ContextBudget(0, opus, 0); got != 170000 {
		t.Errorf("auto budget for %s: want 170000, got %d", opus, got)
	}

	// User ceiling below the window wins (min).
	if got := ContextBudget(50000, opus, 0); got != 50000 {
		t.Errorf("user ceiling 50k under window: want 50000, got %d", got)
	}

	// User ceiling ABOVE the model window is clamped to the window budget — the
	// model's actual limit always caps.
	if got := ContextBudget(10_000_000, opus, 0); got != 170000 {
		t.Errorf("user ceiling above window must clamp to 170000, got %d", got)
	}

	// Unknown model, no user setting: fall back to the provider default.
	if got := ContextBudget(0, "totally-unknown-model", 180000); got != 180000 {
		t.Errorf("unknown model default: want 180000, got %d", got)
	}

	// Unknown model with a user ceiling below the default wins.
	if got := ContextBudget(90000, "totally-unknown-model", 180000); got != 90000 {
		t.Errorf("user ceiling under provider default: want 90000, got %d", got)
	}

	// Unknown model, user ceiling above the default: default caps it.
	if got := ContextBudget(500000, "totally-unknown-model", 180000); got != 180000 {
		t.Errorf("user ceiling above provider default must clamp to 180000, got %d", got)
	}

	// Nothing known at all: 0.
	if got := ContextBudget(0, "totally-unknown-model", 0); got != 0 {
		t.Errorf("no window and no default: want 0, got %d", got)
	}

	// User ceiling with no model info still applies.
	if got := ContextBudget(75000, "totally-unknown-model", 0); got != 75000 {
		t.Errorf("user ceiling with no model info: want 75000, got %d", got)
	}
}

func TestContextBudget1MClampedByUserSetting(t *testing.T) {
	// fable-5 has a 1M window (beta default on) → auto = 850k. A user ceiling of
	// 200k (the Bedrock-TPM guard, now an explicit setting) must win.
	const fable = "global.anthropic.claude-fable-5"
	auto := ContextBudget(0, fable, 0)
	if auto < 800000 {
		t.Fatalf("expected ~850k auto budget for a 1M model, got %d (1M beta off?)", auto)
	}
	if got := ContextBudget(200000, fable, 0); got != 200000 {
		t.Errorf("user 200k ceiling on a 1M model: want 200000, got %d", got)
	}
}
