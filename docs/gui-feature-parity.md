# GUI feature parity matrix

This matrix tracks the product surfaces that must stay covered while Eigen moves toward a premium desktop app. It is intentionally test-linked: a surface is not considered covered unless it names automated evidence.

## App shell pages

| Surface | User journey | Automated evidence |
| --- | --- | --- |
| home | Review proactive feed, recent sessions, and start/resume work from the shell. | `internal/app:TestAppHomePageResumeFeatureJourney`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppPremiumShellVisualContract`, `internal/app:TestAppKeyboardE2EOpenFeedTaskCancelsWork` |
| live | Inspect live daemon sessions and attach via selected live row/rail. | `internal/app:TestAppLivePageFeatureJourney`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestAppSelectionUsesSelectionRoleNotAccent`, `internal/app:TestViewFitsAllPagesAcrossSizes` |
| projects | Browse project groups and open work rooted in a project. | `internal/app:TestAppProjectsPageFeatureJourney`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| machines | Browse remote machines and drill into remote sessions/install state. | `internal/app:TestAppMachinesPageFeatureJourney`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| sessions | Browse/resume/delete/filter session history. | `internal/app:TestAppSessionsPageFeatureJourney`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| config | View and edit app/agent configuration fields. | `internal/app:TestAppConfigMemorySkillsFeatureJourneys`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| skills | Browse installed skills and add skill sources. | `internal/app:TestAppConfigMemorySkillsFeatureJourneys`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| models | Inspect and navigate model catalog/availability. | `internal/app:TestAppProvidersModelsCronsFeatureJourneys`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `.:TestPTYSmokeAppShellKeyboardNavigation`, `internal/app:TestAppKeyboardE2ENavigatePaletteAndOpen` |
| providers | Inspect provider credential/default-model status. | `internal/app:TestAppProvidersModelsCronsFeatureJourneys`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| memory | Inspect/consolidate project/global memory. | `internal/app:TestAppConfigMemorySkillsFeatureJourneys`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| crons | Inspect scheduled/background automation. | `internal/app:TestAppProvidersModelsCronsFeatureJourneys`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| plugins | Browse marketplaces, installed plugins, toggle installed plugin state, and inspect extension wiring. | `internal/app:TestAppPluginsPageFeatureJourney`, `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestAppPaletteVisualContract`, `internal/app:TestAppKeyboardE2ENavigatePaletteAndOpen` |

## Native/browser desktop GUI shell

| Surface | User journey | Automated evidence |
| --- | --- | --- |
| local-only server | Launch browser/debug GUI without exposing the daemon-backed API off-machine. | `internal/gui:TestServeRejectsNonLocalBind`, `scripts/gui-smoke.sh` |
| static premium shell | Serve the desktop HTML/JS/CSS bundle containing new-session/profile/system modals, timeline, model/effort controls, tool cards, approval cards, shell summaries, and diff rendering. | `internal/gui:TestHandlerStaticAndAPIContracts`, `scripts/gui-smoke.sh`, `node --check internal/gui/static/app.js` |
| service validation | Reject empty session IDs and empty input before reaching the daemon. | `internal/gui:TestServiceValidationErrors` |
| event streaming adapter | Stop stream writers cleanly on context cancel or closed event channels. | `internal/gui:TestStreamJSONLinesStopsOnContextOrClosedEvents` |

## Chat TUI panels and flows

| Surface | User journey | Automated evidence |
| --- | --- | --- |
| transcript | Read streamed assistant/tool/user history with wrapped content. | `internal/tui:TestTUIComposerTranscriptJourney`, `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines` |
| composer | Type prompts, slash commands, mentions, and multiline input. | `internal/tui:TestTUIComposerTranscriptJourney`, `internal/tui:TestTUISlashCommandJourneyFromComposer`, `internal/tui:TestMentionFileIndexCachesBetweenKeystrokes`, `internal/tui:TestSlashMenuOpensAndFilters` |
| plan | Track todo/plan state above the transcript. | `internal/tui:TestTUIPlanPanelFeatureJourney`, `internal/tui:TestTUIKeyboardE2EPalettePanelAndNotepad` |
| left rail | Navigate sessions/tasks/context without losing orientation. | `internal/tui:TestTUILeftRailFeatureJourney`, `internal/tui:TestTUIKeyboardE2EHomeAndBackgroundActions` |
| changes tab | Review changed files and edit blocks from the current turn. | `internal/tui:TestTUIChangesPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestTUIRightPanelCycleKeyboardJourney`, `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines` |
| git tab | Inspect repository summary without render-time git subprocess storms. | `internal/tui:TestTUIGitPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestTUIRightPanelCycleKeyboardJourney`, `internal/tui:TestGitLinesUsesCachedSummary`, `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines` |
| terminal tab | Use embedded PTY terminal and release it cleanly. | `internal/tui:TestTUITerminalPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestTUIRightPanelCycleKeyboardJourney`, `internal/tui:TestTUITerminalPanelFeatureJourney` |
| notepad tab | Keep per-session notes and persist them on tab switch. | `internal/tui:TestTUINotepadPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestNotepadTabSwitchFlushesDirtyNotes`, `internal/tui:TestTUIKeyboardE2EPalettePanelAndNotepad` |
| tasks tab | Inspect delegated/background task state, expand results, and confirm cancel markers. | `internal/tui:TestTUITasksPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines` |
| shells tab | Inspect background shell state, expand shell output, and kill running shells. | `internal/tui:TestTUIShellsPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines` |
| command palette | Navigate actions/pages from keyboard. | `internal/tui:TestTUIKeyboardE2EHomeAndBackgroundActions` |
| app return | Return from chat to app shell. | `internal/tui:TestTUIKeyboardE2EHomeAndBackgroundActions` |

## Coverage standard

- Every app page in `internal/app.pages` must appear in this document.
- Every major TUI panel/flow above must keep at least one automated test reference.
- The broad verification command remains:

```bash
go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1
```
