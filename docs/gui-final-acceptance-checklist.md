# GUI final acceptance checklist

This checklist records the current status of the persistent GUI desktop-app goal after the latest independent review and CI unblock work.

## Completed evidence

- Full local GUI phase gate passes: `scripts/verify-gui-phase.sh`.
- Full repository tests pass inside that gate: `go test ./... -count=1`.
- GUI package shuffle/race gates pass.
- Smoke-tag root suite and repeated PTY smoke suite pass.
- Release-binary PTY longer soak passes: `TestPTYReleaseAppShellLongerSoak`.
- CI workflow exists and is green for this phase: `.github/workflows/gui-phase.yml`, running `xvfb-run -a scripts/verify-gui-phase.sh`.
- Green GitHub Actions evidence for PR #3:
  - workflow: `GUI phase gate`;
  - run ID: `27859059893`;
  - run URL: `https://github.com/avifenesh/eigen/actions/runs/27859059893`;
  - head SHA: `42c8a08f8b4752495f42e6a5aafc6aa0ae8c4077`;
  - observed status: success via `gh run watch 27859059893 --exit-status`.
- Repo-local desktop screenshots exist and are valid PNGs with decoded dimensions:
  - `docs/artifacts/gui/release-app-shell.png`
  - `docs/artifacts/gui/chat-tui-shell.png`
- Screenshot manifest records the final displayed xterm/tmux evidence:
  - release app shell: `agent-workspace:screenshot-release`, `[screensho1:eigen-release*]`, `"avifenesh" 04:13 20-Jun-26`;
  - chat TUI shell: `agent-workspace:screenshot-chat`, `[screensho1:eigen-smoke.test*]`, `"avifenesh" 04:15 20-Jun-26`.

## Final-claim status

The green-CI blocker for this GUI phase is resolved by run `27859059893` on head SHA `42c8a08f8b4752495f42e6a5aafc6aa0ae8c4077`.

This checklist is still phase-scoped: the persistent product goal should only be claimed when the evidence in `docs/gui-phase-gate.md` and `docs/gui-parity-evidence.md` is accepted as sufficient for the current desktop-app parity phase, or after any newly requested broader parity rows are added and gated.

## Screenshot evidence caveat

The PNG tests verify files, decoding, and dimensions. The mapping from pixels to UI features is human-attested in `docs/gui-screenshot-artifacts.md`; CI does not yet regenerate screenshots from the current build.
