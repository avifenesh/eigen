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

2. **Longer live binary soak**
   - Extend from smoke-length PTY tests to a longer real-binary loop.
   - Exercise app page navigation, chat composer, right panels, background tasks, shells, and exit.
   - Record goroutine/subprocess/file-descriptor bounds or equivalent process-level metrics.

3. **Richer visual review artifacts**
   - Expand stable token goldens into full ANSI/text snapshots or visual screenshots for representative desktop widths.
   - Cover app home/live/sessions/plugins/config and TUI composer/transcript/tasks/shells/notepad/terminal.

## P1 — feature-depth and UX polish

4. **Per-feature E2E workflows for app-side mutating pages**
   - Config multi-select provider save.
   - Skills install prompt success/failure UI.
   - Memory delete/consolidate flows with confirmation/error states.
   - Plugin marketplace install/remove/disable with realistic catalog data.

5. **Chat TUI end-to-end agent turn with tools**
   - Fake provider + fake tool registry flow that renders tool start/output, todo updates, changes panel, and final assistant answer in one journey.
   - Verify no render-path subprocesses or unexpected goroutines during the flow.

6. **Accessibility/keyboard audit**
   - Ensure every clickable action has keyboard parity.
   - Verify focus/focused-row visual language is consistent and non-brand.

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
