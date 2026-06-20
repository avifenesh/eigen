package app

import "testing"

func TestAppCodeDerivedFeatureInventoryHasJourneyEvidence(t *testing.T) {
	evidence := map[Page][]string{
		PageHome:      {"TestAppHomePageResumeFeatureJourney", "TestAppHomeGoldenSnapshotTokens"},
		PageLive:      {"TestAppLiveSessionsPluginsGoldenSnapshotTokens", "TestAppEveryPageKeyboardJourney"},
		PageProjects:  {"TestAppProjectsPageFeatureJourney", "TestAppEveryPageKeyboardJourney"},
		PageMachines:  {"TestAppMachinesPageFeatureJourney", "TestMachinesInstallMessage"},
		PageSessions:  {"TestAppSessionsPageFeatureJourney", "TestAppLiveSessionsPluginsGoldenSnapshotTokens"},
		PageConfig:    {"TestConfigEditFreeText", "TestConfigMultiSelectRouteProviders"},
		PageSkills:    {"TestSkillsInstallRunsInBackgroundWithBusyMarker", "TestInstallPromptCapturesInput"},
		PageModels:    {"TestModelsAndProvidersPagesShowRoutingContext", "TestAppEveryPageKeyboardJourney"},
		PageProviders: {"TestProvidersAddCustomProviderUpdatesCatalog", "TestModelsAndProvidersPagesShowRoutingContext"},
		PageObserve:   {"TestAppEveryPageKeyboardJourney", "TestAppEveryPageGoldenSnapshotTokens"},
		PageMemory:    {"TestMemoryDeleteWithConfirm", "TestMemoryEnterOpensScrollableNote"},
		PageCrons:     {"TestAppEveryPageKeyboardJourney", "TestAppEveryPageGoldenSnapshotTokens"},
		PagePlugins:   {"TestPluginsPageCanNavigateCatalogAndInstall", "TestPluginsPageSurfacesScanResultsAndRollback"},
		PageProfile:   {"TestAppEveryPageKeyboardJourney", "TestAppEveryPageGoldenSnapshotTokens"},
	}
	if len(evidence) != len(pages) {
		t.Fatalf("evidence covers %d app pages, code declares %d", len(evidence), len(pages))
	}
	seen := map[Page]bool{}
	for _, p := range pages {
		if p.name == "" || p.key == "" || p.purpose == "" || p.action == "" {
			t.Fatalf("page %#v lacks premium navigation/product copy", p)
		}
		if seen[p.page] {
			t.Fatalf("duplicate page enum in code-derived inventory: %v", p.page)
		}
		seen[p.page] = true
		if len(evidence[p.page]) == 0 {
			t.Fatalf("page %s (%v) has no journey evidence", p.name, p.page)
		}
	}
}
