# GUI final acceptance checklist

This checklist records the current status of the persistent GUI desktop-app goal after the latest independent review.

## Completed evidence

- Full local GUI phase gate passes: `scripts/verify-gui-phase.sh`.
- Full repository tests pass inside that gate: `go test ./... -count=1`.
- GUI package shuffle/race gates pass.
- Smoke-tag root suite and repeated PTY smoke suite pass.
- Release-binary PTY longer soak passes: `TestPTYReleaseAppShellLongerSoak`.
- CI workflow exists: `.github/workflows/gui-phase.yml`, running `xvfb-run -a scripts/verify-gui-phase.sh`.
- Repo-local desktop screenshots exist and are valid PNGs with decoded dimensions:
  - `docs/artifacts/gui/release-app-shell.png`
  - `docs/artifacts/gui/chat-tui-shell.png`
- Screenshot manifest records the final displayed xterm/tmux evidence:
  - release app shell: `agent-workspace:screenshot-release`, `[screensho1:eigen-release*]`, `"avifenesh" 04:13 20-Jun-26`;
  - chat TUI shell: `agent-workspace:screenshot-chat`, `[screensho1:eigen-smoke.test*]`, `"avifenesh" 04:15 20-Jun-26`.

## Remaining blockers before calling `goal_achieved`

1. **Green CI run evidence**
   - Need a specific successful GitHub Actions run for `.github/workflows/gui-phase.yml` on the target commit.
   - Required evidence: commit SHA plus run URL or run ID.
   - Current live check: local HEAD is `6a87330886df61fbdd8f304d807f4dec21b886eb`; `gh run list --workflow gui-phase.yml --limit 5` returned `HTTP 404: workflow gui-phase.yml not found on the default branch`, so this blocker is still open until the workflow is pushed/landed and a green run exists.

2. **Docs completion reconciliation**
   - Docs that currently describe the goal as non-final/persistent must be reconciled before claiming completion.
   - Until then, `goal_achieved` would contradict repository evidence.

## Screenshot evidence caveat

The PNG tests verify files, decoding, and dimensions. The mapping from pixels to UI features is human-attested in `docs/gui-screenshot-artifacts.md`; CI does not yet regenerate screenshots from the current build.
