package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGUIMutatingPagesEvidenceMapsWorkflows(t *testing.T) {
	b, err := os.ReadFile("gui-mutating-pages-evidence.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"config",
		"skills",
		"memory",
		"plugins",
		"providers",
		"TestConfigEditFreeText",
		"TestConfigMultiSelectRouteProviders",
		"TestSkillsInstallRunsInBackgroundWithBusyMarker",
		"TestMemoryDeleteWithConfirm",
		"TestMemoryEnterOpensScrollableNote",
		"TestPluginsPageCanNavigateCatalogAndInstall",
		"TestPluginsPageCanMarkCatalogPluginsAndInstallBatch",
		"TestPluginsPageToggleInstalledPlugin",
		"TestPluginsPageSurfacesScanResultsAndRollback",
		"TestProvidersAddCustomProviderUpdatesCatalog",
		"go test ./internal/app -run",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("mutating pages evidence missing %q", want)
		}
	}
}
