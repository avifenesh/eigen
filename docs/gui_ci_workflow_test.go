package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIPhaseWorkflowRunsVerificationScript(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/gui-phase.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"name: GUI phase gate",
		"pull_request:",
		"branches: [main]",
		"actions/setup-go@v5",
		"go-version-file: go.mod",
		"xterm tmux xvfb",
		"xvfb-run -a scripts/verify-gui-phase.sh",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("GUI phase workflow missing %q", want)
		}
	}
}
