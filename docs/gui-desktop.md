# Eigen desktop GUI

Eigen's non-terminal GUI is a Wails desktop app backed by the existing local
Eigen daemon. The daemon remains the source of truth for sessions, transcripts,
approvals, memory, and tool events; the GUI is a desktop surface over that
state, not a separate agent runtime.

## Build requirements

Normal CLI/TUI builds do **not** require desktop dependencies. The Wails GUI is
behind explicit build tags.

Linux desktop builds need Wails' GTK/WebKit stack. On Debian/Ubuntu-like
systems:

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
```

The repository uses WebKitGTK 4.1, so desktop builds use the `webkit2_41` tag.

## Run during development

```bash
make gui-run
```

This runs:

```bash
go run -tags 'wails production webkit2_41' . gui
```

The command ensures the Eigen daemon is running, opens a native Wails window,
and uses the daemon-backed GUI service. For a browser fallback/debug surface:

```bash
go run . gui --browser
```

## Build a desktop binary

```bash
make gui-desktop
./bin/eigen-gui gui
```

`make gui-desktop` writes `bin/eigen-gui` with Wails production tags.

## Current GUI surfaces

The GUI currently includes:

- session rail with live daemon sessions;
- new-session modal with workspace/model/permission inputs;
- active transcript/timeline;
- topbar permission/search/fast/run controls;
- context inspector with tokens, roots, tools, shells, goal, and workspace roots;
- composer auto-grow, Stop state, scroll anchoring, and jump-to-latest;
- persistent approval cards recovered from daemon `SessionState.pending`;
- rich tool execution cards with arguments/result sections;
- profile/personality editor backed by global `USER.md`.

## Validation checklist

Before shipping GUI changes, run:

```bash
node --check internal/gui/static/app.js
GOTMPDIR=/run/user/1000/eigen-gotmp go test ./...
GOTMPDIR=/run/user/1000/eigen-gotmp go build -tags 'wails production webkit2_41' .
```

Then launch the app in an isolated workspace and verify the changed surface
visually. The GUI should feel like a desktop workspace, not a terminal dump.
