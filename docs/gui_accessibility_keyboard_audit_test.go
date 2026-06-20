package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIAccessibilityKeyboardAuditMapsParityEvidence(t *testing.T) {
	b, err := os.ReadFile("gui-accessibility-keyboard-audit.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"keyboard-parity evidence map, keyboard-parity evidence, not an external certification document",
		"Every clickable TUI chrome action dispatches through the same validated action registry",
		"model picker",
		"permission picker",
		"session rail",
		"right panel/tabs",
		"notepad",
		"terminal panel",
		"tasks/shells",
		"tray/approvals",
		"command palette",
		"copy/read/voice controls",
		"tool turn output",
		"App shell keyboard parity",
		"Native/browser GUI accessibility seams",
		"TestTUIToolTurnDrivesPlanChangesAndTaskPanels",
		"TestServeRejectsNonLocalBind",
		"TestHandlerStaticAndAPIContracts",
		"TestServiceValidationErrors",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("keyboard audit missing %q", want)
		}
	}
}
