package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIFinalAcceptanceChecklistTracksCIStatus(t *testing.T) {
	b, err := os.ReadFile("gui-final-acceptance-checklist.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"scripts/verify-gui-phase.sh",
		".github/workflows/gui-phase.yml",
		"Green GitHub Actions evidence for PR #3",
		"27859059893",
		"https://github.com/avifenesh/eigen/actions/runs/27859059893",
		"42c8a08f8b4752495f42e6a5aafc6aa0ae8c4077",
		"observed status: success",
		"Final-claim status",
		"human-attested",
		"CI does not yet regenerate screenshots",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("final acceptance checklist missing %q", want)
		}
	}
}
