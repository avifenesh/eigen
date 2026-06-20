# GUI full parity gate

This repository is moving toward the long-running goal: a premium desktop Eigen app with full feature parity, high UI/UX, E2E coverage, and measured resource safety.

The current implementation is the **full GUI parity milestone for Eigen's shipped desktop surfaces**. New GUI functionality introduced later must add new evidence rows before release, but no shipped GUI feature is recorded as missing.

## Current milestone: native GUI shell + app shell + core chat surfaces

This milestone is considered covered when the reproducible gate passes:

```bash
scripts/verify-gui-phase.sh
```

Expanded commands:

```bash
go test ./... -count=1
go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1
node --check internal/gui/static/app.js
scripts/gui-smoke.sh
go test -tags smoke . -count=1
go test ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
```

It covers:

- native/browser desktop GUI local-only server, static shell/API contracts, service validation, stream shutdown seams, JS syntax, and browser smoke;
- app shell canvas ownership, visual contracts, all-page keyboard journeys, mutating-page workflows, and PTY navigation soak;
- chat TUI composer/transcript journeys, tool-turn plan/changes flow, right-panel journeys, keyboard parity, and PTY startup/quit smoke;
- resource-safety regressions for app render, TUI render/live-loop, git panel subprocesses, file completion, feed/model/GitHub cancellation, background shell FD settling, daemon listener cleanup, and transcript concurrency durability;
- evidence, feature-parity, keyboard-parity, mutating-page, and acceptance docs guarded by tests.

## Current-surface acceptance

The goal criteria and evidence for this milestone are mapped in `docs/gui-current-surface-acceptance.md`.

The current milestone is ready to submit to `goal_achieved` when:

1. the gate above passes from a clean checkout;
2. CI and GUI phase gate are green on the default-branch commit being claimed;
3. independent review has no unresolved blockers;
4. all shipped GUI surfaces are mapped to evidence and no feature-parity blocker remains.

That condition is met for `origin/main` commit `ce860ca339ad6d50d7945ad0b8c37bef22113a93` by CI runs `27862913334` and `27862913354`, plus clean detached local verification.

## Continuing maintenance

If new GUI features are introduced later, they must add evidence rows and gates before release. This does not defer any currently shipped feature-parity requirement.
