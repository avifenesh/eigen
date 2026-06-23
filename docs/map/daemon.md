# daemon/ — unix-socket server, client, sessions

> `internal/daemon` is the long-lived session host — the real core of the eigen app. It owns agent
> sessions (each a whole chat rooted at its own directory), keeps them running whether or not any
> window is attached, and serves "views" (terminal TUI, Wails GUI bridge, Telegram bridge, remote
> SSH/WebSocket clients) over a Unix socket using line-delimited JSON. The package is transport +
> lifecycle only: it never builds providers/tools/MCP itself — package `main` injects a `Builder`
> (and a `ModelSwitcher`, titler, notifier) so the daemon stays decoupled from how agents are wired.
> Sessions are durable: each streams its transcript + a sidecar meta file to disk after every
> message, so killing the daemon loses nothing — on restart sessions come back as cold rows under
> the same id and rehydrate (rebuild the agent) only when a view or input needs them. The same
> `Client` works over any `io.ReadWriteCloser`, which is how remote transports reuse the protocol.

## Files

### internal/daemon/client.go
- **Role:** The view-side connection to the daemon: one socket = one `Client`, with request/reply
  serialization and an attach-stream event loop.
- **Key symbols:**
  - `Client` (type) — holds the conn, a `replies` channel (request answers, in order), an `events`
    channel (attach stream), and the `onEvent` handler.
  - `Dial(sockPath)` — connects to the unix socket; error message tells the caller to start a daemon.
  - `DialConn(conn)` — wraps any already-connected `io.ReadWriteCloser` (ssh stdio, WebSocket adapter)
    as a `Client`; this is the transport-agnostic entry used by `internal/remote`.
  - `readLoop` / `eventLoop` (unexported) — `readLoop` routes events vs replies off the wire;
    `eventLoop` delivers events to the handler off the read loop so a handler issuing a request can't
    deadlock the connection.
  - `request` / `requestWithin(req, d)` — send one request, wait for its reply; `reqMu` keeps exactly
    one request in flight (replies carry no id, so pairing relies on serialization); drains stale
    replies from a prior timeout first.
  - `requestTimeoutFor(op)` / `envTimeout` / `maxDur` — per-op deadlines read lazily from
    `EIGEN_DAEMON_TIMEOUT` (compact ≥6m, new ≥2m, set ≥90s, else 30s).
  - Control ops (thin wrappers over `request`): `Ping`, `Stats`, `List`, `New`, `NewSession`,
    `Attach`, `Input`, `SteerInput`, `Interrupt`, `Remove`, `Prune`, `Approve`, `State`, `SetPerm`,
    `SetGoal`, `SetTitle`, `AddDir`, `KillShell`, `DetachBash`, `Compact`, `Clear`, `ResetTo`,
    `Resend`, `SetModel`, `SetEffort`, `SetSearch`, `SetFast`, `Done`, `Close`.
- **Depends on:** `internal/llm` (Image/Message types in request payloads).
- **Used by / entrypoint:** entrypoint for all clients — `internal/chat/remote.go`, `internal/gui`
  (bridge/pump/feed), `internal/telegram`, `internal/app/live.go`, `internal/remote/dial.go`,
  `daemon.go`, `main.go`, `remote_session.go`.

### internal/daemon/host.go
- **Role:** The in-memory core: owns all live `Session`s, the session map + id sequence, persistence
  wiring, hydration/unload lifecycle, the daemon `stats` snapshot, and restore-on-startup.
- **Key symbols:**
  - `Host` (type) — guards `sessions map[string]*Session`, `seq`, `persistDir`, and the injected
    hooks `builder`/`switchModel`/`titler`/`notify`/`bgCount`.
  - `NewHost()` (no persistence — tests) / `NewPersistentHost(dir)` (persists under dir).
  - Injection setters: `SetTitler`, `SetNotifier`, `SetModelSwitcher`, `SetBuilder`, `SetBgCount`.
  - `ModelSwitcher` (type) — `func(dir, modelID) (provider, compactor, budget, err)` for live /model.
  - `Add(dir, model, agent)` — registers a freshly built agent as a hosted session (assigns `s<seq>`).
  - `enablePersist(s)` — installs the agent `Persist` hook (continuous transcript autosave), the
    `onAttach` (save meta) and `onInactive` (unload) hooks.
  - `hydrateLocked(s)` / `Hydrate(id)` — rebuild a cold session's agent from disk + meta (resume or
    new), re-apply perm/goal/added-roots; `hydrateLocked` requires the caller hold `loadMu`.
  - `UnloadIfInactive(id)` — drop heavyweight agent/MCP/LSP for an idle, view-less session, keeping
    only cold metadata; double-checks running/subs/shells around a flush.
  - `saveSessionMeta(s)` — snapshot resurrect state (dir/model/title/perm/goal/added-roots/last-
    attached) to the sidecar.
  - `Restore(build)` — on startup, load persisted metas as cold rows (no providers built), backfill
    titles, advance `seq`; returns count.
  - `Stats()` — build a `DaemonStats` (goroutines, heap/RSS, GC, session/view/running counts, cumulative
    token usage, build identity).
  - `Get` / `isCurrent` / `List` (newest-first) / `Count` / `AnyRunning`.
  - `Shutdown()` — lossless daemon stop: flush, interrupt, wait-idle, flush again, then close each
    session's resources WITHOUT deleting persisted state.
  - `Remove(id)` (user delete: stop + delete durable files) / `PruneEmpty()` (drop conversation-less
    sessions, in memory + on disk).
  - `daemonBuildIdentity` / `sessionHasRunningShells` (unexported helpers).
- **Depends on:** `internal/agent` (Agent/Session/Permission), `internal/llm` (Provider/Compactor/
  Message/Version), `internal/transcript` (Save/Load).
- **Used by / entrypoint:** constructed in `daemon.go` (`NewPersistentHost` + setters + `Restore`),
  exposed to clients by `server.go`. `AnyRunning` is read by the nightly dreamer in `daemon.go`.

### internal/daemon/server.go
- **Role:** Exposes a `Host` over a unix socket: accepts connections (one per view), parses requests,
  dispatches each op, and streams a session's events to an attached view.
- **Key symbols:**
  - `Server` (type) — listener + host + builder + socket path + connection WaitGroup.
  - `SocketPath()` — `~/.eigen/daemon[-instance].sock`.
  - `Listen(sockPath, host, build)` — install the builder, remove a stale socket (after probing it),
    bind; a second bind failing is treated as "already running".
  - `Serve()` — accept loop, one goroutine per connection.
  - `Close()` — stop the listener, wait for handlers, remove the socket.
  - `handle(conn)` (unexported) — the per-connection request loop. Panic-recovers per connection,
    serializes writes, and implements every op: `ping`, `stats`, `list`, `new`, `remove`, `prune`,
    `state`, `set` (perm/goal/title/effort/search/fast/model), `add-dir`, `kill-shell`, `detach-bash`,
    `clear`, `resend`, `compact`, `approve`, `interrupt`, `input` (send-or-steer), `attach`.
  - `withLiveSession` (closure inside `handle`) — get → check still current → hydrate-under-loadMu →
    run the op against a live session.
- **Depends on:** `internal/agent` (Permission for `set`/`new`).
- **Used by / entrypoint:** entrypoint reached from `daemon.go` (`Listen` + `Serve`); the dispatch
  switch is the server side of every `Client` op.

### internal/daemon/session.go
- **Role:** One hosted chat — an `agent.Session` plus the bookkeeping to multiplex many views onto
  it: event fan-out + bounded replay buffer, status, turn lifecycle, gated approvals, cold metadata.
- **Key symbols:**
  - `Status` consts (`StatusIdle/Working/Approval/Error`); `SessionInfo` (rail listing DTO).
  - `Session` (type) — agent + sess, status/title/updated, cold metadata (turns/fallbackTitle/
    coldPerm/coldGoal/coldRoots), subs/events for fan-out, cumulative token counters, approvals map,
    and host hooks `onAttach`/`onInactive`/`onClose`/`notify`.
  - `newSession` / `newColdSession` / `bindAgent` — construction; `bindAgent` wires `OnEvent` →
    `dispatch` (composing the agent's `EventWrap` for observability) and installs the approver.
  - `dispatch(e)` — record event, update status, accumulate token usage, bound the replay buffer
    (`maxReplayEvents`), fan out to subs, and trigger a background-done wake.
  - `wakeForBg(id)` / `wakeForGoalStart` / `wakeForGoalContinue` / `goalJudgeAvailable` — autonomous
    self-continuation: collect a finished bg task or keep working an unachieved goal with no TUI.
  - `attach()` — register a view (replay + live channel + detach func that fires `onInactive`).
  - `send` / `steer` / `resend` — start a new turn / inject mid-turn / retry last turn.
  - `runTurn` / `finishTurn` — execute a turn body with panic recovery; on finish clear running,
    emit a terminal note, fire the backgrounded-turn desktop notification, drop the replay buffer,
    continue any active goal, and let the host unload if idle+detached.
  - `interrupt` / `waitUntilIdle` / `flush` — cancel in-flight turn / bounded wait for unwind /
    persist current transcript (used by `Host.Shutdown`).
  - `info` / `state` — listing snapshot / full `SessionState` snapshot (history + model/provider/
    perm/goal/effort/search/fast/tools/roots/shells/pending).
  - `installApprover` / `answer` / `pendingList` + `pendingApproval` + `approvalTimeout` — gated-tool
    approvals broadcast to views; fail-closed deny after 10m if no view answers.
  - `setPerm/setGoal/addDir/killShell/detachBash/setEffort/setSearch/setFast/compact/resume/clear/
    setModel` — per-op session mutators (forward to the agent / provider capability interfaces).
  - `SetTitle` (exported) — set display title; `maxReplayEvents`, `backgroundedNotifyMin` (vars/consts).
- **Depends on:** `internal/agent` (Agent/Session/Event/Permission/GoalStart-ContinueInstruction),
  `internal/llm` (Image/Message/RoleUser + provider capability interfaces EffortSetter/Searcher/FastModer).
- **Used by / entrypoint:** driven entirely by `server.go`'s op handlers and `host.go`'s lifecycle.

### internal/daemon/protocol.go
- **Role:** The wire contract — request/response structs, the `Builder` injection type, the wire
  event/state DTOs, and the event-kind/encode helpers shared by client and server.
- **Key symbols:**
  - `Builder` (type) — `func(dir, model) (*agent.Agent, func(), error)` injected by `main`.
  - `Request` / `Response` — line-JSON command + reply (Type discriminates the payload).
  - `DaemonStats` — resource-health snapshot DTO (uptime/goroutines/heap/RSS/GC/counts/token usage/
    build identity).
  - `SessionState` — full remote-UI snapshot DTO (messages + model/perm/goal/budget/tools/roots/
    shells/pending).
  - `ApprovalInfo` / `ShellInfo` / `ToolInfo` — sub-DTOs mirroring chat-layer types over the wire.
  - `WireEvent` + `wireEvent(e)` + `eventKindName(k)` — flatten `agent.Event` (kind as string) for
    the socket.
  - `encode(v)` — marshal a value to one JSON line.
- **Depends on:** `internal/agent` (Agent/Event/EventKind), `internal/llm` (Image/Message).
- **Used by / entrypoint:** the shared vocabulary of `client.go` and `server.go`; DTOs also consumed
  by `internal/chat/remote.go`, `internal/gui`, `internal/telegram`, `internal/app`.

### internal/daemon/host.go ↔ instance.go
### internal/daemon/instance.go
- **Role:** Instance isolation — lets a separate named daemon (own socket/pid/log/sessions) run
  alongside the production default, so developing eigen never touches real sessions.
- **Key symbols:**
  - `validInstance` (regexp) — short, separator-free, no-traversal names.
  - `ValidInstanceName(name)` — empty (default) or matches the regexp.
  - `SetInstance(name)` — fix the active instance for the process (false on invalid; never silently
    falls back to production).
  - `ResolveInstance(flagVal)` — flag wins, then `$EIGEN_INSTANCE`.
  - `Instance()` — active name (falls back to env when `SetInstance` was never called).
  - `IsDefaultInstance()` — guards the destructive default-daemon rebuild.
  - `suffix()` (unexported) — `""` for default else `-<name>`, appended to every runtime path.
- **Depends on:** stdlib only (os/regexp/sync).
- **Used by / entrypoint:** `Set/Resolve/Instance/IsDefaultInstance` called from `main.go` +
  `daemon.go`; `suffix()` drives `SocketPath`, `PIDPath`, `SessionsDir`.

### internal/daemon/persist.go
- **Role:** Durable-session storage — the sidecar meta format, the sessions dir, and load/list/
  prune/delete of persisted sessions (works whether or not the daemon is running).
- **Key symbols:**
  - `SessionsDir()` — `~/.eigen/daemon[-instance]/sessions`.
  - `persistMeta` (type) + `transcriptPath`/`metaPath` (paths) + `saveMeta` (write sidecar).
  - `loadPersisted(dir)` / `persisted` (type) — scan dir, return every resurrectable (meta+history),
    ordered by numeric id.
  - `idNum(id)` — parse `s12` → 12; `snippet(s, n)` — first line truncated to n runes.
  - `removePersisted(dir, id)` — delete transcript + .bak backups + meta.
  - `PersistedInfo` (type) + `ListPersisted()` — list durable sessions for the picker (title falls
    back to first-user-message snippet; "last used by me" ordering).
  - `PrunePersisted()` / `DeletePersisted(id)` / `PersistedTranscriptPath(id)` — disk-side operations
    used when no daemon is running.
- **Depends on:** `internal/llm` (Message), `internal/transcript` (Load).
- **Used by / entrypoint:** `host.go` (saveMeta/loadPersisted/removePersisted/transcriptPath);
  exported listing/prune/delete called by `daemon.go` + `internal/app` (data.go/sessions.go).

### internal/daemon/pid.go
- **Role:** Daemon PID-file management for `eigen daemon --start/--stop/--status` and stale-daemon
  detection.
- **Key symbols:**
  - `PIDPath()` — `~/.eigen/daemon[-instance].pid`.
  - `WritePID` / `RemovePID` — record / clear ownership.
  - `RunningPID(path)` — live daemon pid or 0 (a pid pointing at a dead process counts as not-running).
  - `processAlive(pid)` (unexported) — signal-0 liveness probe.
  - `Stop(pidPath)` — SIGTERM a running daemon, return the pid stopped.
- **Depends on:** stdlib only (os/syscall).
- **Used by / entrypoint:** all called from `daemon.go` (the `eigen daemon` subcommand lifecycle).

### internal/daemon/rss.go
- **Role:** Linux resident-set-size reader for the `stats` op.
- **Key symbols:** `currentRSS()` — reads `/proc/self/statm` resident pages × page size; 0 on
  non-Linux / unreadable.
- **Depends on:** stdlib only (os/strconv/strings).
- **Used by / entrypoint:** `host.go` `Stats()`.

## Cross-links
- **internal/agent** — the daemon hosts `*agent.Agent`/`*agent.Session`; `Builder`/`ModelSwitcher`
  build them, sessions drive turns/approvals/goals/shells through the agent API.
- **internal/llm** — provider/compactor types, message/image DTOs, and the optional provider
  capability interfaces (EffortSetter, Searcher, FastModer) probed in `Session.state`/setters.
- **internal/transcript** — durable transcript Save/Load behind the persist hook and restore path.
- **internal/chat (remote.go)** — `chat.Remote` is the primary TUI view: dials the daemon, attaches,
  and renders the event stream + `SessionState`.
- **internal/gui** — the Wails bridge/pump/feed dial the daemon `Client` for the desktop GUI.
- **internal/telegram** — the Telegram bridge is another `Client` (NewSession/SteerInput).
- **internal/remote (dial.go)** — wraps ssh/WebSocket streams via `DialConn` to reuse the protocol.
- **internal/app** — the session-picker app reads persisted sessions (`ListPersisted`/
  `DeletePersisted`/`PersistedTranscriptPath`) and dials the live daemon.
- **package main (`main.go`, `daemon.go`, `remote_session.go`)** — owns the `eigen daemon` lifecycle:
  constructs the host, injects Builder/ModelSwitcher/titler/notifier, and exposes the CLI subcommand.

## Dead-code suspects
- `Host.Count()` (host.go:646) — **high**: grepped the whole repo; zero callers (production or test).
  The only `Count` references are unrelated (`RunningCount`, `SetBgCount` comment, a test
  `callCount`). Looks orphaned.
- `Host.SetBgCount()` (host.go:140) — **high**: setter has zero callers anywhere; the `bgCount` field
  it sets stays nil, so the `stats` op's `BgTasks` is never populated by a live reporter. The field
  plumbing exists but the injector is never wired.
- `var _ = llm.RoleUser` (session.go:490) — **high**: a blank import-guard with comment "keep llm
  imported," but `llm` is used extensively in the same file (`llm.Image`, `llm.Message`,
  `llm.RoleUser` at line 460, the provider capability interfaces). The guard is redundant — removing
  it cannot break the build.
- `Host.Hydrate(id)` (host.go:355) — **low**: exported but only test callers (`persist_test.go`); its
  own doc-comment says "for tests and low-risk control paths," so this is likely a deliberate
  test/low-risk helper rather than accidental dead code — not flagged with confidence.
