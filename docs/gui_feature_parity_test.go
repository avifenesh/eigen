package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestGUIFeatureParityMatrixCoversAppPagesAndTUIPanels(t *testing.T) {
	b, err := os.ReadFile("gui-feature-parity.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, surface := range []string{
		"home", "live", "projects", "machines", "sessions", "config", "skills", "models", "providers", "memory", "crons", "plugins",
		"transcript", "composer", "plan", "left rail", "changes tab", "git tab", "terminal tab", "notepad tab", "tasks tab", "shells tab", "command palette", "app return",
	} {
		row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(surface) + ` \|.*\|.*Test[^|]*\|$`)
		if !row.MatchString(s) {
			t.Fatalf("feature parity matrix missing tested row for %q", surface)
		}
	}
	if !strings.Contains(s, "go test . ./docs ./internal/app ./internal/feed ./internal/tui -count=1") {
		t.Fatal("feature parity matrix missing broad verification command")
	}
}

func TestGUIFeatureParityMatrixRequiresAppFeatureJourneys(t *testing.T) {
	b, err := os.ReadFile("gui-feature-parity.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	required := map[string]string{
		"home":      "TestAppHomePageResumeFeatureJourney",
		"live":      "TestAppLivePageFeatureJourney",
		"projects":  "TestAppProjectsPageFeatureJourney",
		"machines":  "TestAppMachinesPageFeatureJourney",
		"sessions":  "TestAppSessionsPageFeatureJourney",
		"config":    "TestAppConfigMemorySkillsFeatureJourneys",
		"skills":    "TestAppConfigMemorySkillsFeatureJourneys",
		"providers": "TestAppProvidersModelsCronsFeatureJourneys",
		"models":    "TestAppProvidersModelsCronsFeatureJourneys",
		"memory":    "TestAppConfigMemorySkillsFeatureJourneys",
		"crons":     "TestAppProvidersModelsCronsFeatureJourneys",
		"plugins":   "TestAppPluginsPageFeatureJourney",
	}
	for surface, testName := range required {
		row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(surface) + ` \|.*\|.*` + regexp.QuoteMeta(testName) + `.*\|$`)
		if !row.MatchString(s) {
			t.Fatalf("app surface %q must cite feature journey %s", surface, testName)
		}
	}

}

func TestGUIFeatureParityMatrixRequiresTUIFeatureJourneys(t *testing.T) {
	b, err := os.ReadFile("gui-feature-parity.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	required := map[string]string{
		"transcript":      "TestTUIComposerTranscriptJourney",
		"composer":        "TestTUIComposerTranscriptJourney",
		"tasks tab":       "TestTUITasksPanelFeatureJourney",
		"shells tab":      "TestTUIShellsPanelFeatureJourney",
		"plan":            "TestTUIPlanPanelFeatureJourney",
		"left rail":       "TestTUILeftRailFeatureJourney",
		"command palette": "TestTUIKeyboardE2EHomeAndBackgroundActions",
		"app return":      "TestTUIKeyboardE2EHomeAndBackgroundActions",
	}
	for surface, testName := range required {
		row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(surface) + ` \|.*\|.*` + regexp.QuoteMeta(testName) + `.*\|$`)
		if !row.MatchString(s) {
			t.Fatalf("TUI surface %q must cite feature journey %s", surface, testName)
		}
	}
	panelJourneys := map[string]string{
		"changes tab":  "TestTUIChangesPanelFeatureJourney",
		"git tab":      "TestTUIGitPanelFeatureJourney",
		"terminal tab": "TestTUITerminalPanelFeatureJourney",
		"notepad tab":  "TestTUINotepadPanelFeatureJourney",
	}
	for surface, testName := range panelJourneys {
		row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(surface) + ` \|.*\|.*` + regexp.QuoteMeta(testName) + `.*\|$`)
		if !row.MatchString(s) {
			t.Fatalf("TUI surface %q must cite feature journey %s", surface, testName)
		}
	}
}
