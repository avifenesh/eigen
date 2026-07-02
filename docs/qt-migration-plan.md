# Eigen Desktop GUI: Wails → Qt Migration — Final Design (single-user edition)

**Constraint that shapes everything below:** eigen has exactly one user, and he is the developer. There is no fleet, no support queue, no stakeholder roadmap. Process that exists to protect strangers is deleted; engineering that protects *him* (his data, his logins, his twice-burned stale-binary trap) stays. Every deletion is recorded in §7 so future-Avi knows it was a choice.

## 1. Decision summary

**Backbone: sidecar-hybrid — settled, unchanged.** The existing 8,320-LOC `internal/gui.Bridge` compiles headless as a new `eigen guiserver` subcommand (Wails coupling is ~30 lines across 4 files); PySide6/QML is a pure view over a request-id socket protocol. **Zero Go domain logic rewritten, zero Python reimplementation of `internal/*`, zero daemon protocol changes in v1.** Daemon-first is deferred, not abandoned: new features land as daemon ops or bridge methods, never Svelte-only and never Qt-only logic. The pragmatic-port stance stays rejected (it hid ~17k LOC of internal packages and would fork the LLM client and the skill-scan security control).

What changed from the previous revision: the migration tail. Seven phases become three; the terminal dock is not ported at all; per-channel backpressure collapses to one policy; the parity gate, soak period, multi-client refusal, and `types.py` generator are gone. **Svelte feature work freezes now** — bugfix-only; features land Qt-first (bridge-side changes remain free, the dispatcher auto-exposes them).

## 2. Architecture

### Topology — three processes, two sockets

```
eigen daemon (unchanged)         ~/.eigen/daemon.sock
        ▲  existing bridge.go control conn + per-session pump conns
eigen guiserver (new subcommand) ~/.eigen/guiserver.sock  (mode 0600)
        ▲  TWO connections: RPC conn + events conn
eigen-qt (PySide6)               pure view
```

**guiserver** is the Bridge with four pieces of surgery:
1. An `Emitter` interface replaces the 20 `b.app.Event.Emit` call sites; Wails build injects the Wails emitter, guiserver injects a socket fan-out.
2. The **four** host-UI call sites (`newchat.go:72`, `google.go:46`, `builtins.go:70` dialogs + `Window.Current()`) go behind the `wails` tag; Qt provides file paths via `QFileDialog` and passes them as plain args. No fork-inventory document — the table in §3 *is* the inventory; just do the forks.
3. A reflect dispatcher exposes all 161 methods by name — new bridge methods auto-exposed forever.
4. `TasksAPIHandler` folds into RPC (two RPC methods replace 111 lines of HTTP plumbing and a `net/http` server inside guiserver — a net deletion).

guiserver compiles **tagless** (no webkitgtk), so it joins `make gate` — that is what makes contract enforcement real.

### Guiserver protocol

- **Two connections, deliberately — this survives every cut.** RPC conn carries id-multiplexed request/reply; events conn carries only pushes. Multi-MB `state` replies never queue token deltas or approvals behind them. The conn split is the part that actually kills head-of-line blocking; everything fancier was deleted.
- **One backpressure policy:** every events channel is a bounded queue; on overflow guiserver sends `{"event":"dropped","channel":...}` and the client refetches via RPC. Session channels keep the existing `seq` + state-resync discipline (already built). There is **no** credit window, no acks, no per-channel class hierarchy — the only lossless consumer (terminal bytes) no longer exists because the terminal isn't ported (§7).
- **Explicit line budget: 32 MB** — one constant, documented; Python reader configured accordingly.
- **`hello` handshake** returns build SHA + a hash of the method manifest. This is the single most user-serving item in the plan (the stale-inode trap has burned him twice; the reflect dispatcher would otherwise fail only at runtime with opaque errors). On mismatch Qt **auto-kills and respawns guiserver from the on-disk binary path and toasts once** — the usual fix is "re-exec the sibling binary," so do it. A blocking screen appears only if the respawned binary *still* mismatches (the binary on disk is genuinely stale → `make`).
- **Per-connection subscriptions** on the events conn: `{"sub":["session:<id>","stats","feed",...]}`. Per-connection scoping is also why multi-client refusal was deleted: N clients are already safe, and the loop-flock handles double-loops. **All connections are accepted.** A debug REPL script coexisting with Qt is a feature.
- **Contract enforcement, minimum viable:** a generated **golden manifest** (method names, arity, DTO JSON tags) + a gate test that fails when it's stale + the manifest hash in `hello`. That ~100-line test is the load-bearing piece — AI agents rename Go DTO fields freely, and under a reflect dispatcher a renamed JSON tag becomes a silent `null` in Qt. **`types.py` generation is deferred:** Python starts with dict payloads; the sole user sees a field mismatch instantly at runtime. Generate types later only if dict-wrangling actually hurts.

### Qt layer

```
gui-qt/eigenqt/
  rpc/        client (QThread socket reader, JSON decode OFF the GUI thread,
              queued signals in), dict payloads, guiserver spawn/supervise
  models/     TranscriptModel (16ms delta coalescing, dataChanged per row,
              never model reset), sessions/feed/tasks/board/diff/filetree
  markdown/   markdown-it-py token walk → typed block-list model;
              QSyntaxHighlighter code fences; math via matplotlib.mathtext → SVG,
              raw-LaTeX fallback
  qml/        Theme.qml (deepteal/nord/gruvbox by name), Rail, ~10 views at
              switchover (+7 trickled later), ~15 components
```

- **All socket reads and JSON parsing on a worker thread**; parsed payloads cross to the GUI thread via queued signals.
- View lifecycle mirrors `viewCache.svelte.ts` (active + recently-used live, others suspended) — the phase-16 lesson, kept.
- **No `terminal/` package. No pyte spike. No QtWebEngine contingency.** The Svelte terminal is 122 lines wrapping xterm.js but its Qt cost was the plan's largest concentrated risk (pyte spike, damage batching, bespoke lossless channel, WebEngine fallback). Replaced by an **"Open in terminal" button** that launches his real terminal in the session's sandbox/worktree cwd — guiserver already knows the path. PTYs/sandbox shells live in guiserver/daemon and keep working regardless. Revisit only behind evidence of actually missing a session-attached shell.
- **QML component collapse:** Button/Badge/Card/StatusDot/Segmented/Tabs/Skeleton/Sheet/EmptyState/Popover/Dropdown/Tooltip (~2,240 Svelte LOC) → QtQuick Controls 2 + one Theme.qml, ~400 QML total. VirtualList → native `ListView` (delete). Shortcuts → `Shortcut{}` (~30 lines). ToastHost → timed Popups (~60). Tooltip → attached `ToolTip.text` (~0). BrowserPanel → open-externally (delete).

### Coexistence: the single-writer rule — **non-negotiable, lands before day one of coexistence**

Svelte is frozen but the Wails binary stays his daily driver until Qt chat parity — so **both** Bridges run `feedLoop`/`gpuSampleLoop`/`healthLoop` (bridge.go:97–99) for weeks. This item survives every cut scenario because cutting it bites *him*, not a hypothetical fleet:

1. **Loop ownership flock** (`~/.eigen/gui-loops.lock`): one Bridge runs background loops (suggester LLM spend — real money — GPU sampling, notifications happen once); the other serves reads/RPC only.
2. **Atomic + locked writes:** `config.json` is a plain `os.WriteFile` today (config.go:185) — last-writer-wins between two GUIs. Memory notes are rename-atomic but read-modify-write unguarded — two consolidating Bridges = **lost notes in his persistent knowledge base** (the highest-severity coexistence bug). Codex `auth.json` (codex.go:382) unlocked double-refresh with rotating refresh tokens can invalidate his login. All three get temp+rename+flock. Feed files are already rename-atomic — untouched.

Both GUIs attaching the *same daemon session* is already safe (proven fan-out, any view may approve).

### Lifecycle

Qt spawns guiserver from a configured binary path (default: sibling of the `eigen` binary), polls `hello`, compares SHA, auto-respawns on mismatch (see above), restarts with backoff. **guiserver lingers 5 minutes after last client disconnect** — kept deliberately: a Qt crash not killing live PTYs, voice, or OAuth flows is exactly the "losing work time" failure that matters, and it costs ~20 lines and a timer. A guiserver crash does kill them — today's exact blast radius, accepted, with a "backend restarted" toast. Daemon spawn stays in Go: guiserver IS eigen, `ensureDaemon` re-exec unchanged.

Approval UX: answered via RPC; approval-resolved reconciliation (`transcript.svelte.ts:375-383`) ported into TranscriptModel; guiserver re-sends pending approvals on subscribe.

**One line-item that must not silently vanish:** the "new reply" desktop notification is **frontend-initiated** — `sessionReplyWatch.svelte.ts:38` calls `Bridge.NotifyChatReply` from the Svelte sessions poll; `session_notify.go` only fires when called. Port sessionReplyWatch into Qt or finished-turn notifications disappear and the daemon gets blamed. This is the sole survivor of the deleted parity inventory.

## 3. Domain serving table

Tiering is by observed personal use (git churn since March + what he actually opens), not by porting difficulty.

| Domain(s) | Served by | Qt tier | Notes |
|---|---|---|---|
| Sessions lifecycle, state, turn I/O, settings, compact/clear, sandbox/shells, event streaming, daemon stats (1–8) | guiserver → daemon, unchanged `bridge.go`/`pump.go` | **PORT-NOW** (Chat 3,605 / Home 1,058 / Sessions 371 / Live 526) | DTO layer normalizes the PascalCase `llm.Message` gotcha; Qt never parses the daemon protocol |
| Agents/tasks (27) | guiserver RPC (folded from HTTP) | **PORT-NOW** (Tasks 853) | cancel (`agent.RequestCancel`) must not live only in a fallback binary — he runs agent sessions daily |
| Feed, board, kanban, GitHub lanes (13–15) | guiserver, unchanged | PORT-LATER (Board 783) | suggester LLM stays in Go; loop-flock prevents double spend |
| Memory (16) | guiserver | PORT-LATER (1,134) | consult-frequency, not drive-frequency |
| Skills (19), Notes/Obsidian builtins (21) | guiserver | PORT-LATER (893 / 347) | Obsidian itself is the Notes fallback |
| Connectors/OAuth, Google (20, 22) | guiserver | PORT-LATER (938) | port when a token expires; **the re-auth path must stay reachable** (legacy binary or CLI) or a dead Google token strands him. Loopback server runs locally — remote-OAuth never arises |
| Config (31), Reviewers | guiserver | PORT-LATER (519 / 150) | Reviewers is an afternoon |
| Workflows, slash commands (10–11) | guiserver | with Chat | file discovery + daemon `input`, unchanged |
| Voice (12) | guiserver (STT/TTS subprocess) + Qt audio UI | with Chat tail | phases stream on events conn |
| Worktree diff/tree/read (26) | guiserver | with Chat | diff/files dock |
| New-chat picker (33) | Qt (`QFileDialog`) + guiserver recent-dirs | PORT-NOW | |
| Remote machines/sessions (9) | guiserver | **DON'T-PORT the view** | ssh-stdio pumps + `remote:` refs work regardless; the card-browsing view collapses to a remote-host entry in the session picker |
| Terminal (25) | guiserver PTY (unchanged, serves sandbox shells) | **DON'T-PORT** | "Open in terminal" button launches a real terminal in the session cwd |
| Crons (24) | `systemctl --user list-timers` / `crontab -e` | **DON'T-PORT** | the view literally wraps these |
| Observe (28) | `eigen daemon stats` | **DON'T-PORT** | its headline Svelte leak-HUD is moot in Qt |
| Routing (29) | legacy binary / CLI dump | **DON'T-PORT** | read-only catalog, lowest churn in the repo |
| Profile (USER.md + KPIs) | any editor | **DON'T-PORT** | edit USER.md directly |
| Plugins (18), Dreaming (17), Google dashboard (23) | legacy binary | **DON'T-PORT** | configure-once / timeline over data visible in Memory; **security scan stays in Go regardless** |
| Clipboard/image paste, drag-drop, notifications-display, theme | Qt native | — | the honest ~2% fork line — including `sessionReplyWatch` (see §2) |

The DON'T-PORT tail is served by keeping the frozen Wails build as **`eigen-gui-legacy`** — one Makefile line. `frontend/` source stays frozen (bugfix-only, then untouched) and is deleted when the tail views die of disuse or grow CLI replacements.

## 4. Migration phases — three, not seven

| # | Phase | Content | Exit criteria | Status |
|---|---|---|---|---|
| A | **Go surgery + contract + vertical slice** (old P0+P1 — genuinely sequenced risk) | Emitter interface; wails-tag fork of 4 host-UI sites; reflect dispatcher; guiserver subcommand; two-conn protocol + per-conn subscriptions + one bounded-queue policy; golden-manifest gate test + `hello` SHA/manifest hash + auto-respawn; **flock + atomic/locked writes (config, memory RMW, codex auth) — MUST land before the first day both GUIs run**; TasksAPI fold; Qt shell with sessions list + one chat pane (attach, replay+state seed, streaming deltas, input/steer, interrupt, approval allow/deny); the one remaining spike: 8 MB `state` decode off-thread | guiserver in `make gate`; manifest test red on a deliberate DTO rename; Qt and Wails attached to the **same live session**, typing in one mirrors in the other, approval answered from Qt; UI never freezes during an 8 MB state load | ✓ **COMPLETE** |
| B | **Port by annoyance** (old P2–P5; the easy/medium/heavy tiering was a roadmap for stakeholders who don't exist) | Chat to daily-usable first (markdown pipeline, tool cards, highlighting, composer + image paste, slash commands, session settings, diff/files dock, sessionReplyWatch) — **start daily-driving Qt the day chat is usable**; then Home, Sessions, Live, Tasks (the switchover set); then trickle PORT-LATER views in whatever order actually itches. Plain TODO list, no percentages | He lives in Qt; PORT-NOW list empty; missing-view pain routed to `eigen-gui-legacy` | ✓ **COMPLETE** — all 12 views ported: Chat, Home, Sessions, Live, Tasks, Board, Skills, Notes, Config, Reviewers, Connectors, Memory. Each verified with pytest + offscreen launch + screenshot. Qt app is daily-drivable. |
| C | **Flip + delete** (old P6+P7) | Flip `eigen-gui.desktop` Exec to the Qt launcher. No soak window — he IS the soak; fallback is a one-line `Exec=` edit for the first days (don't delete `frontend/` the same week as the flip). Keep `eigen-gui-legacy` building for the DON'T-PORT tail; delete `frontend/` + wails tags + gui-phase workflow when he notices he hasn't launched legacy in a while | One primary GUI; both gates green; legacy binary is a museum piece until it isn't built at all | **READY** — requires: (1) daily-drive Qt for a few days to confirm stability, (2) edit `~/.local/share/applications/eigen-gui.desktop` Exec line to point to `eigen-qt` launcher, (3) keep `eigen-gui-legacy` available for DON'T-PORT views (Crons/Observe/Routing/Plugins/Dreaming/Profile). |

Standing rules during B: **Svelte is feature-frozen from today** (bugfix-only; bridge-side changes free — the dispatcher exposes them to both GUIs, the manifest test catches breaks). New features land as daemon ops or bridge methods, Qt-first UI.

**Effort:** ~1.5k Go touched (surgery + protocol + locks), ~4k Python (rpc/models/markdown — terminal package's ~1k is gone), **~7k QML at switchover** (~6.9k port-now Svelte views + ~4.6k real components with ~3.4k of primitives collapsing to ~500), plus ~3.5k QML trickled later — **~12.5k new lines to switchover, ~16k ceiling** (vs. the old plan's ~20k), ~9 fewer views, and the plan's largest concentrated risk (terminal rendering) deleted outright. Risk lives almost entirely in Phase A.

## 5. Critical findings: resolutions

**Resolved by design (unchanged from the losing proposals):** lossy daemon fan-out, daemon-restart blast radius, remote-OAuth loopback, 4 MB request cap for new op families, daemon kitchen-sink — moot, no domain moves into the daemon (the 4 MB image-input cap exists today under Wails; inherited, not a regression). Python reimplementation criticals — moot, zero Go logic reimplemented.

**Resolved by explicit mechanism:**
1. **Dual-Bridge dual writers** → loop-ownership flock + atomic locked writes, landing in Phase A **before** coexistence opens. Highest-severity self-harm risk in the plan; the one item that survives every cut.
2. **Stale-inode / version skew (his twice-burned trap)** → `hello` SHA + manifest-hash handshake with auto-respawn-then-block. ~20 lines, cheapest insurance in the plan. guiserver↔daemon skew stays today's stats-version check; the Qt launcher reuses the staleness-checking wrapper so the desktop icon rebuilds both artifacts.
3. **Reflect dispatcher = silent runtime breaks under agent-driven renames** → golden-manifest gate test (names + JSON tags). The expensive half (`types.py`) is deferred; the catching half is kept.
4. **Head-of-line blocking / main-thread freezes** → two-conn split + worker-thread reader + off-main-thread JSON decode + the 8 MB decode spike in Phase A. The per-channel policy zoo is gone because its only lossless customer (terminal) is gone.
5. **guiserver crash blackout** → supervision + 5-min linger + restart-with-backoff + toast. Blast radius identical to today's Wails process — accepted.
6. **Privileged 161-method socket** → 0600, same-user only, no network transport. Multi-client is *allowed*, not refused — per-connection subscriptions already scope it.
7. **Entrenching the anti-daemon architecture** → accepted deliberately; graduation is a v2 program, one domain at a time, against a stable Qt client.

## 6. Explicitly out of v1 scope

- **Daemon protocol changes** of any kind — v2, post-switchover.
- **KaTeX-parity math** — mathtext subset + raw-LaTeX fallback.
- **In-app browser panel** — open externally.
- **QtWebEngine, in any role** — its only justification (terminal fallback) is gone.
- **Windows/macOS packaging**; pip polish beyond venv + launcher script.
- **Typed `types.py`** — dicts until dict-wrangling demonstrably hurts.
- **Feed/memory/skills for TUI** — guiserver-shaped until v2 graduation.

## 7. What we deliberately do NOT do (single-user cuts)

Each of these was in the previous plan. They are choices, not oversights.

- **No 2-week soak.** He is the fleet; if Qt annoys him the fallback is a 30-second `Exec=` edit. Only guard kept: don't delete `frontend/` the same week as the flip.
- **No ≥95% parity inventory gate.** A percentage over an inventory document is process theater; a plain TODO list plus "a view nobody misses is already consciously dropped." Sole survivor: the sessionReplyWatch checklist line (§2), because that break would be *silent*.
- **No Svelte feature work from today.** A frontend with a delete date gets bugfixes only.
- **No 7-phase roadmap.** Easy/medium/heavy tiering serves stakeholders who don't exist → 3 phases (§4).
- **No per-channel backpressure taxonomy / credit window.** One bounded-queue-plus-refetch policy; the lossless customer (terminal) was cut. The two-conn split — the part that actually works — stays.
- **No "rebuild guiserver" blocking screen on first mismatch.** Auto-respawn the sibling binary and toast; block only if the on-disk binary is itself stale. The handshake stays (§5.2).
- **No multi-client refusal.** Per-connection subscriptions make N clients safe; the REPL script next to Qt is a feature, and the flock already prevents double loops.
- **No terminal port, full stop.** No pyte spike, no `terminal/` package, no damage-batching, no WebEngine fallback. "Open in terminal" at the session cwd; his real terminal is 2 cm away. Revisit behind evidence only.
- **No ported Crons/Observe/Routing/Machines-view/Profile/Plugins/Dreaming.** systemctl/crontab, `eigen daemon stats`, a CLI dump, the session-picker remote entry, any text editor, and `eigen-gui-legacy` respectively cover a population of one.
- **No `types.py` generator (yet).** The golden-manifest test catches agent renames; typed dataclasses are deferred until dicts hurt.
- **No per-frontend fork-inventory document.** The §3 table is the inventory; do the forks.

**Kept without apology,** because they protect him, not a fleet: flock + atomic writes (his memory notes and Codex login), SHA/manifest handshake (his twice-burned trap), manifest gate test (his agents rename fields), 5-min linger (his live PTYs), TasksAPI fold (net code deletion), two-conn split, 32 MB budget constant, 0600 socket, Connectors re-auth reachability.

## 8. First week, concretely

1. **Day 1–2 — Emitter seam + tag fork.** `type Emitter interface{ Emit(name string, data any) }` in `internal/gui`; replace the 20 `b.app.Event.Emit` sites; move 3 dialog sites + `Window.Current()` behind `//go:build wails`; Wails build green, `internal/gui` compiles tagless for the first time.
2. **Day 2–3 — guiserver subcommand.** Socket server (0600), RPC conn with id multiplexing, reflect dispatcher, `hello` with build SHA + manifest hash; startup mirrors `main_gui_wails.go` (suggester injection, project dirs, loops).
3. **Day 3 — events conn.** Per-connection subscription registry, one bounded-queue channel type with `dropped` notice, socket Emitter fan-out; fold TasksAPI into RPC.
4. **Day 4 — coexistence safety (blocks everything downstream).** flock loop-ownership; temp+rename+flock in `internal/config` (config.go:185), memory read-modify-write paths, Codex `auth.json` refresh (codex.go:382). **No day where both GUIs run precedes this day.**
5. **Day 5 — contract tooling.** `go:generate` golden-manifest emitter (method names + JSON tags); gate test asserting freshness; guiserver packages into `make gate`. Also: Svelte freeze announced to himself — feature branches close.
6. **Day 5–6 — proof script.** ~100-line Python script: connect both sockets, `hello`, `Sessions`, subscribe to a live session, print streaming deltas, send input, answer an approval — while the Wails GUI displays the same session. (This script stays alive afterward — multi-client is allowed now.)
7. **Day 6–7 — the one spike.** 8 MB JSON decode on a QThread with queued-signal handoff; measure GUI-thread stall. (The pyte spike is deleted with the terminal.)

Key files: `/home/avifenesh/projects/eigen/internal/gui/bridge.go` (`:30` app field, `:97–99` loops, `:164` emit, `:173` control), `/home/avifenesh/projects/eigen/internal/gui/pump.go`, `/home/avifenesh/projects/eigen/internal/gui/newchat.go:72`, `/home/avifenesh/projects/eigen/internal/gui/google.go:46`, `/home/avifenesh/projects/eigen/internal/gui/builtins.go:70`, `/home/avifenesh/projects/eigen/internal/gui/session_notify.go`, `/home/avifenesh/projects/eigen/frontend/src/lib/stores/sessionReplyWatch.svelte.ts:38`, `/home/avifenesh/projects/eigen/main_gui_wails.go`, `/home/avifenesh/projects/eigen/internal/config/config.go:185`, `/home/avifenesh/projects/eigen/internal/llm/codex.go:382`, `/home/avifenesh/projects/eigen/internal/gui/tasks_api.go`, `/home/avifenesh/projects/eigen/Makefile`.
