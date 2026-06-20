# GUI screenshot artifacts

Durable desktop screenshot artifacts captured during the GUI phase.

## Release app shell

- Artifact: `docs/artifacts/gui/release-app-shell.png`
- Source capture: isolated X11 workspace, xterm/tmux terminal, release binary built with `go build -buildvcs=false -o /tmp/eigen-release .`, temp `HOME`, `TERM=xterm-256color`.
- Visual assertions from captured artifact:
  - Premium app shell is visible in a foreground desktop xterm window titled `agent-workspace:screenshot-release`, with a browser/other desktop window partially visible behind it, tmux status identifying `[screensho1:eigen-release*]`, user label `"avifenesh"`, and timestamp `04:13 20-Jun-26`.
  - Sidebar includes home, live, projects, machines, sessions, config, skills, models, providers, memory, crons, plugins.
  - Home content includes `λ eigen`, `burning the midnight oil — what's next?`, `0 sessions 0 projects 0 skills`, recent section, and `no sessions yet — press n to start one`.
  - Footer includes keyboard help: `tab pages · j/k move · enter open · n new · : palette · q quit`.
  - The screenshot was visually reviewed in-session after capture and again in the final verification pass.

## Chat TUI shell

- Artifact: `docs/artifacts/gui/chat-tui-shell.png`
- Source capture: isolated X11 workspace, xterm/tmux terminal, smoke-tagged chat TUI helper, fake local provider, smoke session, `TERM=xterm-256color`.
- Visual assertions from captured artifact:
  - Premium chat shell is visible in a foreground desktop xterm window titled `agent-workspace:screenshot-chat`, with a browser/other desktop window partially visible behind it, tmux status identifying `[screensho1:eigen-smoke.test*]`, user label `"avifenesh"`, and timestamp `04:15 20-Jun-26`.
  - Left rail includes Eigen mark, `untitled session`, navigation links, `right panel`, `tasks 22x`, and session metadata (`smoke`, `perm=auto`, `input=steer`).
  - Center panel includes `λ eigen · type a task to begin` and the composer placeholder `type a task…  (enter send · ctrl+j newline · / commands · ↑↓ history · ctrl+c quit)`.
  - Right panel includes tabs `[chg] [git] [trm] [tsk] [nt] [X]` and the visible truncated no-edits placeholder `no edits yet — file changes will sho`.
  - Footer includes composer help and voice controls exactly as shown in the final displayed screenshot: `● speak · ▶ read · ⊙ voice`.
  - The screenshot was visually reviewed in-session after capture and again in the final verification pass.

These screenshots are evidence artifacts, not the only verification. Automated structural/golden, PTY, race, and soak checks are still run by `scripts/verify-gui-phase.sh`.
