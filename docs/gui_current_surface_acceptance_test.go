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
		"Scope boundary",
		"current shipped surfaces",
		"full WCAG conformance certification",
		"Criterion-to-evidence map",
		"Premium desktop app shell exists",
		"Includes current app features",
		"End-to-end tested",
		"High UI/UX quality",
		"Measured no resource leak/misuse for covered flows",
		"Independent review and delivery gate",
		"27862913334",
		"27862913354",
		"ce860ca339ad6d50d7945ad0b8c37bef22113a93",
		"docs/gui-final-review-resolution.md",
		"Acceptance statement",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("current-surface acceptance missing %q", want)
		}
	}
}
