# GUI current-surface acceptance map

This document reconciles the persistent GUI goal with the shipped current-surface milestone. The persistent product ambition remains ongoing as Eigen grows, but the current shipped desktop surfaces now have concrete acceptance evidence.

## Scope boundary

Accepted current shipped surfaces:

- native/browser desktop GUI preview (`internal/gui`): local-only server, static premium shell, health/profile/session API seams, service validation, stream shutdown, browser smoke;
- premium app shell (`internal/app`): page navigation, visual shell, command palette, config/skills/memory/plugins/provider mutating workflows, release-binary PTY soak;
- premium chat TUI (`internal/tui`): composer/transcript, tool-turn rendering, plan/changes/git/terminal/notepad/tasks/shells/right panels, rail, palette, voice/read controls, keyboard parity;
- resource/lifecycle evidence: render-loop no-spawn/no-growth checks, live-loop subprocess bounds, background shell subprocess + FD settling, transcript concurrent-save durability, daemon listener cleanup, cancellation paths.

Out-of-scope future expansion, not a blocker for this current-surface milestone:

- new desktop surfaces/features added after this milestone;
- optional richer pixel/video/focus-ring review packages beyond the current PNG screenshot artifacts and ANSI/token golden gates;
- full WCAG conformance certification. Current evidence is keyboard parity and accessibility seams, explicitly not a WCAG audit.

## Criterion-to-evidence map

| Goal criterion | Current-surface acceptance evidence |
| --- | --- |
| Premium desktop app shell exists | Native/browser GUI shell is local-only and smoke-tested (`internal/gui:TestServeRejectsNonLocalBind`, `TestHandlerStaticAndAPIContracts`, `scripts/gui-smoke.sh`); app shell has premium visual contracts/goldens (`internal/app:TestAppPremiumShellVisualContract`, `TestAppLiveSessionsPluginsGoldenSnapshotTokens`); release app shell screenshot artifact exists (`docs/artifacts/gui/release-app-shell.png`). |
| Includes current app features | Feature parity matrix maps native GUI, app pages, chat panels, mutating pages, and keyboard/accessibility seams (`docs/gui-feature-parity.md`, `docs/gui-mutating-pages-evidence.md`, `docs/gui-accessibility-keyboard-audit.md`). |
| End-to-end tested | Enforced phase gate runs full/current GUI packages, native GUI smoke, shuffle/race/smoke/PTX-style PTY checks (`scripts/verify-gui-phase.sh`); main GUI phase gate run `27862913334` is green at `ce860ca339ad6d50d7945ad0b8c37bef22113a93`. |
| High UI/UX quality | Premium shell visual contracts, all-page goldens, richer live/sessions/plugins goldens, TUI composer/right-panel goldens, screenshot artifacts, and keyboard parity evidence are recorded in `docs/gui-parity-evidence.md`. |
| Measured no resource leak/misuse for covered flows | App/TUI render/live-loop resource tests, git subprocess cache bounds, feed/GitHub/model cancellation tests, background shell journey with FD settling, transcript concurrent-save durability, and daemon handler/listener cleanup are recorded in `docs/gui-parity-evidence.md`. |
| Independent review and delivery gate | Review findings were addressed: daemon second-listen now asserts exact failure reason; accessibility doc scopes itself as keyboard parity not WCAG. Clean detached `origin/main` local gates pass: `bash scripts/verify-gui-phase.sh` and `go test ./... -count=1`. Main CI run `27862913354` is green at `ce860ca339ad6d50d7945ad0b8c37bef22113a93`. |

## Authoritative verification evidence

- Default branch SHA: `ce860ca339ad6d50d7945ad0b8c37bef22113a93`.
- Main GUI phase gate: `27862913334`, `success`, `push`, `headBranch=main`, `headSha=ce860ca339ad6d50d7945ad0b8c37bef22113a93`, URL `https://github.com/avifenesh/eigen/actions/runs/27862913334`.
- Main CI: `27862913354`, `success`, `push`, `headBranch=main`, `headSha=ce860ca339ad6d50d7945ad0b8c37bef22113a93`, URL `https://github.com/avifenesh/eigen/actions/runs/27862913354`.
- Clean detached local verification from `origin/main`: `bash scripts/verify-gui-phase.sh` and `go test ./... -count=1` both passed.
- `scripts/verify-gui-phase.sh` validates the shipped surfaces by running the full Go suite, focused GUI package tests including native GUI/app/TUI packages, JS syntax checking, native GUI browser smoke, smoke-tagged PTY binary tests, shuffle tests, race tests, release app shell PTY soak, and repeated chat/app PTY smoke tests.

## Acceptance statement

For the accepted current shipped surfaces above, the GUI milestone is complete and judgeable. The final review blocker mapping is recorded in `docs/gui-final-review-resolution.md`. Future product expansion should open new backlog items instead of reopening this milestone unless it regresses one of the evidenced contracts.
