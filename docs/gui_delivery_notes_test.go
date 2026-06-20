package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIDeliveryNotesRecordScopeAndPreexistingStagedFiles(t *testing.T) {
	b, err := os.ReadFile("gui-delivery-notes.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"internal/gui",
		"internal/app",
		"internal/tui",
		"internal/feed",
		"every-page and feature-specific journeys",
		"composer/plan/rail/right-panel feature journeys",
		"internal/command/command_test.go",
		"internal/memory/memory_test.go",
		"internal/memory/redact_test.go",
		"Pre-existing staged files not owned",
		"scripts/verify-gui-phase.sh",
		"go test ./... -count=1",
		"go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1",
		"go test ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -shuffle=on -count=1",
		"go test -tags 'wails production webkit2_41' ./internal/gui -count=1",
		"node --check internal/gui/static/app.js",
		"node internal/gui/static/app_behavior_test.mjs",
		"scripts/gui-smoke.sh",
		"go test -race ./internal/app ./internal/feed ./internal/tui -count=1",
		"go test -tags smoke . -count=1",
		"TestPTYReleaseAppShellLongerSoak",
		"-count=5",
		"docs/gui-current-surface-acceptance.md",
		"no shipped GUI feature-parity blocker remains",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("GUI delivery notes missing %q", want)
		}
	}
}
