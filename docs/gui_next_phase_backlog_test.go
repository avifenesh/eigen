package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUINextPhaseBacklogKeepsFullGoalActionable(t *testing.T) {
	b, err := os.ReadFile("gui-next-phase-backlog.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"scripts/verify-gui-phase.sh",
		"Real desktop sandbox journey",
		"Started with an isolated X11 workspace terminal",
		"release binary premium app shell",
		"release-app-shell.png",
		"chat-tui-shell.png",
		"smoke-tagged chat TUI in the isolated desktop terminal",
		"Longer live binary soak",
		"TestPTYReleaseAppShellLongerSoak",
		"Richer visual review artifacts",
		"Chat TUI end-to-end agent turn with tools",
		"Accessibility/keyboard audit",
		"Clean-tree delivery gate",
		"Independent final review",
		"goal_achieved",
		"P0 items are complete",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("next-phase backlog missing %q", want)
		}
	}
}
