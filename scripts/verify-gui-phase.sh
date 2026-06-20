#!/usr/bin/env bash
set -euo pipefail

# Reproducible gate for the GUI desktop-app phase. Keep in sync with
# docs/gui-parity-evidence.md, docs/gui-phase-gate.md, and docs/gui-delivery-notes.md.

go test ./... -count=1
go test . ./docs ./internal/app ./internal/feed ./internal/tui -count=1
go test ./docs ./internal/app ./internal/feed ./internal/tui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test -tags smoke . -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
