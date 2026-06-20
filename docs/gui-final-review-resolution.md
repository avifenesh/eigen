# GUI final review resolution

This file answers the final independent review blockers before calling the current shipped-surface GUI milestone complete.

## Blocker 1: goalpost-moving check

No functional blocker from the prior review was demoted to future scope.

| Prior review blocker | Classification | Resolution |
| --- | --- | --- |
| Docs contradicted a completion claim by saying the phase was non-final. | Clarity/evidence blocker. | Replaced the non-final language with an explicit current shipped-surface milestone scope in `docs/gui-phase-gate.md` and `docs/gui-current-surface-acceptance.md`. |
| Need criterion-to-evidence map for the goal text. | Clarity/evidence blocker. | Added `docs/gui-current-surface-acceptance.md` with rows for premium desktop app shell, current app features, E2E testing, UI/UX quality, resource safety, and delivery review. |
| Need verified main CI run SHAs/URLs. | Evidence blocker. | Recorded main run IDs `27862913334` and `27862913354`, both success at `ce860ca339ad6d50d7945ad0b8c37bef22113a93`; verified with `gh run view`. |
| Daemon second-listen test accepted any error. | Functional/test-quality blocker. | Fixed `internal/daemon:TestDaemonSecondListenFails` on main via PR #4 to assert exact `daemon already running`; verified race gate green. |
| Accessibility wording overclaimed as a broad audit. | Clarity/evidence blocker. | Scoped `docs/gui-accessibility-keyboard-audit.md` as a keyboard-parity evidence map and explicitly not full WCAG certification. |
| Native/browser GUI shell was underrepresented relative to app/TUI. | Functional/evidence blocker. | Added `internal/gui` service/server/static tests, JS syntax check, and native GUI smoke to the enforced phase gate in PR #3. |
| Command-executing background shell/resource evidence was missing. | Functional/evidence blocker. | Added `internal/agent:TestAgentBackgroundShellToolJourneySettlesResources` with real background bash, `bash_output`, `kill_shell`, no running shell, event propagation, and FD settling. |
| TUI tool-turn evidence was missing. | Functional/evidence blocker. | Added `internal/tui:TestTUIToolTurnDrivesPlanChangesAndTaskPanels` covering submitted turn, reasoning, todo/edit tool events, plan state, changes panel, and fitted right panels. |
| App-side mutating page evidence was scattered. | Evidence blocker. | Added `docs/gui-mutating-pages-evidence.md` mapping config/skills/memory/plugins/providers mutating workflows to tests. |
| Keyboard/accessibility parity evidence was scattered. | Evidence blocker. | Added `docs/gui-accessibility-keyboard-audit.md` mapping keyboard paths and accessibility seams to tests. |

Items listed as future scope are not prior functional blockers. They are future expansion policy choices: new surfaces/features added after this milestone, optional richer pixel/video/focus-ring packages, and full WCAG certification.

## Blocker 2: gate circularity check

PR #5 does **not** change `scripts/verify-gui-phase.sh` or `.github/workflows/gui-phase.yml`.

The gate substance was established and landed before PR #5. It includes real behavioral checks:

- `go test ./... -count=1`;
- current GUI package tests including `internal/gui`, `internal/app`, `internal/tui`;
- `node --check internal/gui/static/app.js`;
- `scripts/gui-smoke.sh` native/browser GUI launch smoke;
- smoke-tagged PTY binary tests;
- shuffle tests;
- race tests;
- release app shell longer PTY soak;
- repeated chat/app PTY smoke tests.

PR #5 only reconciles acceptance documentation and docs guard tests. It does not weaken the enforced gate.

## Blocker 3: evidence commit boundary check

Acceptance evidence currently cites main commit `ce860ca339ad6d50d7945ad0b8c37bef22113a93` because that is the default-branch commit containing all functional/test/gate changes from PR #3 and PR #4.

PR #5 delta is documentation-only:

```text
A docs/gui-current-surface-acceptance.md
M docs/gui-delivery-notes.md
M docs/gui-next-phase-backlog.md
M docs/gui-parity-evidence.md
M docs/gui-phase-gate.md
M docs/gui-phase-summary.json
A docs/gui_current_surface_acceptance_test.go
M docs/gui_delivery_notes_test.go
M docs/gui_next_phase_backlog_test.go
M docs/gui_parity_evidence_test.go
M docs/gui_phase_gate_test.go
M docs/gui_phase_summary_test.go
```

Because PR #5 does not change production code, test logic outside docs, workflow files, or the gate script, the `ce860ca...` functional evidence transfers to PR #5 for behavior. PR #5 also has its own green PR checks:

- GUI phase gate `27863260532`, success;
- CI `27863260578`, success.

After PR #5 merges, the acceptance document should be considered fully current only after the merge SHA has green main CI/GUI runs or after confirming the merge delta remains documentation-only and docs tests pass.

## Conclusion

The review blockers are resolved for the current shipped-surface milestone. Future-scope items are explicitly separated and are not used to excuse missing current-surface functionality.
