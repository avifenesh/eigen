package tui

import "testing"

func TestTUICodeDerivedFeatureInventoryHasJourneyEvidence(t *testing.T) {
	actionEvidence := map[actionID][]string{
		actModelPicker:     {"TestClickStatusSegmentDispatches"},
		actPermPicker:      {"TestPermClickOpensConfirmNotBlindToggle"},
		actEffortCycle:     {"TestClickStatusSegmentDispatches"},
		actSearchCycle:     {"TestClickStatusSegmentDispatches"},
		actFastToggle:      {"TestClickStatusSegmentDispatches"},
		actRouteToggle:     {"TestClickStatusSegmentDispatches"},
		actCompactPrompt:   {"TestCompactClickOpensConfirmNotBlindRun"},
		actReadAloudToggle: {"TestSpeechQueueSpeaksAllAndCloses"},
		actVoiceToggle:     {"TestVoiceModeSpeaksAndRelistens"},
		actVoiceMute:       {"TestVoiceModeSpeaksAndRelistens"},
		actDictate:         {"TestVoiceModeSpeaksAndRelistens"},
		actSpeakAnswer:     {"TestSpeechQueueSpeaksAllAndCloses"},
		actHome:            {"TestHeaderClickDispatches"},
		actSwitcher:        {"TestTUILeftRailFeatureJourney"},
		actNewSession:      {"TestHeaderClickDispatches"},
		actConfigPanel:     {"TestConfigClickOpensPanelNotBlindRun"},
		actRename:          {"TestHeaderClickDispatches"},
		actRailToggle:      {"TestRailToggleCommand"},
		actMouseToggle:     {"TestSidebarClickNavStatusAndRailRows"},
		actInputModeToggle: {"TestSidebarClickNavStatusAndRailRows"},
		actRailCollapse:    {"TestTUILeftRailFeatureJourney"},
		actRailWiden:       {"TestTUILeftRailFeatureJourney"},
		actRailNarrow:      {"TestTUILeftRailFeatureJourney"},
		actChangesToggle:   {"TestChangesToggleCommand"},
		actPanelWiden:      {"TestTUIRightPanelCycleKeyboardJourney"},
		actPanelNarrow:     {"TestTUIRightPanelCycleKeyboardJourney"},
		actRightTabNext:    {"TestTUIRightPanelCycleKeyboardJourney"},
		actTerminalTab:     {"TestTUITerminalPanelFeatureJourney"},
		actTasksTab:        {"TestTUITasksPanelFeatureJourney"},
		actObserveTab:      {"TestObservePanelShowsEvents"},
		actGoalPanel:       {"TestGoalPanelOpensFromAction"},
		actShellsTab:       {"TestTUIShellsPanelFeatureJourney"},
		actTray:            {"TestTrayKeyAltW"},
		actBackgroundTurn:  {"TestBackgroundTurnAction"},
	}
	if len(actionEvidence) != len(actionRegistry) {
		t.Fatalf("evidence covers %d TUI actions, code declares %d", len(actionEvidence), len(actionRegistry))
	}
	for id, action := range actionRegistry {
		if action.label == "" || action.run == nil {
			t.Fatalf("action %v lacks label/run metadata: %#v", id, action)
		}
		if len(actionEvidence[id]) == 0 {
			t.Fatalf("action %v (%s) has no parity journey evidence", id, action.label)
		}
	}

	rightTabEvidence := map[rightPanelTab][]string{
		rightTabChanges:  {"TestTUIToolTurnDrivesPlanChangesAndTaskPanels", "TestChangesPanelFeatureJourney"},
		rightTabGit:      {"TestTUIEveryRightPanelTabKeyboardJourney", "TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines"},
		rightTabTerminal: {"TestTUITerminalPanelFeatureJourney"},
		rightTabTasks:    {"TestTUITasksPanelFeatureJourney"},
		rightTabObserve:  {"TestObservePanelShowsEvents"},
		rightTabGoal:     {"TestGoalPanelOpensFromAction"},
		rightTabShells:   {"TestTUIShellsPanelFeatureJourney"},
		rightTabNotepad:  {"TestTUINotepadPanelFeatureJourney"},
	}
	allTabs := []rightPanelTab{rightTabChanges, rightTabGit, rightTabTerminal, rightTabTasks, rightTabObserve, rightTabGoal, rightTabShells, rightTabNotepad}
	if len(rightTabEvidence) != len(allTabs) {
		t.Fatalf("evidence covers %d right tabs, code declares %d", len(rightTabEvidence), len(allTabs))
	}
	seenLabels := map[string]bool{}
	for _, tab := range allTabs {
		if tab.label() == "" || tab.shortLabel() == "" {
			t.Fatalf("right tab %v lacks labels", tab)
		}
		if seenLabels[tab.label()] {
			t.Fatalf("duplicate right tab label %q", tab.label())
		}
		seenLabels[tab.label()] = true
		if len(rightTabEvidence[tab]) == 0 {
			t.Fatalf("right tab %s has no journey evidence", tab.label())
		}
	}
}
