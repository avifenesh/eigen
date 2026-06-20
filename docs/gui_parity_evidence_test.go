package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIParityEvidenceMentionsKeyRegressionTests(t *testing.T) {
	b, err := os.ReadFile("gui-parity-evidence.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"TestAppPremiumShellVisualContract",
		"TestAppHomeGoldenSnapshotTokens",
		"TestAppEveryPageGoldenSnapshotTokens",
		"TestAppPaletteGoldenSnapshotTokens",
		"TestAppEveryPageKeyboardJourney",
		"TestAppEveryPageQuickJumpJourney",
		"TestAppHomePageResumeFeatureJourney",
		"TestAppLivePageFeatureJourney",
		"TestAppSessionsPageFeatureJourney",
		"TestAppProjectsPageFeatureJourney",
		"TestAppMachinesPageFeatureJourney",
		"TestAppConfigMemorySkillsFeatureJourneys",
		"TestAppProvidersModelsCronsFeatureJourneys",
		"TestAppPluginsPageFeatureJourney",
		"TestAppViewPaintsFullCanvas",
		"TestAppRenderSoakPaintsAndDoesNotLeakGoroutines",
		"TestAppKeyboardE2ENavigatePaletteAndOpen",
		"TestScanGitHubCanceledSkipsCommands",
		"TestScanSuggestCanceledSkipsModel",
		"TestNotepadTabSwitchFlushesDirtyNotes",
		"TestNotepadQuitFlushesDirtyNotes",
		"TestGitLinesUsesCachedSummary",
		"TestGitSummaryRefreshesOnUpdateEventsOnly",
		"TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines",
		"TestTUILiveLoopResourceMeasurement",
		"TestTUIPlanPanelFeatureJourney",
		"TestTUILeftRailFeatureJourney",
		"TestTUIChangesPanelFeatureJourney",
		"TestTUIGitPanelFeatureJourney",
		"TestTUITerminalPanelFeatureJourney",
		"TestTUINotepadPanelFeatureJourney",
		"TestTUIEveryRightPanelTabKeyboardJourney",
		"TestTUITasksPanelFeatureJourney",
		"TestTUIShellsPanelFeatureJourney",
		"TestTUIRightPanelCycleKeyboardJourney",
		"TestMentionFileIndexCachesBetweenKeystrokes",
		"TestTUIKeyboardE2EHomeAndBackgroundActions",
		"TestTUIComposerTranscriptJourney",
		"TestTUIEmptyComposerGoldenSnapshotTokens",
		"TestTUIComposerMentionMenuGoldenSnapshotTokens",
		"TestTUIComposerTranscriptGoldenSnapshotTokens",
		"TestTUIRightPanelGitGoldenSnapshotTokens",
		"TestTUIEveryRightPanelTabGoldenSnapshotTokens",
		"TestTUISlashCommandJourneyFromComposer",
		"TestCLISmokeVersionAndTheme",
		"TestProductionSmokeCommandsFailExplicitly",
		"Independent review blockers",
		"TestReleaseBinaryDoesNotExposeSmokeCommands",
		"TestSmokeTaggedBinaryBuilds",
		"TestPTYSmokeVersionCommand",
		"Test-only smoke isolation",
		"TestPTYSmokeAppShellKeyboardNavigation",
		"TestPTYAppShellNavigationSoak",
		"TestPTYReleaseAppShellLongerSoak",
		"TestPTYChatTUISmokeQuit",
		"CI enforcement",
		"TestGUIPhaseWorkflowRunsVerificationScript",
		"scripts/verify-gui-phase.sh",
		"go test ./... -count=1",
		"go test . ./docs ./internal/app ./internal/feed ./internal/tui -count=1",
		"go test -tags smoke . -count=1",
		"go test ./docs ./internal/app ./internal/feed ./internal/tui -shuffle=on -count=1",
		"go test -race ./internal/app ./internal/feed ./internal/tui -count=1",
		"go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1",
		"go test -tags smoke . -run",
		"-count=5",
		"docs/gui-phase-gate.md",
		"docs/gui-delivery-notes.md",
		"Remaining gaps before claiming full persistent goal",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("GUI parity evidence missing %q", want)
		}
	}
}
