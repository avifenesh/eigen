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
  - `{#key router.route}` outlet — re-mounts the active view component on route change with a fly transition; maps each `Route` string to its view, falling back to an `EmptyState` "coming soon".
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
  - Health: `Ping`, `Stats` (returns `unknown`).
  - Sessions: `Sessions`, `NewSession`, `RemoveSession`, `PruneSessions`, `State`, `ExportSession`, `RemoteSessions`.
  - Turn I/O: `SendInput`, `SteerInput`, `Interrupt`, `Resend`, `Approve`.
  - Maintenance: `Compact`, `Clear`.
  - Settings (return fresh `SessionStateDTO`): `SetModel`, `SetPerm`, `SetGoal`, `SetTitle`, `SetEffort`, `SetSearch`, `SetFast`.
  - Streaming: `Subscribe`, `Unsubscribe`.
  - Sandbox: `AddDir`, `KillShell`, `DetachBash`.
  - Memory: `Memory`, `AppendMemory`, `WriteUserProfile`, `MemoryBackups`.
  - Skills: `Skills`, `SkillBody`, `AcceptSkill`, `RejectSkill`.
  - Agents: `Agents`, `CancelAgent`, `AgentTranscript`.
  - Dreaming: `Dreaming`, `ConsolidationContent`, `CurrentMemory`.
  - Routing: `Routing`. Observe: `ObserveSummary`.
  - Crons: `Crons`, `SetTimer`. Plugins: `Plugins`, `SetMarketEnabled`, `RemoveMarketplace`, `RemovePlugin`. Config: `Config`, `SetConfig`.
  - Feed: `Feed`, `FeedFor`, `StartFromFeed`, `DismissFeed`, `RescanFeed`. Machines: `Machines`.
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
- **Role:** Centralized status→StatusDot color mapping so views never invent their own (mis)mapping.
- **Key symbols:**
  - `DotState` type — `"working" | "idle" | "ok" | "warn" | "error"`.
  - `sessionDot(status)` — maps daemon `SessionInfo.Status` (working/approval/error/idle) to a dot state.
  - `taskDot(status)` — maps `agent.BgTask.Status` (running/done/error/lost/canceled) to a dot state.
- **Depends on:** none (pure functions).
- **Used by / entrypoint:** `sessionDot` in `views/{Sessions,Machines,Home,Live}.svelte`; `taskDot` in `views/Agents.svelte`. Both feed the `StatusDot` component.

### internal/gui/frontend/src/lib/types.ts
- **Role:** Hand-authored TS mirrors of the Go DTOs (`gui/dto.go`) — give the frontend real types at the otherwise-untyped binding seam. Must stay in sync with `dto.go` field names + json tags.
- **Key symbols (types only; no runtime code):** `ImageDTO`, `ToolCallDTO`, `MessageDTO`, `WireEventDTO`, `StreamEventDTO`, `SessionInfoDTO`, `ToolInfo`, `ShellInfo`, `ApprovalInfo`, `SessionStateDTO`, `CompactResultDTO`, `DaemonStats`, `HealthDTO`, `MemoryNoteDTO`/`MemoryScopeDTO`/`MemoryDTO`, `SkillDTO`/`SkillProposalDTO`/`SkillsDTO`, `BgTaskDTO`/`AgentsDTO`, `RolloutDTO`/`ConsolidationDTO`/`DreamingScopeDTO`/`DreamingDTO`, `ModelDTO`/`ProviderDTO`/`RoutingDTO`, `ToolStatDTO`/`ModelStatDTO`/`HookStatDTO`/`CountDTO`/`RouteStatsDTO`/`SubagentStatsDTO`, `CronDTO`/`CronsDTO`, `InstalledPluginDTO`/`MarketplaceDTO`/`PluginsDTO`, `ConfigFieldDTO`/`ConfigDTO`, `FeedItemDTO`/`FeedDTO`, `MachineDTO`/`MachinesDTO`, `ObserveSummaryDTO`.
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
- **Role:** Daemon connection + stats store. Reflects connection status and the latest `DaemonStats` pushed at ~1 Hz over the event stream.
- **Key symbols:**
  - `createDaemon()` (unexported) — internal `$state` `status` (`connecting|online|offline`) and `stats`, plus a `reconnectCbs` Set.
  - `start()` — subscribes to `daemonStats` (→ online, fires reconnect callbacks on transition) and `daemonHealth` (→ offline when `!ok`); fires a bootstrap `Bridge.Ping()` that only resolves the initial `connecting` state (guarded so live events aren't clobbered); returns a teardown removing both listeners.
  - getters `status`, `stats`.
  - `onReconnect(f)` — registers an online-transition callback (warns if the Set grows >32), returns a remover; must be used inside an `$effect`.
  - `daemon` — the singleton.
- **Depends on:** `$lib/events` (`on`, `ev`), `$lib/bridge` (`Bridge.Ping`), `$lib/types` (`DaemonStats`).
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
- **Role:** Transient feedback toasts with auto-dismiss and a bounded queue.
- **Key symbols:**
  - `ToastKind` type — `success|error|info|working`; `Toast` type — `{ id, kind, text }`.
  - `MAX` (5) / `TTL` (4200ms) constants.
  - `createToasts()` (unexported) — `$state` `items`, a per-id `timers` Map; `dismiss(id)` clears the timer + removes the item; `push(kind, text)` appends, evicts the oldest past `MAX`, schedules auto-dismiss, returns the id.
  - convenience getters/methods: `items`, `push`, `dismiss`, `success(t)`, `error(t)`, `info(t)`.
  - `toasts` — the singleton.
- **Depends on:** none.
- **Used by / entrypoint:** consumed app-wide (~17 views/components) via `toasts.success/error/info`; `lib/components/ToastHost.svelte` renders `toasts.items` and calls `toasts.dismiss`.

### internal/gui/frontend/src/lib/stores/transcript.svelte.ts
- **Role:** Per-session streaming transcript factory built for high-frequency token deltas without re-render storms or unbounded growth (rAF coalescing, in-place `$state` array mutation, hard `CAP`).
- **Key symbols:**
  - Block types: `TextBlock`, `ToolBlock`, `NoteBlock`, union `Block`; `Transcript` = `ReturnType<typeof createTranscript>`.
  - `CAP` (2000) — live-window block cap; older blocks page in from `Bridge.State` (`truncated` flags the boundary).
  - `createTranscript(sessionId)` — the factory; internal `$state` `history`/`live`/`running`/`truncated`/`approvalSeq`, plus rAF/pending-delta bookkeeping. Internal helpers: `pushHistory` (CAP splice), `resetPending`, `commitLive`, `scheduleFlush`/`flush` (once-per-frame coalesce), `onEvent` (handles `text`/`reasoning`/`tool_start`/`tool_result`/`done`/`note`/`approval`), `nextUid`.
  - Returned API: getters `history`/`live`/`running`/`truncated`/`approvalSeq`; `seed(messages, isRunning)` (seed newest `CAP` from a `State` snapshot); `start()` (register the Wails listener; replay events don't flip idle→running); `dispose()` (remove listener, cancel rAF, clear state).
  - `mapMessages(messages, uid)` (module-level, unexported) — collapses assistant tool calls + their results into `ToolBlock`s, sharing the store's monotonic uid space.
- **Depends on:** `$lib/events` (`on`, `ev.sessionEvent`), `$lib/types` (`StreamEventDTO`, `WireEventDTO`, `MessageDTO`).
- **Used by / entrypoint:** `views/Chat.svelte` only — `createTranscript(id)` per active session, with `seed`/`start`/`dispose` driven by Chat's `$effect`, and `history`/`live`/`running`/`truncated`/`approvalSeq` read for rendering and `State()` refetch triggers.

## Cross-links
- **gui-bridge** (`internal/gui/*.go`) — every `Bridge.*` wrapper here is a 1:1 typed facade over a Go `*Bridge` method (`bridge.go`, `feed.go`, `memory.go`, etc.); `types.ts` mirrors `gui/dto.go`; `events.ts` mirrors the Go event-name builders in `pump.go`/`bridge.go`.
- **daemon** (`internal/daemon`) — `DaemonStats` shape and the `eigen:daemon:stats`/`eigen:daemon:health` event stream; session `Status` strings (`session.go`) drive `sessionDot`.
- **agent** (`internal/agent`) — `BgTask.Status` strings drive `taskDot`; `BgTaskDTO`/`AgentsDTO` mirror agent background-task state.
- **gui-views-a / gui-views-b** (`internal/gui/frontend/src/views/*`, `components/*`) — the consumers of this slice: every view imports `Bridge`, the stores, `router`, `status`, `actions`, and `types`.
- **skill-feed-retrieve** (`internal/feed`/skills) — the proactive `FeedDTO`/`FeedItemDTO` data the `feed` store renders originates from the daemon's feed/scan loop.
- **Wails runtime** — `@wailsio/runtime` `Events` API (`events.ts`) and the generated `$bindings/.../internal/gui/bridge` JS (`bridge.ts`).
