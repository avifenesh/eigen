# GUI phase gate

This repository is moving toward the long-running goal: a premium desktop Eigen app with full feature parity, high UI/UX, E2E coverage, and measured resource safety.

The current implementation is a **phase-complete evidence slice**, not a final claim that every feature in Eigen has full desktop parity. This gate makes that explicit so future work cannot accidentally treat the current test set as full-goal completion.

## Current phase: shell + core chat surfaces

This phase is considered covered when the reproducible gate passes:

```bash
scripts/verify-gui-phase.sh
```

Expanded commands:

```bash
go test ./... -count=1
go test . ./docs ./internal/app ./internal/feed ./internal/tui -count=1
go test -tags smoke . -count=1
go test ./docs ./internal/app ./internal/feed ./internal/tui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
```

It covers:

- app shell canvas ownership, visual contracts, all-page keyboard journeys, and PTY navigation soak;
- chat TUI composer/transcript journeys, right-panel journeys, and PTY startup/quit smoke;
- resource-safety regressions for app render, TUI render/live-loop, git panel subprocesses, file completion, feed/model/GitHub cancellation;
- evidence and feature-parity docs guarded by tests.

## Required before claiming the persistent goal fully achieved

Do **not** call the whole goal complete until these are also true:

1. **Deep feature journeys for every parity row**
   - Every row in `docs/gui-feature-parity.md` has a feature-specific journey, not only a generic layout/fit or render-soak reference.
2. **Longer real-binary soak**
   - A real PTY or desktop-sandbox run exercises app shell and chat TUI loops for materially longer than the current smoke tests and records bounded goroutine/subprocess behavior.
3. **Visual snapshot/golden workflow**
   - App pages, TUI right-panel tabs, and central TUI composer/transcript states now have stable token goldens. Expand to richer snapshots/pixel review if final acceptance requires it.
4. **Independent review blockers fixed**
   - Final independent review blockers found in this phase have been fixed and test-guarded: release smoke commands now fail explicitly instead of false-green no-op/fallthrough, notepad dirty notes flush on quit, GitHub/feed subprocess cancellation is context-backed, and git/feed counters are atomic.
5. **Delivery-quality gate**
   - Normal tests, smoke-tagged PTY tests, and any broader project-required build/test gates pass from a clean working tree.

## Current non-final verdict

The current GUI work is suitable as a strong phase toward the persistent goal. It is not, by this document's own criteria, enough to claim the full persistent goal is judge-ready.
