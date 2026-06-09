package dream

import (
	"context"
	"strings"
	"testing"
)

const memSample = `- 2026-01-01 — build with go build ./...
- 2026-01-02 — model X is broken
- 2026-01-03 — model X works now
- 2026-01-03 — build with go build ./...
`

func TestConsolidateRewrites(t *testing.T) {
	f := &fakeProv{reply: "- 2026-01-03 — model X works now\n- 2026-01-03 — build with go build ./...\n"}
	out, err := Consolidate(context.Background(), f, memSample)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "model X works now") {
		t.Fatalf("consolidated output missing content: %q", out)
	}
	if !strings.Contains(f.gotSystem, "RECENCY WINS") {
		t.Fatal("consolidation prompt should encode the recency rule")
	}
	if !strings.Contains(f.gotUser, "go build ./...") {
		t.Fatal("current memory should be sent to the model")
	}
}

func TestConsolidateFailsClosedOnEmpty(t *testing.T) {
	f := &fakeProv{reply: "   "}
	if _, err := Consolidate(context.Background(), f, memSample); err == nil {
		t.Fatal("empty output must be rejected")
	}
}

func TestConsolidateFailsClosedOnNonBullets(t *testing.T) {
	f := &fakeProv{reply: "I have consolidated your memory successfully."}
	if _, err := Consolidate(context.Background(), f, memSample); err == nil {
		t.Fatal("non-bullet output must be rejected")
	}
}

func TestConsolidateFailsClosedOnMassiveShrink(t *testing.T) {
	big := strings.Repeat("- 2026-01-01 — a long detailed note about the build system and its many flags\n", 200)
	f := &fakeProv{reply: "- 2026-01-01 — stuff\n"}
	if _, err := Consolidate(context.Background(), f, big); err == nil {
		t.Fatal(">90% shrink must be rejected as likely destructive")
	}
}

func TestConsolidateEmptyMemoryErrors(t *testing.T) {
	f := &fakeProv{reply: "- x\n"}
	if _, err := Consolidate(context.Background(), f, "  "); err == nil {
		t.Fatal("empty memory should error (nothing to consolidate)")
	}
}
