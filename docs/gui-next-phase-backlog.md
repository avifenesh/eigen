# GUI next-phase backlog

This backlog translates the remaining full-goal criteria into concrete next work. The current phase is verified by `scripts/verify-gui-phase.sh`; these items are what keep the persistent desktop-app goal active after this phase.

## P0 — full desktop-app parity

1. **Real desktop sandbox journey** — partially complete
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
   - Remaining: optional richer recordings/pixel-review packages if final acceptance requires more than the current screenshot artifacts.

2. **Longer live binary soak** — in progress
   - Extend from smoke-length PTY tests to a longer real-binary loop.
   - Exercise app page navigation, chat composer, right panels, background tasks, shells, and exit.
   - Completed deterministic premium TUI interaction/view soak (`TestPremiumInteractionViewSoak`) for chat composer, mention completion, submitted turn rendering, right-panel tabs, notepad, tasks empty state, changes empty state, and repeated composer edits; this intentionally avoids claiming runtime leak coverage because it does not execute Bubble Tea commands.
   - Existing runtime/resource coverage remains in PTY/resource tests (`TestPTYReleaseAppShellLongerSoak`, chat PTY smoke, render/live-loop resource tests).
   - Added command-executing agent shell journey (`TestAgentBackgroundShellToolJourneySettlesResources`) for a real background bash subprocess, bash_output polling, kill_shell cleanup, zero remaining running shells, file-descriptor baseline/delta settling when `/proc/self/fd` is available, and UI-facing event/request propagation.

3. **Richer visual review artifacts** — substantially complete
   - Expanded stable token goldens for representative desktop widths.
   - Added app live/sessions/plugins marketplace+wiring snapshot tokens (`TestAppLiveSessionsPluginsGoldenSnapshotTokens`) beyond the existing home/palette/every-page goldens.
   - Added TUI task/shell/notepad premium surface snapshot tokens (`TestTUIRightPanelPremiumSurfaceGoldenSnapshotTokens`) beyond the existing composer/transcript/git/right-tab goldens.
   - Remaining: optional pixel-review/video package if final acceptance requires richer artifacts than PNG screenshots plus ANSI/token goldens.

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
   - Remaining optional hardening: PTY-level local-provider tool turn through the compiled binary.

6. **Accessibility/keyboard audit** — evidence mapped
   - Every current clickable TUI chrome action dispatches through the shared validated action registry and has a documented keyboard path.
   - App shell, chat TUI, and native/browser GUI seams are mapped to automated keyboard/accessibility evidence.
   - Evidence map: `docs/gui-accessibility-keyboard-audit.md`.
   - Remaining optional hardening: pixel/video focus-ring review and browser tab-order/ARIA automation when the browser GUI becomes the primary desktop surface.

## P2 — release readiness

7. **Clean-tree delivery gate**
   - Re-run `scripts/verify-gui-phase.sh` from a clean tree after separating pre-existing staged memory/command changes.
   - Add any project-wide release commands required outside this GUI phase.

8. **Independent final review**
   - Provide the final diff plus verification output to Opus/GLM or another reviewer.
   - Fix any concrete blockers and record them in `docs/gui-parity-evidence.md`.

## Definition of done for the persistent goal

The persistent goal can be submitted to `goal_achieved` only when:

- this backlog's P0 items are complete;
- all phase gate commands pass;
- release/clean-tree gates pass;
- independent final review has no unresolved blockers;
- evidence is concrete enough for a judge to verify without relying on intent.
