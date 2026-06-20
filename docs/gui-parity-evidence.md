# GUI parity evidence

Eigen's premium desktop surface is delivered in phases across the native/browser desktop GUI (`internal/gui`), app shell (`internal/app`), and chat TUI (`internal/tui`). This file is a living evidence map: every row names the product contract and the automated test that proves it today.

## Current automated evidence

| Area | Product contract | Evidence |
| --- | --- | --- |
| Native/browser desktop GUI shell | Daemon-backed GUI preview stays local-only, serves static premium desktop assets, exposes health/session/profile APIs, validates service inputs before daemon calls, and stops event streaming cleanly. | `internal/gui:TestServeRejectsNonLocalBind`, `internal/gui:TestHandlerStaticAndAPIContracts`, `internal/gui:TestServiceValidationErrors`, `internal/gui:TestStreamJSONLinesStopsOnContextOrClosedEvents`, `scripts/gui-smoke.sh` |
| App-side mutating pages | Config, skills, memory, plugins, marketplace, and provider pages have explicit persistence/confirmation/busy/rollback workflow evidence. | `docs/gui-mutating-pages-evidence.md`, `internal/app:TestConfigMultiSelectRouteProviders`, `internal/app:TestSkillsInstallRunsInBackgroundWithBusyMarker`, `internal/app:TestMemoryDeleteWithConfirm`, `internal/app:TestPluginsPageCanNavigateCatalogAndInstall`, `internal/app:TestPluginsPageSurfacesScanResultsAndRollback`, `internal/app:TestProvidersAddCustomProviderUpdatesCatalog` |
| Accessibility/keyboard parity | Current app shell, chat TUI, and native/browser GUI seams have keyboard paths and accessibility contracts mapped to automated evidence; TUI click paths share the validated action registry. | `docs/gui-accessibility-keyboard-audit.md`, `docs:TestGUIAccessibilityKeyboardAuditMapsParityEvidence`, `internal/tui:actionRegistry` |
| App shell visual premium | Wide app renders the premium sidebar/content shell, not classic header buttons; every page keeps shell/page/help golden tokens; key pages and home activity sections are visible, including richer live/sessions/plugins snapshot tokens. | `internal/app:TestAppPremiumShellVisualContract`, `internal/app:TestAppHomeGoldenSnapshotTokens`, `internal/app:TestAppEveryPageGoldenSnapshotTokens`, `internal/app:TestAppPaletteGoldenSnapshotTokens`, `internal/app:TestAppLiveSessionsPluginsGoldenSnapshotTokens` |
| App command palette | App command palette filters/renders as a Base-painted overlay and can launch pages. | `internal/app:TestAppPaletteVisualContract`, `internal/app:TestAppKeyboardE2ENavigatePaletteAndOpen` |
| App canvas ownership | App owns the full terminal rectangle; rows are Base-painted and exact terminal size; app render soak uses GC+settled goroutine polling for bounded-growth checks. | `internal/app:TestAppViewPaintsFullCanvas`, `internal/app:TestAppRenderSoakPaintsAndDoesNotLeakGoroutines` |
| App responsive fit | Every app page fits narrow/normal/wide breakpoints without width/height overflow. | `internal/app:TestViewFitsAllPagesAcrossSizes`, `internal/app:TestViewLineWidthsWithinTerminal` |
| App page journeys | Every app page is reachable by keyboard through the command palette and quick-jump keys while retaining premium shell identity; key app pages have feature-specific journeys for open/resume/attach/edit/drill-in/catalog/schedule flows. | `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageQuickJumpJourney`, `internal/app:TestAppHomePageResumeFeatureJourney`, `internal/app:TestAppLivePageFeatureJourney`, `internal/app:TestAppSessionsPageFeatureJourney`, `internal/app:TestAppProjectsPageFeatureJourney`, `internal/app:TestAppMachinesPageFeatureJourney`, `internal/app:TestAppConfigMemorySkillsFeatureJourneys`, `internal/app:TestAppProvidersModelsCronsFeatureJourneys`, `internal/app:TestAppPluginsPageFeatureJourney` |
| App resource lifetime | Every app exit/open/attach/remote path routes through cancellation so app background work stops. | `internal/app:TestAppQuitCancelsBackgroundWork`, `internal/app:TestAppKeyboardE2EOpenFeedTaskCancelsWork`, `internal/app:TestPaletteQuitCancelsBackgroundWork`, `internal/app:TestPaletteNewSessionCancelsBackgroundWork` |
| App selection semantics | Live-session selection uses the unified selection bar, not brand accent. | `internal/app:TestAppSelectionUsesSelectionRoleNotAccent` |
| Feed/model suggestion resources | Feed scans, GitHub feed calls, and model suggestions inherit cancellation and avoid cache writes after cancellation. | `internal/feed:TestScanGitHubCanceledSkipsCommands`, `internal/feed:TestScanSuggestCanceledSkipsModel` |
| TUI notepad data safety | Dirty notes flush when leaving the notepad tab and before quitting. | `internal/tui:TestNotepadTabSwitchFlushesDirtyNotes`, `internal/tui:TestNotepadQuitFlushesDirtyNotes`, `internal/tui:TestTUIKeyboardE2EPalettePanelAndNotepad` |
| TUI git panel resources | Rendering the git tab never spawns git subprocesses; git refresh is Update-owned on tab/window/turn events and then reused from cache. | `internal/tui:TestGitLinesUsesCachedSummary`, `internal/tui:TestGitSummaryRefreshesOnUpdateEventsOnly`, `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines` |
| TUI file completion resources | `@file` completion reuses cached indexes while typing and is time/file bounded. | `internal/tui:TestMentionFileIndexCachesBetweenKeystrokes`, `internal/tui:TestIndexFilesIsTimeBudgeted`, `internal/tui:TestMentionCompletionSoakReusesIndex` |
| TUI render resources | Hot render/live loops do not spawn work and use GC+settled goroutine polling for bounded-growth checks. | `internal/tui:TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines`, `internal/tui:TestTUILiveLoopResourceMeasurement` |
| TUI rail/plan/right-panel journeys | Plan todo state renders from todo tool events; left rail lists/toggles/hops sessions; changes/git/terminal/notepad/tasks/shells have feature journeys; every right-panel tab has stable golden tokens, with richer premium task/shell/notepad surface snapshots. | `internal/tui:TestTUIPlanPanelFeatureJourney`, `internal/tui:TestTUILeftRailFeatureJourney`, `internal/tui:TestTUIChangesPanelFeatureJourney`, `internal/tui:TestTUIGitPanelFeatureJourney`, `internal/tui:TestTUITerminalPanelFeatureJourney`, `internal/tui:TestTUINotepadPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestTUIRightPanelCycleKeyboardJourney`, `internal/tui:TestTUITasksPanelFeatureJourney`, `internal/tui:TestTUIShellsPanelFeatureJourney`, `internal/tui:TestTUIEveryRightPanelTabGoldenSnapshotTokens`, `internal/tui:TestTUIRightPanelGitGoldenSnapshotTokens`, `internal/tui:TestTUIRightPanelPremiumSurfaceGoldenSnapshotTokens` |
| Agent shell command journey | Agent loop executes a real background bash subprocess, polls it with `bash_output`, stops it with `kill_shell`, verifies no running shell remains, verifies file descriptors settle back to baseline (+2 tolerance when `/proc/self/fd` is available), and verifies the UI-facing event/request stream carries bash/bash_output/kill_shell results. | `internal/agent:TestAgentBackgroundShellToolJourneySettlesResources` |
| Transcript/concurrency durability | Transcript save uses isolated temp files and tolerates raced temp rename when a destination transcript already exists; a concurrent-writer regression stress verifies one complete final transcript and no leaked temp files. | `internal/transcript:Save`, `internal/transcript:TestSaveConcurrentWritersLeaveCompleteTranscriptAndNoTemps`, `internal/daemon:TestPersistAcrossDaemonRestart` |
| TUI keyboard parity | Keyboard palette/background/home flows work without mouse. | `internal/tui:TestTUIKeyboardE2EHomeAndBackgroundActions` |
| TUI composer/transcript journeys | Empty composer, mention menu, slash-command completion, submission, streamed reasoning/text, transcript rendering, and golden tokens work as keyboard journeys. | `internal/tui:TestTUIEmptyComposerGoldenSnapshotTokens`, `internal/tui:TestTUIComposerMentionMenuGoldenSnapshotTokens`, `internal/tui:TestTUIComposerTranscriptJourney`, `internal/tui:TestTUISlashCommandJourneyFromComposer`, `internal/tui:TestTUIComposerTranscriptGoldenSnapshotTokens` |
| Premium TUI interaction/view soak | Deterministic model-level journey drives composer typing, mention completion, submitted turn rendering, right-panel tabs, notepad input, tasks empty state, changes empty state, and repeated composer edits while asserting rendered view dimensions. | `internal/tui:TestPremiumInteractionViewSoak` |
| Chat TUI tool-turn journey | Submitted chat turn receives reasoning, todo, edit, tool-result, text, and done events; the plan panel updates from todo args, the changes panel indexes the edited file/diff, and right-panel tasks/changes surfaces remain fitted. | `internal/tui:TestTUIToolTurnDrivesPlanChangesAndTaskPanels` |
| CLI binary smoke | Real CLI entrypoints for version and design-system swatch execute in a subprocess with isolated HOME. | `.:TestCLISmokeVersionAndTheme` |
| PTY smoke | Binary starts under a real pseudo-terminal, answers terminal capability probes, and completes a command. | `.:TestPTYSmokeVersionCommand` |
| Test-only smoke isolation | PTY smoke entrypoints are compiled only into the smoke-tagged helper; normal/release binaries fail those commands explicitly instead of silently succeeding or launching an agent, and smoke-tagged binaries compile. | `.:TestProductionSmokeCommandsFailExplicitly`, `.:TestReleaseBinaryDoesNotExposeSmokeCommands`, `.:TestSmokeTaggedBinaryBuilds`, `.:TestPTYChatTUISmokeQuit` |
| Independent review blockers | Final review blockers are test-guarded: release smoke false-greens fail explicitly, notepad dirty notes flush on quit, feed/GitHub cancellation is context-backed, and git/feed subprocess counters are atomic. | `.:TestReleaseBinaryDoesNotExposeSmokeCommands`, `internal/tui:TestNotepadQuitFlushesDirtyNotes`, `internal/feed:TestScanGitHubCanceledSkipsCommands`, `internal/tui:TestGitLinesUsesCachedSummary` |
| Interactive app PTY smoke | Real app shell Bubble Tea program starts under a pseudo-terminal, navigates via palette to Models, quick-jumps to Plugins and Sessions, and exits cleanly via `q`. | `.:TestPTYSmokeAppShellKeyboardNavigation` |
| App PTY navigation soak | Real app shell loops through every page plus repeated palette/quick-jump navigation under a pseudo-terminal, including a release-binary longer soak with bounded goroutine checks. | `.:TestPTYAppShellNavigationSoak`, `.:TestPTYReleaseAppShellLongerSoak` |
| Chat TUI PTY smoke | Real chat TUI starts under a pseudo-terminal with a fake local provider, renders the premium shell, exits via keyboard interrupt, and checks goroutine bounds. | `.:TestPTYChatTUISmokeQuit` |
| Slash menu determinism | Built-in slash menu test is isolated from user/project custom commands. | `internal/tui:TestSlashMenuOpensAndFilters` |
| CI enforcement | GitHub Actions runs the full GUI phase gate on pull requests and pushes to `main` under Xvfb with xterm/tmux installed; PR #3 has a green `GUI phase gate` run `27859059893` at head SHA `42c8a08f8b4752495f42e6a5aafc6aa0ae8c4077`. | `.github/workflows/gui-phase.yml`, `scripts/verify-gui-phase.sh`, `docs:TestGUIPhaseWorkflowRunsVerificationScript`, `https://github.com/avifenesh/eigen/actions/runs/27859059893` |

## Verification commands

Run the whole phase gate with:

```bash
scripts/verify-gui-phase.sh
```

Equivalent expanded commands:

```bash
go test ./... -count=1
go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1
node --check internal/gui/static/app.js
scripts/gui-smoke.sh
go test -tags smoke . -count=1
go test ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
```

## Phase gate and remaining gaps

See `docs/gui-current-surface-acceptance.md` for the full GUI parity acceptance map and CI evidence. See `docs/gui-phase-gate.md` for the reproducible full parity gate. See `docs/gui-delivery-notes.md` for milestone scope. See `docs/gui-next-phase-backlog.md` for the completed backlog and delivery gates.

## Completed in this phase

- Real terminal/desktop harness evidence: release app shell and chat TUI were exercised in an isolated X11 desktop terminal, with screenshots documented in `docs/gui-screenshot-artifacts.md`.
- Feature parity matrix: current native/browser GUI shell seams, every app page, and major TUI panel/flow are mapped to automated journey evidence in `docs/gui-feature-parity.md`.
- Longer release-binary soak: `TestPTYReleaseAppShellLongerSoak` runs a release binary under PTY, repeats app navigation, exits cleanly, and checks bounded goroutine growth.
- Visual snapshots/goldens: app pages, richer live/sessions/plugins surfaces, TUI right-panel tabs, premium task/shell/notepad surfaces, central TUI states, and desktop screenshots have stable token/artifact evidence.
- CI enforcement: `.github/workflows/gui-phase.yml` runs `scripts/verify-gui-phase.sh` under Xvfb.

## Remaining gaps before claiming full persistent goal

- Optional richer recordings/pixel-review package if final acceptance requires more than the current screenshot artifacts.
- Broader product expansion beyond this GUI phase as new desktop surfaces/features are added.
