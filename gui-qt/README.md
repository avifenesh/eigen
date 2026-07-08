# Eigen Qt Desktop GUI

PySide6/QML desktop shell for Eigen — a pure view over the `eigen guiserver` socket protocol. Zero Go domain logic reimplemented; all state, LLM interaction, daemon control, and business logic lives in the guiserver (which is the existing `internal/gui.Bridge` compiled headless with a socket emitter).

## Architecture

**Three processes, two sockets:**

```
eigen daemon (unchanged)         ~/.eigen/daemon.sock
        ▲  existing bridge.go control conn + per-session pump conns
eigen guiserver (new subcommand) ~/.eigen/guiserver.sock  (mode 0600)
        ▲  TWO connections: RPC conn + events conn
eigen-qt (PySide6)               pure view
```

- **guiserver**: The `internal/gui.Bridge` with Wails coupling removed (~30 lines), Emitter abstracted, reflect dispatcher exposing all 161+ bridge methods by name, two-socket protocol (RPC + events), per-connection subscriptions, one bounded-queue backpressure policy. Compiles **tagless** (no webkitgtk), joins `make gate`. Spawned/supervised by Qt; lingers 5 minutes after last client disconnect.
- **Qt layer**: PySide6 + QML. Socket I/O + JSON parsing on worker thread; parsed payloads cross to GUI thread via queued signals. Models (transcript/sessions/feed/tasks/board/diff/filetree/etc.) drive native QML views. Markdown pipeline: markdown-it-py token walk → typed block-list model; Pygments code fences; math via raw-LaTeX fallback (KaTeX-parity deferred). View lifecycle mirrors the Svelte `viewCache` (active + recently-used live, others suspended).

## Layout

```
gui-qt/
  eigenqt/
    rpc/        GuiserverClient (socket I/O, worker-thread JSON decode,
                guiserver spawn/supervise, hello handshake + auto-respawn)
    models/     TranscriptModel (16ms delta coalescing, per-row dataChanged),
                SessionsModel, FeedModel, TasksModel, BoardModel, etc.
    markdown/   markdown-it-py walk → typed block-list; QSyntaxHighlighter
    qml/        Theme.qml (deepteal/nord/gruvbox), Rail, 12 views, ~15 components
  main.py       Qt app entry (QGuiApplication, QQmlApplicationEngine, theme init)
  run.sh        Launcher: bootstrap venv + pip install -r requirements.txt, exec main.py
  requirements.txt  PySide6, pytest, markdown-it-py, pygments
  tests/        Per-view pytest + offscreen launch + screenshot capture
  screenshots/  View verification images (gitignored)
```

## Run instructions

**Via desktop icon** (preferred):

```bash
~/.local/bin/eigen-qt
```

Install or refresh the launcher and desktop entries with:

```bash
./install-desktop.sh
```

The installer writes `~/.local/bin/eigen-qt`, `~/.local/share/applications/eigen-qt.desktop`, and the primary `~/.local/share/applications/eigen-gui.desktop`. The launcher checks for stale Go source, rebuilds `bin/eigen` (which includes guiserver) if needed, bootstraps the Qt venv, then launches `main.py`.

**Directly** (for development):

```bash
cd gui-qt
./run.sh
```

Or with an active venv:

```bash
source .venv/bin/activate
python3 main.py
```

**Via desktop entry:**

The dedicated desktop icon is at `~/.local/share/applications/eigen-qt.desktop` (Name: "Eigen (Qt)"). The primary `eigen-gui.desktop` entry also points at the Qt launcher; the Wails GUI remains available as `bin/eigen-gui-legacy` for fallback.

## Footguns (all bit us already — violating these wastes a round)

1. **NEVER declare `property var X: null` in QML matching a Python context-property name** — it shadows it app-wide. Bind through readonly aliases.
2. **In delegates always `model.X`, never bare `X`.** In component instantiation `Foo { x: x }` self-binds — use root-scoped alias.
3. **Python `@property` is INVISIBLE to QML** — use `@Property(type, notify=sig)`. Role class attrs (`Model.SomeRole`) are also invisible from QML — hardcode the int with a comment or expose counts as Properties.
4. **`d.get("key", default)` returns `None` for explicit JSON `null`** — use `(d.get("key") or default)` everywhere a value can be `null`.
5. **`font.pixelSize` must be `int`.** No inline `component` defs below other members — put them right after the root's properties.
6. **QML function-call bindings are NOT reactive to model resets** — expose reactive properties or `Connections` on model signals.
7. **Block comments containing `*/` patterns (like glob strings) self-close** — avoid in QML comments.

## Testing

Each view MUST end with: pytest for its model logic green + offscreen launch (`QT_QPA_PLATFORM=offscreen`, timeout 12s) with ZERO new stderr errors + a `grabWindow` screenshot saved to `gui-qt/screenshots/`.

Run all tests:

```bash
pytest -v
```

Run a specific view test:

```bash
pytest test_chat_parity.py -v
```

Screenshot tests (require `DISPLAY=:0` or a real X11/Wayland session):

```bash
DISPLAY=:0 ./take_live_screenshot.sh
```

## Bridge method signatures

The reflect dispatcher in guiserver exposes all bridge methods by name. The canonical method inventory is in `/home/avifenesh/projects/eigen/internal/gui/bridge.manifest.json`. **READ IT — do not guess signatures.** The manifest hash is part of the `hello` handshake; on mismatch Qt auto-kills and respawns guiserver.

## Coexistence with Wails GUI

Both GUIs attach to the **same daemon** (proven fan-out; any view may approve tools). A **loop-ownership flock** (`~/.eigen/gui-loops.lock`) ensures only one Bridge runs background loops (suggester LLM, GPU sampling, notifications). All writes (config, memory notes, Codex auth) are temp+rename+flock. The frozen Wails build is `bin/eigen-gui-legacy` (`make gui-legacy`) and serves DON'T-PORT views (Crons/Observe/Routing/Plugins/Dreaming/Profile).

## Migration status

- **Phase A (Go surgery + vertical slice):** ✓ COMPLETE
- **Phase B (port by annoyance):** ✓ COMPLETE — all 12 views ported (Chat, Home, Sessions, Live, Tasks, Board, Skills, Notes, Config, Reviewers, Connectors, Memory)
- **Phase C (flip + delete):** FLIPPED — primary desktop entry points at the Qt launcher; keep `bin/eigen-gui-legacy` for fallback until the legacy frontend is removed

See `/home/avifenesh/projects/eigen/docs/qt-migration-plan.md` for full architecture and decision log.
