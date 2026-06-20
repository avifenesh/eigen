package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUICurrentSurfaceAcceptanceMapsGoalCriteria(t *testing.T) {
	b, err := os.ReadFile("gui-current-surface-acceptance.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"shipped Eigen surfaces",
		"Criterion-to-evidence map",
		"Premium desktop app shell exists",
		"Includes all Eigen GUI features",
		"TestAppCodeDerivedFeatureInventoryHasJourneyEvidence",
		"TestTUICodeDerivedFeatureInventoryHasJourneyEvidence",
		"End-to-end tested",
		"High UI/UX quality",
		"Measured no resource leak/misuse for covered flows",
		"Independent review and delivery gate",
		"27863744643",
		"27863744647",
		"scripts/verify-gui-phase.sh` validates the shipped surfaces",
		"go test -tags 'wails production webkit2_41' ./internal/gui -count=1",
		"node internal/gui/static/app_behavior_test.mjs",
		"8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9",
		"docs/gui-final-review-resolution.md",
		"Acceptance statement",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("full parity acceptance missing %q", want)
		}
	}
}
