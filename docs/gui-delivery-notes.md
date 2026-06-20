# GUI delivery notes

This milestone changes the native/browser GUI shell, app shell, chat TUI, feed cancellation, PTY smoke harness, and GUI evidence docs.

## Scope changed by this GUI milestone

- `internal/gui`: local-only native/browser GUI server, static shell/API contracts, JS syntax check, and browser smoke gate.
- `internal/app`: app lifetime cancellation, Base canvas painting, every-page and feature-specific journeys, visual goldens, resource soaks, PTY app smoke support.
- `internal/tui`: notepad persistence, git panel render-safety, file completion bounds, composer/plan/rail/right-panel feature journeys, visual goldens, resource/race checks, PTY chat smoke support.
- `internal/feed`: cancellation-aware feed/GitHub/model suggestion scans.
- root tests/smoke hooks: release-safe smoke command behavior, smoke-tag helper builds, PTY command/app/TUI tests.
- `docs/gui-*`: parity matrix, evidence map, phase gate, and delivery notes.

## Pre-existing staged files not owned by this GUI phase

These files were already staged before the GUI work began and are intentionally not part of this phase's ownership:

- `internal/command/command_test.go`
- `internal/memory/memory_test.go`
- `internal/memory/redact_test.go`

Do not infer GUI-phase behavior from those staged files; review/commit them separately.

## Verification gate used for this phase

```bash
scripts/verify-gui-phase.sh
```

Expanded commands:

```bash
go test ./... -count=1
go test . ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -count=1
go test -tags 'wails production webkit2_41' ./internal/gui -count=1
node --check internal/gui/static/app.js
scripts/gui-smoke.sh
go test ./docs ./internal/app ./internal/feed ./internal/gui ./internal/tui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test -tags smoke . -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
```

## Current completion status

The full GUI parity milestone for shipped Eigen surfaces is accepted in `docs/gui-current-surface-acceptance.md`; no shipped GUI feature-parity blocker remains.
