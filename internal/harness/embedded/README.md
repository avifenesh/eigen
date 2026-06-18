# Bundled harness components

These directories vendor the source of three companion projects by the same
author (Avi Fenesh), embedded into the `eigen` binary so the computer-use,
workspace, and Chrome capabilities install without separate checkouts or
package downloads. They are **first-party** Eigen components, not third-party
code lifted from elsewhere.

| Directory | What it is | Language | License | Runtime needs |
|-----------|------------|----------|---------|---------------|
| `computer-use-linux/` | MCP server controlling the user's **real** Linux desktop (AT-SPI accessibility, multi-compositor window targeting, portal screenshots, `ydotool` input) | Rust | MIT (`computer-use-linux/LICENSE`) | `ydotool`, `gnome-screenshot`/xdg-desktop-portal, `gdbus`/`gsettings` (+ per-WM `xprop`/`i3-msg`/`hyprctl`) |
| `agent-workspace-linux/` | MCP server providing **isolated** X11 desktop sandboxes (Xvfb display, `bwrap` isolation, tmux terminals, GUI apps, browser control) | Rust | MIT (`agent-workspace-linux/LICENSE`) | `Xvfb`, `xauth`, `xdpyinfo`, `xdotool`, `tmux`, `bwrap`, `setsid`, `import`/`scrot` |
| `chrome-bridge/` | Connector-only bridge to the user's **real** logged-in Chrome (MV3 extension + native messaging host + broker + MCP tools); no chat UI | Node (stdlib only) | MIT (`chrome-bridge/LICENSE`) | `node` >= 18, Google Chrome/Chromium |

How they are used:

- `eigen harness install` builds the Rust components (requires `cargo`) and
  copies their release binaries to `~/.local/bin`.
- `eigen chrome install` materializes `chrome-bridge/` to `~/.eigen/chrome-bridge`
  and registers the native messaging host.
- At runtime Eigen auto-registers each capability as a built-in MCP server when
  its binary/script is present (see `internal/mcp/builtin.go`).

Build notes:

- `agent-workspace-linux` depends on Zed's `gpui`/`gpui_platform`, which are not
  published to crates.io. They are pinned to an explicit Zed git revision in
  `agent-workspace-linux/Cargo.toml` (and `Cargo.lock`) so public clones build
  reproducibly. Bump the `rev` deliberately and run `cargo update -p gpui` to
  refresh the lockfile.
- `chrome-bridge` has **no npm dependencies** — it uses only the Node standard
  library, so there is nothing to `npm install`.

Normal `go build`/`go test` of Eigen does **not** require Rust, Node, or any of
the desktop tools above; those are only touched when you explicitly install or
exercise a harness capability.
