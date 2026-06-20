package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIFinalReviewResolutionAnswersBlockers(t *testing.T) {
	b, err := os.ReadFile("gui-final-review-resolution.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"Blocker 1: goalpost-moving check",
		"No functional blocker from the prior review was demoted to future scope",
		"Blocker 2: gate circularity check",
		"does **not** change `scripts/verify-gui-phase.sh` or `.github/workflows/gui-phase.yml`",
		"Blocker 3: evidence commit boundary check",
		"PR #5 delta is documentation-only",
		"27863260532",
		"27863260578",
		"ce860ca339ad6d50d7945ad0b8c37bef22113a93",
		"The review blockers are resolved",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("final review resolution missing %q", want)
		}
	}
}
