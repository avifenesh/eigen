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
		"No functional blocker from the prior review was reclassified as incomplete maintenance work",
		"Blocker 2: gate circularity check",
		"does **not** change `scripts/verify-gui-phase.sh` or `.github/workflows/gui-phase.yml`",
		"Blocker 3: evidence commit boundary check",
		"Default branch `origin/main`",
		"27863744643",
		"27863744647",
		"does not read `docs/gui-phase-summary.json`",
		"8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9",
		"The prior accessibility blocker was overclaiming language",
		"The review blockers are resolved",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("final review resolution missing %q", want)
		}
	}
}
