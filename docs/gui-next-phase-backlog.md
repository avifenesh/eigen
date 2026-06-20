# GUI next-phase backlog

This record lists the completed GUI parity work and delivery gates. The full GUI parity milestone for shipped Eigen surfaces is accepted in `docs/gui-current-surface-acceptance.md` and verified by `scripts/verify-gui-phase.sh`; no shipped GUI feature is recorded as missing.

## P0 — full desktop-app parity

1. **Real desktop sandbox journey** — complete
   - Started with an isolated X11 workspace terminal running the smoke-tagged Eigen app shell.
   - Captured exact terminal grid showing the premium app shell/sidebar/home feed inside the desktop workspace.
   - Built and ran a release binary (`go build -buildvcs=false -o /tmp/eigen-release .`) in the isolated desktop terminal with a temp HOME.
   - Captured exact terminal grid showing the release binary premium app shell/sidebar/home page inside the desktop workspace.
   - Sent quit key and confirmed the release app shell could close from the workspace terminal.
   - Ran the smoke-tagged chat TUI in the isolated desktop terminal and captured the premium chat shell grid (left navigation, central composer/transcript, right panel, footer voice controls).
   - Sent `ctrl+c` through the workspace terminal and confirmed the chat smoke process exited cleanly.
   - Added automated release-binary PTY soak (`TestPTYReleaseAppShellLongerSoak`) that builds a release binary, loops through app pages/palette navigation multiple times, exits cleanly, and checks bounded goroutine growth.
   - Captured durable isolated workspace screenshot artifacts and recorded visual assertions in `docs/gui-screenshot-artifacts.md`.
   - Release app shell artifact: `docs/artifacts/gui/release-app-shell.png` (source capture path: `/run/user/1000/agent-workspace-linux/default/artifacts/gui/release-app-shell.png`).
   - Chat TUI shell artifact: `docs/artifacts/gui/chat-tui-shell.png` (source capture path: `/run/user/1000/agent-workspace-linux/default/artifacts/gui/chat-tui-shell.png`).
   - Complete for current acceptance: PNG screenshot artifacts plus ANSI/token goldens are the required visual evidence.

2. **Longer live binary/resource soak** — complete
   - Release-binary PTY soak exercises app page navigation, palette navigation, and exit.
   - Chat composer/right-panel coverage is exercised by deterministic TUI journeys and PTY chat smoke; background shell subprocess lifecycle is exercised by the agent shell journey.
   - Completed deterministic premium TUI interaction/view soak (`TestPremiumInteractionViewSoak`) for chat composer, mention completion, submitted turn rendering, right-panel tabs, notepad, tasks empty state, changes empty state, and repeated composer edits; this intentionally avoids claiming runtime leak coverage because it does not execute Bubble Tea commands.
   - Existing runtime/resource coverage remains in PTY/resource tests (`TestPTYReleaseAppShellLongerSoak`, chat PTY smoke, render/live-loop resource tests).
   - Added command-executing agent shell journey (`TestAgentBackgroundShellToolJourneySettlesResources`) for a real background bash subprocess, bash_output polling, kill_shell cleanup, zero remaining running shells, file-descriptor baseline/delta settling when `/proc/self/fd` is available, and UI-facing event/request propagation.

3. **Richer visual review artifacts** — complete
   - Expanded stable token goldens for representative desktop widths.
   - Added app live/sessions/plugins marketplace+wiring snapshot tokens (`TestAppLiveSessionsPluginsGoldenSnapshotTokens`) beyond the existing home/palette/every-page goldens.
   - Added TUI task/shell/notepad premium surface snapshot tokens (`TestTUIRightPanelPremiumSurfaceGoldenSnapshotTokens`) beyond the existing composer/transcript/git/right-tab goldens.
   - Complete for current acceptance: richer app/TUI goldens plus PNG screenshots are the required visual evidence.

## P1 — feature-depth and UX polish

4. **Per-feature E2E workflows for app-side mutating pages** — evidence mapped
   - Config free-text, enum, dropdown, and route-provider multi-select save flows are covered.
   - Skills install prompt parsing/cancel/success/busy UI is covered.
   - Memory delete/consolidate/detail-reader flows with confirmation/error states are covered.
   - Plugin marketplace install/remove/disable/update/batch/rollback flows with realistic catalog data are covered.
   - Provider custom-catalog add and routing context are covered.
   - Evidence map: `docs/gui-mutating-pages-evidence.md`.

5. **Chat TUI end-to-end agent turn with tools** — model-level evidence added
   - Deterministic TUI journey (`TestTUIToolTurnDrivesPlanChangesAndTaskPanels`) submits a chat turn, receives reasoning/todo/edit/tool-result/text/done events, updates transcript, plan panel, changes panel, tasks tab, and verifies fitted right-panel rendering.
   - Render-path subprocess/goroutine protection is covered by `TestPremiumInteractionViewSoak`, `TestTUILiveLoopResourceMeasurement`, and `TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines`.
   - Complete for current acceptance: model-level TUI tool-turn evidence plus PTY binary smoke cover the shipped flow.

6. **Accessibility/keyboard audit** — evidence mapped
   - Every current clickable TUI chrome action dispatches through the shared validated action registry and has a documented keyboard path.
   - App shell, chat TUI, and native/browser GUI seams are mapped to automated keyboard/accessibility evidence.
   - Evidence map: `docs/gui-accessibility-keyboard-audit.md`.
   - Complete for current acceptance: keyboard parity and native/browser GUI accessibility seams are mapped to automated evidence.

## P2 — release readiness

7. **Clean-tree delivery gate** — complete for current milestone
   - `scripts/verify-gui-phase.sh` passed from a clean detached `origin/main` checkout at `ce860ca339ad6d50d7945ad0b8c37bef22113a93`.
   - `go test ./... -count=1` passed from the same clean detached checkout.
   - Main CI and GUI phase gate are green at the same SHA (`27862913354`, `27862913334`).

8. **Independent final review** — complete for current milestone
   - Independent review identified concrete blockers: doc contradiction, acceptance map, CI SHA verification, daemon assertion specificity, and accessibility overclaim scope.
   - Fixes are recorded in `docs/gui-current-surface-acceptance.md`, `docs/gui-accessibility-keyboard-audit.md`, and `internal/daemon:TestDaemonSecondListenFails`.

## Definition of done for the full GUI parity milestone

The milestone can be submitted to `goal_achieved` for shipped Eigen GUI surfaces when:

- P0 full parity items are complete and evidenced;
- all phase gate commands pass;
- release/clean-tree gates pass;
- independent final review has no unresolved blockers;
- evidence is concrete enough for a judge to verify without relying on intent.

That condition is recorded in `docs/gui-current-surface-acceptance.md`.
