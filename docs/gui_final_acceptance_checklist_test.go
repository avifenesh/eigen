package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIFinalAcceptanceChecklistTracksBlockers(t *testing.T) {
	b, err := os.ReadFile("gui-final-acceptance-checklist.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"scripts/verify-gui-phase.sh",
		".github/workflows/gui-phase.yml",
		"Green CI run evidence",
		"commit SHA plus run URL or run ID",
		"HTTP 404: workflow gui-phase.yml not found on the default branch",
		"Docs completion reconciliation",
		"`goal_achieved` would contradict repository evidence",
		"human-attested",
		"CI does not yet regenerate screenshots",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("final acceptance checklist missing %q", want)
		}
	}
}
