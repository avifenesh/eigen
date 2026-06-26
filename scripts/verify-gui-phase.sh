#!/usr/bin/env bash
set -euo pipefail

# Reproducible gate for the GUI desktop-app phase. Keep in sync with
# docs/gui-parity-evidence.md, docs/gui-phase-gate.md, and docs/gui-delivery-notes.md.
#
# internal/gui is the Wails desktop package: it imports Wails (cgo → webkitgtk),
# so it ONLY builds under a webkit tag. The default build is gtk4/webkitgtk-6.0;
# this gate's runner provides webkit2gtk-4.1, so the gui package is exercised
# with `-tags gtk3` (and the full production tag set on its own line). The
# untagged `./...` runs therefore EXCLUDE internal/gui — it cannot compile
# tagless — while every other package is covered tagless as before.
GUI_TAGS='gtk3'
NON_GUI_PKGS="$(go list ./... | grep -v '/internal/gui')"

go test $NON_GUI_PKGS -count=1
go test . ./internal/app ./internal/feed ./internal/tui -count=1
go test -tags "$GUI_TAGS" ./internal/gui -count=1
go test -tags 'wails production webkit2_41 gtk3' ./internal/gui -count=1
# The GUI frontend is the embedded Svelte bundle under internal/gui/frontend/dist
# (built by `vite build`, committed, and go:embed-ed into the binary). The old
# browser GUI (internal/gui/static/app.js + gui-smoke.sh's HTTP launch) was
# removed in the Wails v3 rebuild, so its node/app.js/gui-smoke checks are gone.
# Assert the embedded bundle the Go build depends on is present instead.
test -f internal/gui/frontend/dist/index.html \
  || { echo "gui phase: missing internal/gui/frontend/dist/index.html (run 'pnpm build' in internal/gui/frontend)" >&2; exit 1; }
go test ./internal/app ./internal/feed ./internal/tui -shuffle=on -count=1
go test -tags "$GUI_TAGS" ./internal/gui -shuffle=on -count=1
go test -race ./internal/app ./internal/feed ./internal/tui -count=1
go test -tags smoke . -count=1
go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1
go test -tags smoke . -run 'TestPTYChatTUISmokeQuit|TestPTYAppShellNavigationSoak|TestPTYSmokeAppShellKeyboardNavigation|TestPTYSmokeVersionCommand' -count=5
