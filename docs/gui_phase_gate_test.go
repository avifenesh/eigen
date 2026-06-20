package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIPhaseGatePreventsPrematureFullGoalClaim(t *testing.T) {
	b, err := os.ReadFile("gui-phase-gate.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"phase-complete evidence slice",
		"not a final claim",
		"Do **not** call the whole goal complete",
		"Deep feature journeys for every parity row",
		"Longer real-binary soak",
		"Visual snapshot/golden workflow",
		"Independent review blockers fixed",
		"release smoke commands now fail explicitly",
		"notepad dirty notes flush on quit",
		"not, by this document's own criteria, enough to claim the full persistent goal",
		"native/browser desktop GUI local-only server",
		"scripts/verify-gui-phase.sh",
		"go test ./... -count=1",
		"go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1",
		"go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1",
		"go test -tags smoke . -run",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("phase gate missing %q", want)
		}
	}
}
