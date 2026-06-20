package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUICurrentSurfaceGateDocumentsAcceptance(t *testing.T) {
	b, err := os.ReadFile("gui-phase-gate.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"full GUI parity milestone",
		"docs/gui-current-surface-acceptance.md",
		"native/browser desktop GUI local-only server",
		"scripts/verify-gui-phase.sh",
		"go test ./... -count=1",
		"go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1",
		"node --check internal/gui/static/app.js",
		"scripts/gui-smoke.sh",
		"go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1",
		"go test -tags smoke . -run",
		"CI and GUI phase gate are green on the default-branch commit",
		"27862913334",
		"27862913354",
		"no shipped GUI feature is recorded as missing",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("full parity gate missing %q", want)
		}
	}
}
