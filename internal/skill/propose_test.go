package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProposeAcceptReject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Propose two skills.
	p1, err := Propose("fix-flaky-test", "when a test flakes under load", "1. find the race\n2. fix it")
	if err != nil || p1 == "" {
		t.Fatalf("propose: %v path=%q", err, p1)
	}
	Propose("serve-model", "how to serve the local model", "run serve.sh")
	props := Proposals()
	if len(props) != 2 {
		t.Fatalf("want 2 proposals, got %d", len(props))
	}
	if props[0].Description == "" {
		t.Fatal("proposal description should parse from frontmatter")
	}

	// Accept one → moves to active skills, gone from proposals.
	active, err := Accept("fix-flaky-test")
	if err != nil || !strings.HasSuffix(active, "skills/fix-flaky-test/SKILL.md") {
		t.Fatalf("accept: %v path=%q", err, active)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatal("accepted skill should exist in active dir")
	}
	if len(Proposals()) != 1 {
		t.Fatal("accepted proposal should leave the proposals list")
	}
	// Discover finds the accepted skill.
	set := Discover(filepath.Dir(filepath.Dir(active)))
	if set == nil || func() bool { n, ok := set.Resolve("fix-flaky-test"); return !ok || n == "" }() {
		t.Fatal("accepted skill should be discoverable")
	}

	// Re-accepting an active skill fails.
	if _, err := Accept("fix-flaky-test"); err == nil {
		t.Fatal("accepting an active skill should fail")
	}
	// Propose of an already-active skill is a no-op (empty path).
	if p, _ := Propose("fix-flaky-test", "x", "y"); p != "" {
		t.Fatal("proposing an already-active skill should no-op")
	}

	// Reject the other.
	if err := Reject("serve-model"); err != nil {
		t.Fatal(err)
	}
	if len(Proposals()) != 0 {
		t.Fatal("reject should remove the proposal")
	}
}

// A repeated dream pass must not silently overwrite a pending proposal the user
// is about to review: the first proposal wins, later passes no-op.
func TestProposePreservesFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p1, err := Propose("dedupe-logs", "collapse repeated log lines", "1. detect runs\n2. collapse")
	if err != nil || p1 == "" {
		t.Fatalf("first propose: %v path=%q", err, p1)
	}
	original, err := os.ReadFile(p1)
	if err != nil {
		t.Fatalf("read first proposal: %v", err)
	}

	// A later pass with a refined body must NOT clobber the pending proposal.
	p2, err := Propose("dedupe-logs", "collapse repeated log lines (refined)", "totally different body")
	if err != nil {
		t.Fatalf("second propose: %v", err)
	}
	if p2 != "" {
		t.Fatalf("re-proposing a pending name should no-op (got path %q)", p2)
	}
	if len(Proposals()) != 1 {
		t.Fatalf("want 1 proposal after re-propose, got %d", len(Proposals()))
	}
	after, err := os.ReadFile(p1)
	if err != nil {
		t.Fatalf("read proposal after re-propose: %v", err)
	}
	if string(after) != string(original) {
		t.Fatal("pending proposal must be preserved, not overwritten by a later pass")
	}
}
