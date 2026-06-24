# GUI stores, bridge, router & lib

> The TypeScript "glue layer" of the Eigen desktop GUI (Wails v3 + Svelte 5). It owns the seam
> between the generated Go→JS Wails bindings and the Svelte views: a single typed `Bridge` facade
> over the bindings, hand-authored DTO types that mirror `gui/dto.go`, a thin typed wrapper over the
> Wails event runtime, a hash router, and a set of Svelte-5-runes (`$state`) singleton stores
> (daemon health/stats, session list, proactive feed, toasts, a shared 1 Hz clock) plus a
> per-session streaming `transcript` factory. `App.svelte` mounts the shell, starts the long-lived
> stores on the root lifecycle, and routes between view components. Everything here runs in the
> webview; the actual work happens in the Go daemon reached through the Bridge and the event stream.

## Files

### internal/gui/frontend/src/main.ts
- **Role:** App entry — imports global CSS and mounts the root Svelte component into `#app`.
- **Key symbols:**
  - default export — `mount(App, { target })` after asserting the `#app` element exists (throws if missing).
- **Depends on:** `svelte` (`mount`), `./App.svelte`, `./styles/*` (tokens, fonts, base CSS).
- **Used by / entrypoint:** entrypoint — the Vite/Wails frontend bundle boots here; nothing imports it.

### internal/gui/frontend/src/App.svelte
- **Role:** Root shell — fixed chrome (Rail, TopBar, ToastHost, CommandPalette, Shortcuts) plus the routed outlet; owns app-lifetime store startup.
- **Key symbols:**
  - `onMount` block — calls `daemon.start()` and `feed.start()` (keeping their teardown fns) and `sessions.refresh()`; returns a cleanup that runs both teardowns on unmount.
  - `$effect` on `daemon.onReconnect(() => sessions.refresh())` — re-lists sessions every time the daemon comes back online; the effect's cleanup removes the reconnect callback.
  - `{#key router.route}` outlet — re-mounts the active view component on route change with a `fly` transition (collapsed to 0ms when `reduceMotion` is set, since Svelte JS transitions don't honor `prefers-reduced-motion` on their own); maps each `Route` string to its view, falling back to an `EmptyState` "coming soon".
  - `reduceMotion` — module-eval `matchMedia("(prefers-reduced-motion: reduce)")` check gating the outlet transition duration.
- **Depends on:** `$lib/router.svelte`, `$lib/stores/{daemon,sessions,feed}.svelte`, all `$lib/components/*` chrome, all `./views/*` view components, `svelte` (`onMount`), `svelte/transition` (`fly`).
- **Used by / entrypoint:** entrypoint — mounted by `main.ts`. Top of the component tree.

### internal/gui/frontend/src/lib/actions.ts
- **Role:** Reusable Svelte `use:` actions (currently just focus trapping for modals/sheets).
- **Key symbols:**
  - `trapFocus(node)` — moves focus into a dialog/slide-over on mount, contains Tab/Shift+Tab within its focusable elements, and restores prior focus on `destroy()`. Returns `{ destroy }`.
- **Depends on:** none (DOM only).
- **Used by / entrypoint:** `lib/components/{Sheet,Popover,Shortcuts}.svelte` and `views/{Agents,Dreaming,Skills}.svelte` via `use:trapFocus` on `role="dialog"` panels.

### internal/gui/frontend/src/lib/bridge.ts
- **Role:** The single stable import point for the generated Wails Go-method bindings — a typed facade so views/stores never touch the deep generated path and a regen touches one file.
- **Key symbols (each wraps the same-named Go `*Bridge` method, layering DTO return types):**
  - Health: `Ping`, `Stats` (returns `DaemonStats | null`), `GUIVersion` (version string of THIS gui binary, compared against the daemon's reported version for mismatch detection).
  - Sessions: `Sessions`, `NewSession`, `RemoveSession`, `PruneSessions`, `State`, `ExportSession`, `RemoteSessions`.
  - Turn I/O: `SendInput` (text + `ImageDTO[]` + `allowTools[]`), `SteerInput`, `Interrupt`, `Resend`, `Approve`.
  - Maintenance: `Compact`, `Clear`.
  - Settings (return fresh `SessionStateDTO`): `SetModel`, `SetPerm`, `SetGoal`, `SetTitle`, `SetEffort`, `SetSearch`, `SetFast`.
  - Streaming: `Subscribe`, `Unsubscribe`.
  - Sandbox: `AddDir`, `KillShell`, `DetachBash`.
  - Memory: `Memory`, `AppendMemory`, `WriteUserProfile`, `MemoryBackups`.
  - Bans (banthis hard-prohibition layer): `AddBan`, `RemoveBan` (both return whether an existing ban was replaced/removed).
  - Skills: `Skills`, `SkillBody`, `AcceptSkill`, `RejectSkill`, `InstallSkillFromPath` (local path), `InstallSkillFromGitHub` (`owner/repo` ref) — install variants are scanned before write and return `{ name, path } | null`.
  - Agents: `Agents`, `CancelAgent`, `AgentTranscript`.
  - Dreaming: `Dreaming`, `ConsolidationContent`, `CurrentMemory`.
  - Routing: `Routing`. Observe: `ObserveSummary` (historical log summary; live KPIs come from the daemon stats stream).
  - Crons: `Crons`, `SetTimer`. Plugins: `Plugins`, `SetMarketEnabled`, `RemoveMarketplace`, `RemovePlugin`. Config: `Config`, `SetConfig`.
  - Workflows/commands (run on the active session): `Workflows`, `RunWorkflow`, `Commands`, `RunCommand`.
  - Feed: `Feed`, `FeedFor` (per-project accessor reserved for a future drill-in view), `StartFromFeed`, `DismissFeed`, `RescanFeed`. Machines: `Machines`.
- **Depends on:** `$bindings/github.com/avifenesh/eigen/internal/gui/bridge` (generated), `$lib/types` (DTO shapes).
- **Used by / entrypoint:** imported as `Bridge` by `stores/{daemon,sessions,feed}.svelte` and ~16 views/components (Chat, Home, Sessions, Live, Agents, Skills, Memory, Profile, Plugins, Crons, Config, Routing, Observe, Machines, Dreaming, CommandPalette).

### internal/gui/frontend/src/lib/events.ts
- **Role:** Frontend mirror of the Go event-name builders (`pump.go`/`bridge.go`) plus a typed wrapper over the Wails runtime Events API.
- **Key symbols:**
  - `ev` — event-name table: `sessionEvent(id)`, `sessionClosed(id)`, and constants `daemonStats`, `daemonHealth`, `feed`.
  - `on<T>(name, cb)` — subscribes to a Wails event, unwraps `e.data` to typed `T`, returns the unsubscribe fn verbatim (caller must register inside an `$effect` and return the remover).
- **Depends on:** `@wailsio/runtime` (`Events`).
- **Used by / entrypoint:** `stores/{daemon,feed,transcript}.svelte` and `views/Chat.svelte` (`ev.sessionClosed`).

### internal/gui/frontend/src/lib/router.svelte.ts
- **Role:** Hash-based router (webview-safe under `file://`-style asset serving); reactive via a `$state` snapshot.
- **Key symbols:**
  - `routes` (const tuple) + `Route` type — the 15 canonical route names that map 1:1 to the rail.
  - `parse()` (unexported) — reads `location.hash`, splits `route/param`, defaults unknown routes to `home`.
  - `createRouter()` (unexported) — `$state(parse())`, a `hashchange` listener, and getters `route`/`param` + `go(route, param?)` which sets `location.hash`.
  - `router` — the singleton instance.
- **Depends on:** none (DOM `location`/`hashchange` only).
- **Used by / entrypoint:** `App.svelte` (outlet switch), `lib/components/{Rail,TopBar,CommandPalette}.svelte`, `views/{Chat,Live,Home,Sessions}.svelte`.

### internal/gui/frontend/src/lib/status.ts
- **Role:** Centralized status→StatusDot color mapping plus the shared relative-time formatter, so views never invent their own (mis)mapping or re-roll the unix-nano→ms conversion.
- **Key symbols:**
  - `DotState` type — `"working" | "idle" | "ok" | "warn" | "error"`.
  - `sessionDot(status)` — maps daemon `SessionInfo.Status` (working/approval/error/idle) to a dot state.
  - `taskDot(status)` — maps `agent.BgTask.Status` (running/done/error/lost/canceled) to a dot state.
  - `relTime(nano)` — "x ago" label from a unix-nano timestamp (`SessionInfo.updated`), doing the `/1e6` conversion once so a missed division can't cause a 10^6x drift; views wanting a live-ticking label read their shared `now.ms` clock first.
- **Depends on:** none (pure functions).
- **Used by / entrypoint:** `sessionDot` in `views/{Sessions,Machines,Home,Live}.svelte`; `taskDot` in `views/Agents.svelte` (both feed the `StatusDot` component); `relTime` by the same session/task views for elapsed labels.

### internal/gui/frontend/src/lib/types.ts
- **Role:** Hand-authored TS mirrors of the Go DTOs (`gui/dto.go`) — give the frontend real types at the otherwise-untyped binding seam. Must stay in sync with `dto.go` field names + json tags.
- **Key symbols (types only; no runtime code):** `ImageDTO`, `ToolCallDTO`, `MessageDTO`, `WireEventDTO` (now carries per-event token counts `inTokens`/`outTokens`/`cacheReadTokens`/`cacheWriteTokens` + `provider`/`model`), `StreamEventDTO`, `SessionInfoDTO` (`updated` is unix nano — use `relTime()`), `ToolInfo`, `ShellInfo`, `ApprovalInfo`, `SessionStateDTO` (incl. `effort`/`search`/`fast`/`fastOk`/`running`/`tools`/`roots`/`shells`/`pending`), `CompactResultDTO`, `DaemonStats`, `HealthDTO`, `MemoryNoteDTO`/`MemoryScopeDTO`/`MemoryDTO`, `SkillDTO`/`SkillProposalDTO`/`SkillsDTO`, `BgTaskDTO`/`AgentsDTO`, `RolloutDTO`/`ConsolidationDTO`/`DreamingScopeDTO`/`DreamingDTO`, `ModelDTO` (incl. `search`/`vision`/`social`/`available`)/`ProviderDTO`/`RoutingDTO`, `ToolStatDTO`/`ModelStatDTO`/`HookStatDTO`/`CountDTO`/`RouteStatsDTO`/`SubagentStatsDTO`, `CronDTO`/`CronsDTO`, `ScanFindingDTO`/`InstalledPluginDTO`/`MarketplaceDTO`/`PluginsDTO`, `ConfigFieldDTO`/`ConfigDTO`, `FeedItemDTO`/`FeedDTO`, `MachineDTO`/`MachinesDTO`, `WorkflowInfoDTO`/`WorkflowResultDTO`/`CommandInfoDTO`, `ObserveSummaryDTO`.
- **Notable field facts (must stay in lockstep with the daemon):**
  - `DaemonStats` is the one shape the bridge emits RAW (from `Stats()` and on `eigen:daemon:stats`) as the Go `*daemon.DaemonStats` with native snake_case tags, bypassing the camelCase DTO layer — so its keys (incl. identity fields `version`/`executable`/`binary_sha256`/`vcs_revision`/`vcs_modified` and token totals) must mirror `internal/daemon/protocol.go` 1:1; there is no mapper to catch drift.
  - `MemoryScopeDTO` splits USER.md into `profile` (user-editable section) and `profileLearned` (eigen-auto-maintained, read-only) and adds `bans` (the banthis hard-prohibition block), `adHoc` notes, `backups`, and `bytes`.
  - `InstalledPluginDTO` carries marketplace-scan results: `scanStatus`/`scanCount`/`scans` (`ScanFindingDTO[]`) + `warnings`.
  - `WorkflowInfoDTO`/`WorkflowResultDTO` mirror authored workflows (`~/.eigen/workflows`); `CommandInfoDTO` mirrors custom slash commands (`~/.eigen/commands`).
- **Depends on:** none (type declarations only).
- **Used by / entrypoint:** consumed by `bridge.ts` (return types), every store, and every view that renders daemon data.

### internal/gui/frontend/src/lib/stores/clock.svelte.ts
- **Role:** A single shared 1 Hz clock so views showing live elapsed times read one `now.ms` instead of each spinning their own interval.
- **Key symbols:**
  - `createClock()` (unexported) — `$state(Date.now())`, lazily starts one `setInterval` on first `ms` read (`ensure()`), never torn down (module-lifetime singleton).
  - `now` — the singleton; `now.ms` is the reactive timestamp getter.
- **Depends on:** none.
- **Used by / entrypoint:** `views/{Agents,Sessions,Live}.svelte` read `now.ms` to keep elapsed-time labels ticking.

### internal/gui/frontend/src/lib/stores/daemon.svelte.ts
- **Role:** Daemon connection + stats store. Reflects connection status, the latest `DaemonStats` pushed at ~1 Hz over the event stream, and a GUI-vs-daemon version-mismatch signal.
- **Key symbols:**
  - `createDaemon()` (unexported) — internal `$state` `status` (`connecting|online|offline`), `stats`, and `guiVersion` (this gui binary's own version), plus a `reconnectCbs` Set.
  - `start()` — fires a one-shot `Bridge.GUIVersion()` to populate `guiVersion`; subscribes to `daemonStats` (→ online, fires reconnect callbacks on transition) and `daemonHealth` (→ offline when `!ok`); fires a bootstrap `Bridge.Ping()` that only resolves the initial `connecting` state (guarded so live events aren't clobbered); returns a teardown removing both listeners.
  - getters: `status`, `stats`, `guiVersion`, `daemonVersion` (`stats?.version`, empty until first stats event), `versionMismatch` (true when both versions are known and differ — a stale daemon vs fresh GUI, or vice versa).
  - `onReconnect(f)` — registers an online-transition callback (warns if the Set grows >32), returns a remover; must be used inside an `$effect`.
  - `daemon` — the singleton.
- **Depends on:** `$lib/events` (`on`, `ev`), `$lib/bridge` (`Bridge.Ping`, `Bridge.GUIVersion`), `$lib/types` (`DaemonStats`).
- **Used by / entrypoint:** `App.svelte` (`start`, `onReconnect`), `lib/components/{TopBar,Rail}.svelte`, `views/{Home,Observe,Profile,Chat}.svelte`.

### internal/gui/frontend/src/lib/stores/feed.svelte.ts
- **Role:** Proactive-feed store — rides the daemon `eigen:feed` push stream and seeds from the instant cache; splits items into "act on" vs "ideas" lanes for Home.
- **Key symbols:**
  - `createFeed()` (unexported) — `$state` `items`/`fresh`/`scannedMs`; `apply(f)` ingests a `FeedDTO`.
  - `start()` — seeds via `Bridge.Feed()` then subscribes to `ev.feed`; returns the listener remover.
  - `refresh()` — fires `Bridge.RescanFeed()` (results arrive async via the push stream).
  - `dismiss(key)` — optimistically drops locally then calls `Bridge.DismissFeed(key)`.
  - getters: `items`, `fresh`, `scannedMs`, `actOn` (kind ≠ suggest), `ideas` (kind = suggest), `count`.
  - `feed` — the singleton.
- **Depends on:** `$lib/events` (`on`, `ev`), `$lib/bridge` (`Feed`, `RescanFeed`, `DismissFeed`), `$lib/types` (`FeedDTO`, `FeedItemDTO`).
- **Used by / entrypoint:** `App.svelte` (`start`), `lib/components/{Rail,CommandPalette}.svelte`, `views/Home.svelte`.

### internal/gui/frontend/src/lib/stores/sessions.svelte.ts
- **Role:** The live session list backing the Home board and rail badge; refreshed on demand and on daemon reconnect.
- **Key symbols:**
  - `createSessions()` (unexported) — `$state` `list`/`loading`/`error`/`loaded`; an internal monotonic `loadSeq` guard so concurrent refreshes never let a stale snapshot clobber a newer one.
  - `refresh()` — `Bridge.Sessions()`, committing only if it's still the latest call; sets `loaded=true` on first success (lets views tell "no sessions" from "not loaded yet").
  - getters: `list`, `loading`, `error`, `loaded`, `count`.
  - `sessions` — the singleton.
- **Depends on:** `$lib/bridge` (`Bridge.Sessions`), `$lib/types` (`SessionInfoDTO`).
- **Used by / entrypoint:** `App.svelte` (initial refresh + onReconnect refresh), `lib/components/{Rail,CommandPalette}.svelte`, `views/{Home,Sessions,Live,Chat}.svelte`.

### internal/gui/frontend/src/lib/stores/toasts.svelte.ts
- **Role:** Transient feedback toasts with a bounded queue. Most kinds auto-dismiss after TTL; `working` persists (no timer) until dismissed or evicted, so it can track a long-running op.
- **Key symbols:**
  - `ToastKind` type — `success|error|info|working`; `Toast` type — `{ id, kind, text }`.
  - `MAX` (5) / `TTL` (4200ms) constants.
  - `createToasts()` (unexported) — `$state` `items`, a per-id `timers` Map; `dismiss(id)` clears the timer + removes the item; `push(kind, text)` appends, evicts the oldest past `MAX`, schedules a TTL auto-dismiss for every kind except `working`, returns the id.
  - convenience getters/methods: `items`, `push`, `dismiss`, `success(t)`, `error(t)`, `info(t)`, `working(t)`.
  - `toasts` — the singleton.
- **Depends on:** none.
- **Used by / entrypoint:** consumed app-wide (~17 views/components) via `toasts.success/error/info/working`; `lib/components/ToastHost.svelte` renders `toasts.items` and calls `toasts.dismiss`.

### internal/gui/frontend/src/lib/stores/transcript.svelte.ts
- **Role:** Per-session streaming transcript factory built for high-frequency token deltas without re-render storms or unbounded growth (rAF coalescing, in-place `$state` array mutation, hard `CAP`).
- **Key symbols:**
  - Block types: `TextBlock`, `ToolBlock`, `NoteBlock`, union `Block`; `Transcript` = `ReturnType<typeof createTranscript>`.
  - `CAP` (2000) — live-window block cap; older blocks page in from `Bridge.State` (`truncated` flags the boundary).
  - `TURN_KINDS` (module-level Set) — the event kinds that imply an in-flight turn (`text`/`reasoning`/`tool_start`/`tool_result`/`approval`); a live (non-replay) one of these may flip an idle session to running. Wake/lifecycle kinds (`note`/`bg_done`/`done`/unknown) never resurrect an idle session.
  - `createTranscript(sessionId)` — the factory; internal `$state` `history`/`live`/`running`/`truncated`/`approvalSeq`/`liveTokens`, plus rAF/pending-delta bookkeeping and a `streamedThisTurn` guard. Internal helpers: `pushHistory` (CAP splice), `resetPending`, `commitLive`, `scheduleFlush`/`flush` (once-per-frame coalesce), `onEvent` (handles `text`/`reasoning`/`tool_start`/`tool_result`/`done`/`note`/`approval`, threading per-event token counts into `liveTokens`), `nextUid`.
  - `streamedThisTurn` behavior (GUI-092): non-Streamer providers (Converse/opus path) deliver a whole answer as a single `done`{text} with no preceding deltas, so `done` pushes `e.text` as a final text block only when nothing streamed; the flag is reset at fresh-turn start, on `seed`, and on terminal `note`. A terminal-only `note` (provider error/interrupt/overflow) also clears `running` (no `done` follows it).
  - `liveTokens` — latest token count streamed mid-turn (prefers `outTokens`, falls back to `inTokens`); lets the dock context ring update live instead of freezing at `sess.tokens` until turn end. Cleared (→0) on `done`; 0 means "fall back to `sess.tokens`".
  - Returned API: getters `history`/`live`/`running`/`truncated`/`approvalSeq`/`liveTokens`; `seed(messages, isRunning)` (seed newest `CAP` from a `State` snapshot, dropping any half-streamed live block so the daemon's reconnect replay can't double-render the reply); `start()` (register the Wails listener; replay events don't flip idle→running and replayed approvals don't bump `approvalSeq`); `dispose()` (remove listener, cancel rAF, clear state).
  - `mapMessages(messages, uid)` (module-level, unexported) — collapses assistant tool calls + their results into `ToolBlock`s via a `Map<toolId,block>` (O(N), GUI-018), last-wins matching, never keying on an empty id (id-less results become standalone done blocks, GUI-062); caps the mapped window at the last `CAP*2` messages; shares the store's monotonic uid space.
- **Depends on:** `$lib/events` (`on`, `ev.sessionEvent`), `$lib/types` (`StreamEventDTO`, `WireEventDTO`, `MessageDTO`).
- **Used by / entrypoint:** `views/Chat.svelte` only — `createTranscript(id)` per active session, with `seed`/`start`/`dispose` driven by Chat's `$effect`, and `history`/`live`/`running`/`truncated`/`approvalSeq`/`liveTokens` read for rendering and `State()` refetch triggers.

## Cross-links
- **gui-bridge** (`internal/gui/*.go`) — every `Bridge.*` wrapper here is a 1:1 typed facade over a Go `*Bridge` method (`bridge.go`, `feed.go`, `memory.go`, the workflow/command and skill-install/ban handlers, etc.); `types.ts` mirrors `gui/dto.go`; `events.ts` mirrors the Go event-name builders in `pump.go`/`bridge.go`. `Bridge.GUIVersion` returns the gui binary's own version (`llm.FullVersion`-style identity) the daemon store compares against `DaemonStats.version`.
- **daemon** (`internal/daemon`) — `DaemonStats` shape and the `eigen:daemon:stats`/`eigen:daemon:health` event stream; session `Status` strings (`session.go`) drive `sessionDot`.
- **agent** (`internal/agent`) — `BgTask.Status` strings drive `taskDot`; `BgTaskDTO`/`AgentsDTO` mirror agent background-task state.
- **gui-views-a / gui-views-b** (`internal/gui/frontend/src/views/*`, `components/*`) — the consumers of this slice: every view imports `Bridge`, the stores, `router`, `status`, `actions`, and `types`.
- **skill-feed-retrieve** (`internal/feed`/skills) — the proactive `FeedDTO`/`FeedItemDTO` data the `feed` store renders originates from the daemon's feed/scan loop.
- **Wails runtime** — `@wailsio/runtime` `Events` API (`events.ts`) and the generated `$bindings/.../internal/gui/bridge` JS (`bridge.ts`).
