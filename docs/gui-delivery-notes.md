# GUI delivery notes

This phase changes the app shell, chat TUI, feed cancellation, PTY smoke harness, and GUI evidence docs.

## Scope changed by this GUI phase

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
go test . ./docs ./internal/app ./internal/feed ./internal/tui -count=1
go test ./docs ./internal/app ./internal/feed ./internal/tui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test -tags smoke . -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
```

## Current completion status

This is a strong GUI phase suitable for review. It is not a final claim that the persistent full-parity desktop-app goal is complete; see `docs/gui-phase-gate.md` for remaining final-goal criteria, `docs/gui-next-phase-backlog.md` for actionable next work, and `docs/gui-phase-summary.json` for a machine-readable handoff.
