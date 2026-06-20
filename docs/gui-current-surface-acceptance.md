# GUI full parity acceptance map

This document maps the persistent GUI goal to concrete shipped Eigen surfaces and evidence. The Eigen product in this repository now exposes three desktop GUI surfaces: native/browser GUI shell, premium app shell, and premium chat TUI.

## Shipped GUI surfaces

The shipped desktop GUI surfaces are:

- native/browser desktop GUI (`internal/gui`): local-only server, static premium shell, health/profile/session API seams, service validation, stream shutdown, browser smoke;
- premium app shell (`internal/app`): page navigation, visual shell, command palette, config/skills/memory/plugins/provider mutating workflows, release-binary PTY soak;
- premium chat TUI (`internal/tui`): composer/transcript, tool-turn rendering, plan/changes/git/terminal/notepad/tasks/shells/right panels, rail, palette, voice/read controls, keyboard parity;
- resource/lifecycle evidence: render-loop no-spawn/no-growth checks, live-loop subprocess bounds, background shell subprocess + FD settling, transcript concurrent-save durability, daemon listener cleanup, cancellation paths.

## Criterion-to-evidence map

| Goal criterion | Acceptance evidence |
| --- | --- |
| Premium desktop app shell exists | Native/browser GUI shell is local-only and smoke-tested (`internal/gui:TestServeRejectsNonLocalBind`, `TestHandlerStaticAndAPIContracts`, `scripts/gui-smoke.sh`); app shell has premium visual contracts/goldens (`internal/app:TestAppPremiumShellVisualContract`, `TestAppLiveSessionsPluginsGoldenSnapshotTokens`); release app shell screenshot artifact exists (`docs/artifacts/gui/release-app-shell.png`). |
| Includes all Eigen GUI features | Feature parity matrix maps native GUI, app pages, chat panels, mutating pages, and keyboard/accessibility seams (`docs/gui-feature-parity.md`, `docs/gui-mutating-pages-evidence.md`, `docs/gui-accessibility-keyboard-audit.md`). No shipped GUI feature is recorded as missing. |
| End-to-end tested | Enforced phase gate runs full/current GUI packages, native GUI smoke, shuffle/race/smoke/PTX-style PTY checks (`scripts/verify-gui-phase.sh`); main GUI phase gate run `27863744643` is green at `8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9`. |
| High UI/UX quality | Premium shell visual contracts, all-page goldens, richer live/sessions/plugins goldens, TUI composer/right-panel goldens, screenshot artifacts, and keyboard parity evidence are recorded in `docs/gui-parity-evidence.md`. |
| Measured no resource leak/misuse for covered flows | App/TUI render/live-loop resource tests, git subprocess cache bounds, feed/GitHub/model cancellation tests, background shell journey with FD settling, transcript concurrent-save durability, and daemon handler/listener cleanup are recorded in `docs/gui-parity-evidence.md`. |
| Independent review and delivery gate | Review findings were addressed: daemon second-listen now asserts exact failure reason; keyboard/accessibility evidence is scoped precisely; final blocker resolution is recorded in `docs/gui-final-review-resolution.md`. Clean detached `origin/main` local gates pass: `bash scripts/verify-gui-phase.sh` and `go test ./... -count=1`. Main CI run `27863744647` is green at `8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9`. |

## Authoritative verification evidence

- Default branch SHA: `8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9`.
- Main GUI phase gate: `27863744643`, `success`, `push`, `headBranch=main`, `headSha=8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9`, URL `https://github.com/avifenesh/eigen/actions/runs/27863744643`.
- Main CI: `27863744647`, `success`, `push`, `headBranch=main`, `headSha=8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9`, URL `https://github.com/avifenesh/eigen/actions/runs/27863744647`.
- Clean detached local verification from `origin/main`: `bash scripts/verify-gui-phase.sh` and `go test ./... -count=1` both passed.
- `scripts/verify-gui-phase.sh` validates the shipped surfaces by running the full Go suite, focused GUI package tests including native GUI/app/TUI packages, Wails-tag native GUI compile/test (`go test -tags 'wails production webkit2_41' ./internal/gui -count=1`), JS syntax and behavior checks (`node --check internal/gui/static/app.js`, `node internal/gui/static/app_behavior_test.mjs`), native GUI browser smoke, smoke-tagged PTY binary tests, shuffle tests, race tests, release app shell PTY soak, and repeated chat/app PTY smoke tests.

## Acceptance statement

The persistent GUI goal is complete and judgeable: premium desktop shell, all current GUI features, E2E testing, high UI/UX evidence, and measured resource-safety coverage are all mapped to tests/artifacts/CI. Subsequent product changes should create new acceptance rows when they introduce new GUI functionality or regress an evidenced contract.
