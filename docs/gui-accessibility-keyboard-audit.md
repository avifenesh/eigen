# GUI keyboard parity evidence

This is a keyboard-parity evidence map, keyboard-parity evidence, not an external certification document. It records keyboard parity for the premium app shell, native/browser GUI shell, and chat TUI. It is scoped to the shipped Eigen GUI surfaces and automated evidence.

## Principles

- Every clickable TUI chrome action dispatches through the same validated action registry as keys/palette (`internal/tui/action.go`), so mouse clicks cannot bypass disabled-state guards.
- Keyboard affordances are visible in app/TUI footers or slash/help output.
- Mutating actions that can race an agent turn are gated by `idleOnly`/backend predicates and show a disabled hint instead of silently doing nothing.
- Native/browser GUI controls expose form labels/semantic buttons in static markup; the local server refuses non-local binds.

## Chat TUI keyboard parity

| Surface/action | Keyboard path | Mouse/click path | Evidence |
| --- | --- | --- | --- |
| model picker | `alt+m`, `ctrl+o`, `/model`, palette | status/header/sidebar action | `internal/tui:TestClickStatusSegmentDispatches`, `internal/tui:TestHeaderClickDispatches`, `internal/tui:TestSidebarClickNavStatusAndRailRows` |
| permission picker | `alt+a`, `ctrl+a`, `/perm` | status/sidebar action opens explicit picker | `internal/tui:TestPermClickOpensConfirmNotBlindToggle`, `internal/tui:TestPermClickOpensConfirmNotBlindToggle`, `internal/tui:TestSidebarClickNavStatusAndRailRows` |
| effort/search/fast/route | `alt+r`, `ctrl+e`, slash commands | status/sidebar action | `internal/tui:TestClickStatusSegmentDispatches`, `internal/tui:TestClickStatusSegmentDispatches` |
| session rail | `ctrl+b`, `alt+b`, `/rail`, switcher | header/sidebar/rail rows | `internal/tui:TestRailToggleCommand`, `internal/tui:TestTUILeftRailFeatureJourney`, `internal/tui:TestSidebarClickNavStatusAndRailRows` |
| right panel/tabs | `ctrl+g`, `alt+g`, `ctrl+r`, `/changes`, `/tasks`, `/shells`, `/term`, `/observe` | header/sidebar/right-tab clicks | `internal/tui:TestChangesToggleCommand`, `internal/tui:TestTUIEveryRightPanelTabKeyboardJourney`, `internal/tui:TestTUIRightPanelCycleKeyboardJourney`, `internal/tui:TestRightPanelTabClickSwitches` |
| notepad | keyboard text entry, `ctrl+g` release | tab click/focus | `internal/tui:TestTUINotepadPanelFeatureJourney`, `internal/tui:TestTUIRightPanelPremiumSurfaceGoldenSnapshotTokens` |
| terminal panel | keyboard passthrough with encoded keys, `ctrl+g` release | terminal tab/action | `internal/tui:TestTUITerminalPanelFeatureJourney`, `internal/tui:TestTUITerminalPanelFeatureJourney` |
| tasks/shells | arrows/click rows, cancel/kill confirmation | task/shell row clicks | `internal/tui:TestTUITasksPanelFeatureJourney`, `internal/tui:TestTUIShellsPanelFeatureJourney` |
| tray/approvals | `alt+w`/`alt+n`, arrows, enter, escape | tray status/action | `internal/tui:TestTrayKeyAltW`, `internal/tui:TestApprovalFlow` |
| command palette | `ctrl+k`, slash commands, enter | header/sidebar actions reuse same registry | `internal/tui:TestPaletteEnterRunsAction`, `internal/tui:TestTUISlashCommandJourneyFromComposer` |
| copy/read/voice controls | `/copy`, `/read`, `/voice`, `/mute`, `/dictate`, `/talk`, `/speak`, footer controls | footer/status actions | `internal/tui:TestVoiceModeSpeaksAndRelistens`, `internal/tui:TestSpeechQueueSpeaksAllAndCloses` |
| tool turn output | submitted turn updates transcript/plan/changes/tasks surfaces without layout overflow | changes rows are clickable to jump to tool blocks | `internal/tui:TestTUIToolTurnDrivesPlanChangesAndTaskPanels`, `internal/tui:TestChangesDiffRowClickJumpsToFile` |

## App shell keyboard parity

| Surface/action | Keyboard path | Mouse/click path | Evidence |
| --- | --- | --- | --- |
| page navigation | `tab`, `j/k`, arrows, `enter` | sidebar/page rows where supported | `internal/app:TestAppEveryPageKeyboardJourney`, `internal/app:TestAppEveryPageGoldenSnapshotTokens` |
| command palette | `:`, filter typing, `enter`, `esc` | n/a | `internal/app:TestAppPaletteGoldenSnapshotTokens` |
| new/session/app return | `n`, `enter`, `q` | n/a | `internal/app:TestAppHomePageResumeFeatureJourney`, `internal/app:TestAppQuit` |
| config mutating fields | text, enum/dropdown, multi-select, save/cancel keys | n/a | `docs/gui-mutating-pages-evidence.md`, `internal/app:TestConfigMultiSelectRouteProviders` |
| skills/memory/plugins/provider mutating pages | prompt/confirmation/busy/rollback keys | n/a | `docs/gui-mutating-pages-evidence.md` |

## Native/browser GUI accessibility seams

| Surface/action | Accessibility contract | Evidence |
| --- | --- | --- |
| local server | refuses non-local bind and serves only on loopback | `internal/gui:TestServeRejectsNonLocalBind` |
| static shell | new-session/profile/system modals, timeline, model controls, approval/tool cards, shell summaries, and diff views exist in served markup/assets | `internal/gui:TestHandlerStaticAndAPIContracts`, `scripts/gui-smoke.sh` |
| API form validation | session ID and input text validated before daemon calls | `internal/gui:TestServiceValidationErrors` |
| stream shutdown | event stream exits cleanly on context cancel/closed channel | `internal/gui:TestStreamJSONLinesStopsOnContextOrClosedEvents` |

