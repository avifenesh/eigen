# GUI final review resolution

This file answers the final independent review blockers before calling the shipped Eigen GUI milestone complete.

## Blocker 1: goalpost-moving check

No functional blocker from the prior review was reclassified as incomplete maintenance work.

| Prior review blocker | Classification | Resolution |
| --- | --- | --- |
| Docs contradicted a completion claim by saying the phase was non-final. | Clarity/evidence blocker. | Replaced the non-final language with an explicit full GUI parity milestone scope in `docs/gui-phase-gate.md` and `docs/gui-current-surface-acceptance.md`. |
| Need criterion-to-evidence map for the goal text. | Clarity/evidence blocker. | Added `docs/gui-current-surface-acceptance.md` with rows for premium desktop app shell, current app features, E2E testing, UI/UX quality, resource safety, and delivery review. |
| Need verified main CI run SHAs/URLs. | Evidence blocker. | Recorded main run IDs `27863744643` and `27863744647`, both success at `8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9`; verified with `gh run view`. |
| Daemon second-listen test accepted any error. | Functional/test-quality blocker. | Fixed `internal/daemon:TestDaemonSecondListenFails` to assert exact `daemon already running`; verified race gate green. |
| Accessibility wording overclaimed as a broad audit. | Clarity/evidence blocker. | Scoped `docs/gui-accessibility-keyboard-audit.md` as a keyboard-parity evidence map rather than an external certification document. |
| Native/browser GUI shell was underrepresented relative to app/TUI. | Functional/evidence blocker. | Added `internal/gui` service/server/static tests, JS syntax check, and native GUI smoke to the enforced phase gate. |
| Command-executing background shell/resource evidence was missing. | Functional/evidence blocker. | Added `internal/agent:TestAgentBackgroundShellToolJourneySettlesResources` with real background bash, `bash_output`, `kill_shell`, no running shell, event propagation, and FD settling. |
| TUI tool-turn evidence was missing. | Functional/evidence blocker. | Added `internal/tui:TestTUIToolTurnDrivesPlanChangesAndTaskPanels` covering submitted turn, reasoning, todo/edit tool events, plan state, changes panel, and fitted right panels. |
| App-side mutating page evidence was scattered. | Evidence blocker. | Added `docs/gui-mutating-pages-evidence.md` mapping config/skills/memory/plugins/providers mutating workflows to tests. |
| Keyboard/accessibility parity evidence was scattered. | Evidence blocker. | Added `docs/gui-accessibility-keyboard-audit.md` mapping keyboard paths and accessibility seams to tests. |

Maintenance extensions listed in backlog are not unresolved feature-parity blockers. The prior accessibility blocker was overclaiming language, not a discovered missing focus indicator or failed certification requirement.

## Blocker 2: gate circularity check

The final documentation alignment does **not** change `scripts/verify-gui-phase.sh` or `.github/workflows/gui-phase.yml`.

The gate substance was established and landed before final acceptance. It includes real behavioral checks:

- `go test ./... -count=1`;
- GUI package tests including `internal/gui`, `internal/app`, and `internal/tui`;
- `node --check internal/gui/static/app.js`;
- `scripts/gui-smoke.sh` native/browser GUI launch smoke;
- smoke-tagged PTY binary tests;
- shuffle tests;
- race tests;
- release app shell longer PTY soak;
- repeated chat/app PTY smoke tests.

The executable gate script does not read `docs/gui-phase-summary.json`, `docs/gui-current-surface-acceptance.md`, or `docs/gui-final-review-resolution.md`; only docs unit tests read those files to prevent evidence drift. The gate verdict is driven by Go tests, JS syntax checking, native GUI smoke, PTY smoke/soak tests, shuffle tests, and race tests.

## Blocker 3: evidence commit boundary check

Default branch `origin/main` is `8b2c6f7040f28a27c634e6ccb3a3fbc0bee7a1d9` after final acceptance. It has direct green main checks:

- GUI phase gate `27863744643`, success;
- CI `27863744647`, success.

Clean detached local verification from `origin/main` also passed:

- `bash scripts/verify-gui-phase.sh`;
- `go test ./... -count=1`.

## Conclusion

The review blockers are resolved for the shipped Eigen GUI milestone. Maintenance extensions are not used to excuse missing shipped-surface functionality.
