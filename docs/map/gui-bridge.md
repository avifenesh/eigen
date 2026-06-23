# gui/ — Wails Go bridge & DTOs

> The `internal/gui` package is the **Go side of the Eigen desktop GUI**: a single
> Wails v3 *service* (`*Bridge`) whose every exported method becomes a generated
> TypeScript binding the Svelte 5 frontend calls. It is the seam between the
> Svelte UI and the rest of Eigen. Two IO patterns live here: (1) **request/
> response RPCs** over ONE long-lived control client to the session-host daemon
> (`internal/daemon`), and (2) **streaming "pumps"** — one dedicated daemon
> connection per subscribed session, fanned out to the frontend as `eigen:*`
> Wails events. Beyond the daemon, several bridge methods read/write **local
> filesystem state directly** (memory, skills, plugins, config, crons, agents,
> remote hosts) because the GUI process has the same disk access the CLI does.
> Every type suffixed `…DTO` is a JSON-friendly wire shape: a flattened mirror of
> a daemon/llm/feed/etc. domain type, reshaping the bits that don't bind cleanly
> to TS (raw image bytes → base64, `json.RawMessage` tool args → strings,
> `time.Time` → unix millis). The package is wired up in `main_gui_wails.go`
> (`buildGUIApp`), which constructs the bridge, registers it as a Wails service,
> and owns its `Shutdown`.

## Files

### internal/gui/bridge.go
- **Role:** The core `*Bridge` service: lifecycle, the long-lived control client, daemon health loop, and all session/turn/maintenance/settings RPCs.
- **Key symbols:**
  - `Bridge` (type) — Wails-bound service; holds `app`, `ensure` (daemon dialer), `suggest`/`dirs` (feed inputs), the `ctrl` control client, the `pumps` map, and stop channels, all under one mutex (IO done outside the lock).
  - `NewBridge(ensure, suggest, dirs)` — constructor; injected by `main` so the bridge owns no model/provider construction.
  - `SetApp(app)` — wires the Wails app for event emission (bound + called from bootstrap).
  - `ServiceStartup(ctx, ServiceOptions) error` — Wails v3 lifecycle hook; primes the control client, starts `healthLoop` + `feedLoop`.
  - `ServiceShutdown() error` — Wails shutdown hook; delegates to `Shutdown`.
  - `healthLoop(stop)` — ~1Hz `DaemonStats` push (`eigen:daemon:stats`), backing off to 5s while the daemon is down.
  - `control() (*daemon.Client, error)` — returns/(re)connects the long-lived control client, retrying with backoff outside the mutex; drops stale clients.
  - `Shutdown()` — idempotent teardown of health/feed loops, every pump, and the control client (sync.Once guards).
  - `emit(name, data)` — non-blocking Wails `Event.Emit` wrapper.
  - RPC methods (each acquires `control()` then calls daemon): `Ping`, `Stats`, `Sessions`, `NewSession`, `RemoveSession`, `PruneSessions`, `State`, `SendInput`, `SteerInput`, `Interrupt`, `Resend`, `Approve`, `Compact`, `Clear`, `AddDir`, `KillShell`, `DetachBash`.
  - `setThen(id, fn)` — helper: run a daemon setter then re-fetch+return fresh `SessionStateDTO` so the UI reconciles optimistic state.
  - `SetModel`/`SetPerm`/`SetGoal`/`SetTitle`/`SetEffort`/`SetSearch`/`SetFast` — session-settings mutators built on `setThen`.
- **Depends on:** `internal/daemon` (control client + domain types), `internal/feed` (`Suggester` type), `wails/v3/pkg/application`.
- **Used by / entrypoint:** entrypoint — constructed in `main_gui_wails.go:buildGUIApp` via `gui.NewBridge`, registered with `application.NewService(bridge)`; every exported method reaches the frontend through generated bindings at `internal/gui/frontend/bindings/.../bridge.js`.

### internal/gui/dto.go
- **Role:** Package doc + the core wire DTOs and the daemon/llm ⇄ DTO converters shared by every other file.
- **Key symbols:**
  - `maxImageBytes` (const, 16 MiB) — caps a single decoded inbound image so a hostile data URL can't blow up daemon memory.
  - `ImageDTO`, `ToolCallDTO`, `MessageDTO`, `WireEventDTO`, `StreamEventDTO`, `SessionInfoDTO`, `SessionStateDTO`, `CompactResultDTO`, `HealthDTO` (types) — JSON wire shapes mirroring `llm.*`/`daemon.*`.
  - `toImageDTOs`/`fromImageDTOs` — base64 encode/decode image bytes (the `from` side enforces `maxImageBytes`).
  - `toMessageDTO` — `llm.Message` → `MessageDTO` (tool args → string, images → base64).
  - `fromMessageDTOs` — `[]MessageDTO` → `[]llm.Message`; **test-only** (no production caller — see dead-code).
  - `toWireEventDTO` — `daemon.WireEvent` → `WireEventDTO` (used by the pump stream).
  - `toSessionInfoDTO`, `toSessionStateDTO` — daemon session shapes → DTOs.
- **Depends on:** `internal/daemon`, `internal/llm`.
- **Used by / entrypoint:** internal — converters consumed by `bridge.go`, `pump.go`, `feed.go`, `remote.go`; DTO types serialize across every bound method.

### internal/gui/pump.go
- **Role:** Per-session streaming "pump" — one dedicated daemon connection per subscribed session, relayed to the frontend as Wails events.
- **Key symbols:**
  - `sessionPump` (type) — owns one `*daemon.Client` + a `stop` channel and `stopOnce`/`closeOnce` guards (one connection = one daemon-side view; `Close()` is the whole detach contract).
  - `Subscribe(id) error` — bound; reserves a placeholder pump under the lock (TOCTOU-safe), dials a dedicated connection, `Attach`es a handler that emits `eigen:session:<id>:event`, then starts a watchdog goroutine that tears down on `Unsubscribe` (stop) or daemon death (`Done` → emits `eigen:session:<id>:closed`).
  - `Unsubscribe(id) error` — bound; delegates to `stopPump`.
  - `stopPump(id)` — removes + tears down a pump, every close guarded by sync.Once for race-safety.
  - `sessionEvent(id)`/`sessionClosed(id)` — frontend event-name builders (`eigen:session:<id>:event` / `:closed`).
- **Depends on:** `internal/daemon` (the per-stream `Client` + `Attach`/`WireEvent`).
- **Used by / entrypoint:** entrypoint — `Subscribe`/`Unsubscribe` are bound and called from the frontend chat view; `stopPump` also called by `RemoveSession`/`PruneSessions`; pumps torn down by `Bridge.Shutdown`.

### internal/gui/feed.go
- **Role:** Proactive-feed bridge: the home base "act on" surface (git/github/memory signals + LLM-suggested ideas), with an instant cache read and a background rescan loop.
- **Key symbols:**
  - `eventFeed` (const `eigen:feed`), `feedScanEvery` (const 10m) — push event name + rescan cadence (matches the TUI).
  - `FeedItemDTO`, `FeedDTO` (types) — feed item (with stable dismiss `Key` + display `DirName`) and the snapshot (`Fresh=false` = never scanned).
  - `toFeedItemDTO`/`feedDTO` — `feed.Item`/`feed.Feed` → DTO (top-N filtered, dismissed removed).
  - `Feed() (*FeedDTO, error)` — bound; instant cache read, caches `lastFeed`.
  - `FeedFor(dir)` — bound; feed items scoped to one project dir.
  - `StartFromFeed(dir, task)` — bound; atomically `NewSession`+`SendInput` (the GUI analogue of the TUI one-key act-on).
  - `DismissFeed(key)` — bound; hides an item by key, rebuilding the full `feed.Item` from `lastFeed`, then re-emits the freshened feed.
  - `scanFeed()` — runs one full (slow) scan off the request path, caches + emits `eigen:feed`.
  - `RescanFeed()` — bound; fires `scanFeed` in a goroutine (the "Refresh feed" verb).
  - `feedLoop(stop)` — startup + periodic rescan loop.
  - `projectDirs()` — resolves the dirs to scan from the injected `dirs` provider.
- **Depends on:** `internal/feed` (`Load`/`Scan`/`Top`/`FilterDismissed`/`Dismiss`/`Item`/`Feed`).
- **Used by / entrypoint:** entrypoint — `Feed`/`FeedFor`/`StartFromFeed`/`DismissFeed`/`RescanFeed` bound (used by `Home.svelte`, feed store); `feedLoop` launched by `ServiceStartup`, stopped by `Shutdown`.

### internal/gui/memory.go
- **Role:** Memory-browser bridge; reads/writes the local memory stores (`~/.eigen/memory` + project) directly, NOT via the daemon. Two scopes: project + global.
- **Key symbols:**
  - `MemoryNoteDTO`, `MemoryScopeDTO`, `MemoryDTO` (types) — parsed memory cards, one scope (summary/notes/bans/profile/ad-hoc/backups), and the both-scopes snapshot.
  - `splitNotes(content)` — splits append-only Markdown into entries on blank-line boundaries.
  - `scopeDTO(store, scope)` — builds a `MemoryScopeDTO` from a `memory.Store` (profile only for global).
  - `Memory() (*MemoryDTO, error)` — bound; full project+global snapshot.
  - `AppendMemory(scope, note)` — bound; adds a manual ad-hoc note.
  - `WriteUserProfile(content)` — bound; replaces the global USER.md.
  - `MemoryBackups(scope)` — bound; lists backup snapshot paths.
  - `openScope(scope)` — helper: `memory.OpenGlobal()` vs `memory.Open("")`.
- **Depends on:** `internal/memory` (`Store`, `Open`, `OpenGlobal`).
- **Used by / entrypoint:** entrypoint — bound methods called from the Memory view; `openScope` also used by `dreaming.go`.

### internal/gui/observe.go
- **Role:** Observability-summary bridge; reads the local metadata-only event log (`~/.eigen/observe/events.jsonl`) and flattens its maps into sorted slices for the dashboard.
- **Key symbols:**
  - `ToolStatDTO`, `ModelStatDTO`, `HookStatDTO`, `CountDTO`, `RouteStatsDTO`, `SubagentStatsDTO`, `ObserveSummaryDTO` (types) — per-category stat shapes and the dashboard summary (`Available=false` when no log yet).
  - `sortedCounts(map)` — flattens a `map[string]int` to a count-desc, name-asc `[]CountDTO`.
  - `ObserveSummary(limit) (*ObserveSummaryDTO, error)` — bound; reads + sorts the observe summary, reporting unavailable (not error) on first-run.
- **Depends on:** `internal/observe` (`ReadSummary`, `DefaultPath`).
- **Used by / entrypoint:** entrypoint — `ObserveSummary` bound, called from the observability dashboard.

### internal/gui/plugins.go
- **Role:** Plugin/marketplace bridge; read-only listing + the safe management ops (enable/disable/remove marketplace, uninstall plugin). Installing is deliberately NOT exposed.
- **Key symbols:**
  - `InstalledPluginDTO`, `MarketplaceDTO`, `PluginsDTO` (types) — installed-plugin record (incl. scan status/warnings), marketplace record, and the snapshot.
  - `Plugins() (*PluginsDTO, error)` — bound; lists installed plugins + configured marketplaces from the registry.
  - `SetMarketEnabled(name, enabled)` — bound; toggles a marketplace.
  - `RemoveMarketplace(name)` — bound; removes a marketplace.
  - `RemovePlugin(name)` — bound; full uninstall (reverses wiring + removes files).
- **Depends on:** `internal/plugin` (`NewRegistry` + `Installed`/`Markets`/`SetMarketEnabled`/`RemoveMarket`/`Uninstall`).
- **Used by / entrypoint:** entrypoint — bound methods called from the Plugins view.

### internal/gui/routing.go
- **Role:** Routing/models bridge; surfaces the model catalog plus which providers are credentialed (all local + fast, no network probe).
- **Key symbols:**
  - `ModelDTO`, `ProviderDTO`, `RoutingDTO` (types) — catalog entry (with derived `Available`), provider+credential status, and the snapshot.
  - `providerUniverse` (var) — canonical provider names (`mantle`/`converse`/`anthropic`/`codex`/`grok`/`glm`/`llama`).
  - `Routing() (*RoutingDTO, error)` — bound; builds the catalog, canonicalizing each model's provider so view/count/credential all key off the same backend, and appends custom providers from `~/.eigen/providers.json`.
- **Depends on:** `internal/llm` (`Models`, `ProviderAvailable`, `CanonicalProvider`, `ResolveProvider`, `ModelInfo`).
- **Used by / entrypoint:** entrypoint — `Routing` bound, called from the routing/models view.

### internal/gui/skills.go
- **Role:** Skills-gallery bridge; reads `SKILL.md` files from local dirs directly, plus dream-proposed drafts awaiting accept/reject.
- **Key symbols:**
  - `SkillDTO`, `SkillProposalDTO`, `SkillsDTO` (types) — discovered skill (source = user/project/extra), a proposal, and the snapshot.
  - `skillDirs()` — mirrors `main.skillDirs` (`~/.eigen/skills`, `.eigen/skills`, `EIGEN_SKILLS_DIRS`).
  - `sourceOf(path)` — classifies a skill path into user/project/extra (resolves to absolute first).
  - `Skills() (*SkillsDTO, error)` — bound; discovered skills + proposals.
  - `SkillBody(name)` — bound; a skill's Markdown body (frontmatter stripped) for preview.
  - `AcceptSkill(name)`/`RejectSkill(name)` — bound; promote/discard a dream proposal.
- **Depends on:** `internal/skill` (`Discover`, `Proposals`, `Accept`, `Reject`, `Set.List`/`Body`).
- **Used by / entrypoint:** entrypoint — bound methods called from the Skills view.

### internal/gui/agents.go
- **Role:** Agent fan-out bridge; reads subtask/background-task records from `agent.TasksDir()` directly and can request cancellation.
- **Key symbols:**
  - `BgTaskDTO`, `AgentsDTO` (types) — one task (times as unix millis) and the board snapshot grouped by status counts.
  - `ms(time)` — `time.Time` → unix millis (0 for zero) helper.
  - `toBgTaskDTO(t)` — `agent.BgTask` → DTO.
  - `Agents() (*AgentsDTO, error)` — bound; loads tasks newest-first + running/done/errored counts.
  - `CancelAgent(id)` — bound; drops a cancel marker the host observes.
  - `AgentTranscript(id)` — bound; reads `<id>.transcript.jsonl` if present.
- **Depends on:** `internal/agent` (`BgTask`, `TasksDir`, `LoadBgTasks`, `RequestCancel`).
- **Used by / entrypoint:** entrypoint — bound methods called from `Agents.svelte`.

### internal/gui/config.go
- **Role:** Config-form bridge; surfaces editable `~/.eigen/config.json` as typed fields and validates writes through `config.Set`.
- **Key symbols:**
  - `ConfigFieldDTO`, `ConfigDTO` (types) — one editable field (with options/multi) and the snapshot + path.
  - `dynamicOptions(kind)` — resolves catalog-dependent option sets (`models` → model IDs, `providers` → canonical providers).
  - `Config() (*ConfigDTO, error)` — bound; editable fields + current values + options.
  - `SetConfig(key, value)` — bound; validates + persists one key, returns the normalized stored value.
- **Depends on:** `internal/config` (`Load`/`Fields`/`Get`/`Set`/`Save`/`Path`), `internal/llm` (`Models`).
- **Used by / entrypoint:** entrypoint — bound methods called from the Config view.

### internal/gui/crons.go
- **Role:** Scheduled-work bridge; surfaces systemd `--user` timers + the user's crontab via shelling out, and timer control verbs.
- **Key symbols:**
  - `CronDTO`, `CronsDTO` (types) — one job (timer or crontab line) and the snapshot (with `SystemdAvail`).
  - `humanizeMicros(us)` — systemd microsecond timestamp → human "today HH:MM" / date string.
  - `loadSystemdTimers()` — runs `systemctl --user list-timers --output=json`, parses to `CronDTO`s (returns availability flag).
  - `loadCrontab()` — runs `crontab -l`, parses spec+command lines.
  - `Crons() (*CronsDTO, error)` — bound; merged timers + crontab snapshot.
  - `SetTimer(unit, verb)` — bound; `systemctl --user <start|stop|enable|disable> <unit>` with validation.
- **Depends on:** stdlib only (`os/exec`, `encoding/json`) — shells out to `systemctl`/`crontab`.
- **Used by / entrypoint:** entrypoint — `Crons`/`SetTimer` bound, called from the Crons view.

### internal/gui/dreaming.go
- **Role:** Dreaming-history bridge; reconstructs the memory-consolidation timeline (rollout summaries + timestamped `.bak` snapshots) from local files for diffing.
- **Key symbols:**
  - `RolloutDTO`, `ConsolidationDTO`, `DreamingScopeDTO`, `DreamingDTO` (types) — a distilled rollout, a memory snapshot, the per-scope history, and the both-scopes snapshot.
  - `dreamScope(store, scope)` — builds a `DreamingScopeDTO` (rollouts newest-first, backups newest-first).
  - `parseOutcome(s)` — pulls a leading outcome marker (success/partial/failed/skip) from a rollout.
  - `parseBakStamp(path)` — parses `MEMORY.md.20060102-150405.bak` → label + unix millis.
  - `Dreaming() (*DreamingDTO, error)` — bound; project + global dreaming history.
  - `ConsolidationContent(path)` — bound; raw content of a `.bak` snapshot (path-guarded: must look like a memory backup).
  - `CurrentMemory(scope)` — bound; current MEMORY.md content (the "after" side of a diff).
- **Depends on:** `internal/memory` (`Store`, `Open`, `OpenGlobal`; also reuses `openScope` from memory.go).
- **Used by / entrypoint:** entrypoint — bound methods called from `Dreaming.svelte`.

### internal/gui/sessions_extra.go
- **Role:** Session-manager extras — transcript export to disk (List/Remove/Prune are bridged in bridge.go).
- **Key symbols:**
  - `ExportSession(id) (string, error)` — bound; opens the local session store, discovers, and exports the transcript to `~/eigen-exports/<id>-<stamp>.jsonl`.
  - `exportStamp()` — filename-safe timestamp helper (isolated so it's the only time-dependent line).
  - `safeFileID(id)` — sanitizes a session id for use in a filename.
- **Depends on:** `internal/session` (`Open`, `Discover`, `Export`).
- **Used by / entrypoint:** entrypoint — `ExportSession` bound, called from the Sessions view.

### internal/gui/remote.go
- **Role:** Remote-machines bridge; lists ssh-reachable targets (saved + `~/.ssh/config` aliases) locally, and lists sessions on a remote daemon over ssh on demand. Install deliberately NOT exposed.
- **Key symbols:**
  - `MachineDTO`, `MachinesDTO` (types) — one remote target (saved/detected flags) and the snapshot.
  - `Machines() (*MachinesDTO, error)` — bound; saved + ssh-config-detected targets (instant, local).
  - `RemoteSessions(target) ([]SessionInfoDTO, error)` — bound; dials over ssh to list a remote daemon's sessions (slow; drill-in only).
- **Depends on:** `internal/remote` (`Machine`, `Machines`, `ListSessions`); reuses `toSessionInfoDTO` from dto.go.
- **Used by / entrypoint:** entrypoint — bound methods called from the Remote/Machines view.

## Cross-links

- **`internal/daemon`** — the session-host daemon: the control client (request/response RPCs in bridge.go) + per-session streaming pumps (pump.go); also the source of `WireEvent`/`SessionInfo`/`SessionState`/`ToolInfo`/`ShellInfo`/`ApprovalInfo`/`DaemonStats` domain types DTO'd here.
- **`internal/llm`** — model catalog + provider credential checks (routing.go, config.go), `Message`/`ToolCall`/`Image`/`ModelInfo`/`Role`/`Provider` types (dto.go), and the suggester provider built in `main_gui_wails.go`.
- **`internal/feed`** — proactive-feed scan/cache/dismiss + the `Suggester` injection point (feed.go, bridge.go).
- **`internal/memory`** — local memory stores for the Memory + Dreaming views (memory.go, dreaming.go).
- **`internal/observe`** — local metadata-only event log for the observability dashboard (observe.go).
- **`internal/plugin`** — plugin/marketplace registry (plugins.go).
- **`internal/skill`** — SKILL.md discovery + dream proposals (skills.go).
- **`internal/agent`** — background/subtask records for the fan-out board (agents.go).
- **`internal/config`** — editable `~/.eigen/config.json` form (config.go).
- **`internal/session`** — local session store transcript export (sessions_extra.go).
- **`internal/remote`** — ssh-reachable machines + remote session listing (remote.go).
- **`wailsapp/wails/v3/pkg/application`** — the Wails v3 service host: `*Bridge` is registered as a service, methods → generated TS bindings, `app.Event.Emit` pushes `eigen:*` events.
- **`main` (root: `main_gui_wails.go`)** — the entrypoint that constructs the bridge (`gui.NewBridge` with `ensureDaemon`/`guiSuggester`/`guiProjectDirs`), registers it as a Wails service, calls `SetApp`, and owns `Shutdown`.
- **`internal/gui/frontend`** — the Svelte 5 frontend; generated Go→TS bindings at `frontend/bindings/.../bridge.js` wrap every exported `*Bridge` method, consumed by `frontend/src/lib/bridge.ts` and the `*.svelte` views.
