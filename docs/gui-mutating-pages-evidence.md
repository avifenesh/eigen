# GUI mutating-page evidence

This file makes the app-side P1 mutating workflows explicit. These tests already exercise real persistence, confirmation, busy-state, and rollback paths; this evidence map prevents them from being treated as incidental coverage.

| Surface | Mutating workflow | Automated evidence |
| --- | --- | --- |
| config | Free-text edit saves to config; enum fields cycle/pick; route provider multi-select persists multiple providers; edit/dropdown modes capture jump/quit keys. | `internal/app:TestConfigEditFreeText`, `internal/app:TestConfigCycleEnum`, `internal/app:TestConfigDropdownPicksValue`, `internal/app:TestConfigDropdownEscCancels`, `internal/app:TestConfigMultiSelectRouteProviders`, `internal/app:TestConfigEditingCapturesJumpKeys`, `internal/app:TestConfigPickingCapturesKeys` |
| skills | Install prompt captures/cancels input; parser handles force/overwrite/name/no-scan flags; skill install runs as a background command, shows a busy marker, completes, clears busy state, and rescans installed skills. | `internal/app:TestInstallPromptCapturesInput`, `internal/app:TestInstallPromptEscCancels`, `internal/app:TestInstallPromptEmptyEnterCancels`, `internal/app:TestParseSkillInstallInputFlags`, `internal/app:TestSkillsInstallRunsInBackgroundWithBusyMarker` |
| memory | Delete requires confirmation, snapshots backups, deletes only the selected bullet, cancel preserves content, consolidation refuses to start without a small model, and enter opens a scrollable detail reader. | `internal/app:TestMemoryDeleteWithConfirm`, `internal/app:TestMemoryDeleteCancel`, `internal/app:TestMemoryConsolidateNeedsSmall`, `internal/app:TestMemoryEnterOpensScrollableNote`, `internal/app:TestMemoryBullets` |
| plugins | Marketplace/install UX renders product state, refreshes catalog, previews manifests, installs focused and marked catalog plugins, hides installed plugins, updates installed plugins, toggles installed plugin enabled state, requires uninstall confirmation, persists marketplace/hook disable toggles, rolls back failed scan installs, records forced scan warnings, and exposes plugin agent roles through palette navigation. | `internal/app:TestPluginsPageRendersProductSurface`, `internal/app:TestPluginsPageCanNavigateCatalogAndInstall`, `internal/app:TestPluginsPageCanMarkCatalogPluginsAndInstallBatch`, `internal/app:TestMarketplaceCatalogHidesInstalledPlugins`, `internal/app:TestMarketplaceShiftUUpdatesInstalledAndShowsNewPlugins`, `internal/app:TestPluginsPageToggleInstalledPlugin`, `internal/app:TestPluginsPageUninstallRequiresConfirmation`, `internal/app:TestPluginsPageMouseTabsAndHookRows`, `internal/app:TestPluginsPageSurfacesScanResultsAndRollback`, `internal/app:TestAppPaletteSurfacesPluginAgentRoles` |
| providers | Custom provider add flow updates the provider catalog and route context. | `internal/app:TestProvidersAddCustomProviderUpdatesCatalog`, `internal/app:TestModelsAndProvidersPagesShowRoutingContext` |

## Gate

```bash
go test ./internal/app -run 'Test(Config|Memory|Skills|InstallPrompt|ParseSkill|Plugins|Marketplace|Providers|ModelsAndProviders)' -count=1
go test ./docs -count=1
```
