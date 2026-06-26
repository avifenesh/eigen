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
- **Role:** The core `*Bridge` service: lifecycle, the long-lived control client, daemon health loop, all session/turn/maintenance/settings RPCs, plus the workflow and custom-command runners.
- **Key symbols:**
  - `Bridge` (type) — Wails-bound service; holds `app`, `ensure` (daemon dialer), `suggest`/`dirs` (feed inputs), the `ctrl` control client, the `pumps` map, `closing` flag, `pollStop`/`feedStop` channels, `lastFeed` (most-recent scan, so `DismissFeed` can rebuild an `Item` from its key), and the lazily-built voice controller (`voiceOnce`/`voiceCtl`, see voice.go), all under one mutex (IO done outside the lock).
  - `NewBridge(ensure, suggest, dirs)` — constructor; injected by `main` so the bridge owns no model/provider construction.
  - `SetApp(app)` — wires the Wails app for event emission (bound + called from bootstrap).
  - `ServiceStartup(ctx, ServiceOptions) error` — Wails v3 lifecycle hook; primes the control client (emits `eigen:daemon:health` on failure), starts `healthLoop` + `feedLoop`.
  - `ServiceShutdown() error` — Wails shutdown hook; delegates to `Shutdown`.
  - `eventDaemonStats` (`eigen:daemon:stats`) / `eventDaemonHealth` (`eigen:daemon:health`) — health-stream event names. A long doc block above `healthLoop` states the **DaemonStats parity contract**: `*daemon.DaemonStats` is the ONE shape emitted RAW (its native snake_case JSON tags, bypassing the dto.go camelCase layer) — both on the stats stream and from `Stats()` — so `types.ts` hand-mirrors those tags 1:1 (esp. version/executable/binary_sha256/vcs_revision/vcs_modified) since no DTO+mapper enforces the mapping.
  - `healthLoop(stop)` — ~1Hz RAW `DaemonStats` push (`eigen:daemon:stats`); on failure emits a `HealthDTO` on `eigen:daemon:health` and backs off to 5s while the daemon is down.
  - `control() (*daemon.Client, error)` — returns/(re)connects the long-lived control client, retrying with backoff outside the mutex; drops stale clients; refuses while `closing`.
  - `Shutdown()` — idempotent teardown of health/feed loops, every pump, the control client, and any running voice loop (`VoiceModeStop`) (`closing` flag + per-pump sync.Once guards).
  - `emit(name, data)` — non-blocking Wails `Event.Emit` wrapper.
  - `GUIVersion() string` — build-stamped version of THIS gui binary (via `llm.FullVersion()`), independent of the daemon's; the frontend diffs it against `DaemonStats.version` to flag a daemon/gui mismatch.
  - RPC methods (each acquires `control()` then calls daemon): `Ping`, `Stats` (RAW `*daemon.DaemonStats`), `Sessions`, `NewSession`, `RemoveSession`, `PruneSessions`, `State`, `SendInput`, `SteerInput`, `Interrupt`, `Resend`, `Approve`, `Compact`, `Clear`, `AddDir`, `KillShell`, `DetachBash`.
  - `setThen(id, fn)` — helper: run a daemon setter then re-fetch+return fresh `SessionStateDTO` so the UI reconciles optimistic state.
  - `SetModel`/`SetPerm`/`SetGoal`/`SetTitle`/`SetEffort`/`SetSearch`/`SetFast` — session-settings mutators built on `setThen`.
  - **Workflows:** `WorkflowInfoDTO`, `WorkflowResultDTO` (mirrors `workflow.Result`), `Workflows()` (lists authored `~/.eigen/workflows`, skipping unparseable), `RunWorkflow(sessionID, name, vars)` (plays steps on ONE daemon session via `daemonStepRunner`; no Judge wired, so `check:` steps fail closed), and the internal `daemonStepRunner`/`awaitTurn`/`lastAssistantText` helpers (submit a step's prompt, poll `State` at ~4Hz until the turn ends, return the last assistant text; per-step `model:` is a live switch then restored).
  - **Custom commands:** `CommandInfoDTO`, `Commands()` (lists project+user slash commands via `command.Load(command.Dirs()...)`, project shadows user), `RunCommand(sessionID, name, args)` (expands `$ARGUMENTS`/`$1..$9` via `command.Expand`, best-effort `model:` switch, scopes the turn to `allowed-tools`, returns the expanded prompt to echo).
- **Depends on:** `internal/daemon` (control client + domain types), `internal/feed` (`Suggester` type), `internal/llm` (`FullVersion`, `Message`/`Role`), `internal/command`, `internal/workflow`, `wailsapp/wails/v3/pkg/application`.
- **Used by / entrypoint:** entrypoint — constructed in `main_gui_wails.go:buildGUIApp` via `gui.NewBridge(ensureDaemon, guiSuggester(), guiProjectDirs)`, registered with `application.NewService(bridge)`; every exported method reaches the frontend through generated bindings at `internal/gui/frontend/bindings/.../bridge.js`.

### internal/gui/dto.go
- **Role:** Package doc + the core wire DTOs and the daemon/llm ⇄ DTO converters shared by every other file.
- **Key symbols:**
  - `maxImageBytes` (const, 16 MiB) — caps a single decoded inbound image so a hostile data URL can't blow up daemon memory.
  - `ImageDTO`, `ToolCallDTO`, `MessageDTO`, `WireEventDTO`, `StreamEventDTO`, `SessionInfoDTO`, `SessionStateDTO`, `CompactResultDTO`, `HealthDTO` (types) — JSON wire shapes mirroring `llm.*`/`daemon.*`. `WireEventDTO` carries the streamed agent event plus `EventDone` attribution (`Provider`/`Model`) and prompt-cache counters (`CacheReadTokens`/`CacheWriteTokens`). `StreamEventDTO` is the per-session event-channel payload (`Event` + `Replay` flag).
  - `toImageDTOs`/`fromImageDTOs` — base64 encode/decode image bytes (the `from` side enforces `maxImageBytes`).
  - `toMessageDTO` — `llm.Message` → `MessageDTO` (tool args → string, images → base64).
  - `toWireEventDTO` — `daemon.WireEvent` → `WireEventDTO` (used by the pump stream; carries provider/model + cache counters through).
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
  - `feedScanning` (package-scope `atomic.Bool`) — single-flight guard for the slow scan (git/gh subprocesses + a capped ~2-min LLM suggester racing fixed `feed.json`/`feed-suggest.json` paths); `scanFeed` CAS-acquires it so a ticker tick or palette "Refresh feed" landing mid-scan coalesces into a no-op. Package-scope because the GUI hosts exactly one `Bridge`.
  - `FeedItemDTO`, `FeedDTO` (types) — feed item (with stable dismiss `Key` + display `DirName`) and the snapshot (`Fresh=false` = never scanned).
  - `toFeedItemDTO`/`feedDTO` — `feed.Item`/`feed.Feed` → DTO (top-N filtered via `feed.Top(..., 12, 4)`, dismissed removed).
  - `Feed() (*FeedDTO, error)` — bound; instant cache read, caches `lastFeed`.
  - `FeedFor(dir)` — bound; feed items scoped to one project dir.
  - `StartFromFeed(dir, task)` — bound; atomically `NewSession`+`SendInput` (the GUI analogue of the TUI one-key act-on).
  - `DismissFeed(key)` — bound; hides an item by key, rebuilding the full `feed.Item` from `lastFeed`, then re-emits the freshened feed.
  - `scanFeed()` — single-flights on `feedScanning`, runs one full (slow, 2-min ctx) scan off the request path, caches + emits `eigen:feed`.
  - `RescanFeed()` — bound; fires `scanFeed` in a goroutine (the "Refresh feed" verb; spam-safe via the single-flight).
  - `feedLoop(stop)` — emits the cache immediately, then startup + periodic (`feedScanEvery`) rescan loop.
  - `projectDirs()` — resolves the dirs to scan from the injected `dirs` provider.
- **Depends on:** `internal/feed` (`Load`/`Scan`/`Top`/`FilterDismissed`/`Dismiss`/`Item`/`Feed`).
- **Used by / entrypoint:** entrypoint — `Feed`/`FeedFor`/`StartFromFeed`/`DismissFeed`/`RescanFeed` bound (used by `Home.svelte`, feed store); `feedLoop` launched by `ServiceStartup`, stopped by `Shutdown`.

### internal/gui/memory.go
- **Role:** Memory-browser bridge; reads/writes the local memory stores (`~/.eigen/memory` + project) directly, NOT via the daemon. Two scopes: project + global.
- **Key symbols:**
  - `MemoryNoteDTO`, `MemoryScopeDTO`, `MemoryDTO` (types) — a parsed memory card, one scope (summary/notes/structured bans/profile/ad-hoc/backups/bytes), and the both-scopes snapshot. `MemoryScopeDTO` splits USER.md into `Profile` (user-authored, editable) and `ProfileLearned` (eigen-auto-maintained, read-only) — global only — and carries both `Bans` (raw) and `BanList` (structured `[]memory.Ban` title/rule blocks for editing).
  - `splitNotes(content)` → `splitOnTopLevelHeadings`/`splitOnBlankLines`/`isTopLevelHeading` — splits curated section-structured MEMORY.md on top-level `## ` heading boundaries (keeping each heading with its body), falling back to blank-line chunks for un-consolidated stores, dropping any leading `# ` file title.
  - `scopeDTO(store, scope)` — builds a `MemoryScopeDTO` from a `memory.Store` (USER.md split + ban list only for global).
  - `Memory() (*MemoryDTO, error)` — bound; full project+global snapshot; a failure to open EITHER scope is surfaced (not swallowed) so the frontend can distinguish a load failure from an empty store.
  - `AppendMemory(scope, note)` — bound; adds a manual note via `Store.Append` (enqueues consolidation+summary maintenance, the agent/TUI path).
  - `AddBan(scope, title, rule)` / `RemoveBan(scope, title)` — bound; the banthis layer native in eigen (mirrors the TUI `/ban` `/unban`); return whether a ban was replaced/removed.
  - `WriteUserProfile(content)` — bound; replaces the global USER.md user-authored section (preserves the learned block).
  - `MemoryBackups(scope)` — bound; lists backup snapshot paths.
  - **Per-project memory (any project, not just current + global):** `MemoryScopeRefDTO{Key,Name,Dir,NoteCount,Current}`; `ListMemoryScopes() ([]MemoryScopeRefDTO, error)` — "Global" first, then the dedup union of `b.projectDirs()` session-history dirs (Key=abs dir) and on-disk `memory.ListProjectStores()` entries (Key=on-disk store key), deduped by resolved `memory.StoreKey`. The cwd project's ref is flagged `Current` (resolved by KEY, not by dir-vs-storepath comparison — those never match — so the frontend picker defaults to it instead of a "Select…" placeholder). Empty stores (0 notes) and ephemeral cwds (`isEphemeralDir`: `/tmp`, `/var/tmp`, `/run`, `agent-workspace`, `-itch-` temp dirs) are filtered so the picker shows real projects, not session scratch. `NoteCount` is computed cheaply (no note bodies shipped). `MemoryForScope(scope)` — opens an ARBITRARY store and returns the SAME rich `MemoryScopeDTO` via `scopeDTO()`; accepts `"global"`, `"project"`/`""` (cwd, back-compat), an absolute dir (`memory.Open`), or an on-disk key (`memory.OpenByKey`).
  - `openMemoryScope(scope)` — the full scope router (global / cwd / abs-dir / on-disk-key). ALL write methods (`AppendMemory`/`AddBan`/`RemoveBan`/`MemoryBackups`) route through it, so editing any project's memory works (not just current + global). `noteCount(store)` — lightweight count via `splitNotes`.
  - `openScope(scope)` — legacy helper (`OpenGlobal` vs `Open("")`); now only used by dreaming.go.
- **Depends on:** `internal/memory` (`Store`, `Open`, `OpenGlobal`, `OpenByKey`, `ListProjectStores`, `StoreKey`/`StoreName`, `Ban`; `Append`/`AddBan`/`RemoveBan`/`ListBans`/`UserProfileUser`/`UserProfileLearned`).
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
  - `ScanFindingDTO`, `InstalledPluginDTO`, `MarketplaceDTO`, `PluginsDTO` (types) — a per-component risky-scan verdict, an installed-plugin record (incl. wired component lists, scan status/count/findings, warnings, derived `Enabled`), a marketplace record, and the snapshot.
  - `Plugins() (*PluginsDTO, error)` — bound; lists installed plugins + configured marketplaces from the registry (each plugin's `Enabled` derived via `pluginEnabled`).
  - `SetMarketEnabled(name, enabled)` — bound; toggles a marketplace.
  - `RemoveMarketplace(name)` — bound; removes a marketplace.
  - `SetPluginEnabled(name, enabled)` — bound; enables/disables ALL of a plugin's wired components at once (skills/agents/commands/MCP/hooks) without uninstalling, via `Registry.SetEnabled` (applies to new sessions only).
  - `RemovePlugin(name)` — bound; full uninstall (reverses wiring + removes files).
  - **Add-a-plugin (GUI install, mirrors skill-add):** `PluginPreviewDTO{Name,Description,Marketplace,Version,Skills,Agents,Commands,MCPServers,Hooks}`; `AddMarketplace(source)` — records a catalog (GitHub owner/repo, https URL, or local path) via `Registry.AddMarketplace`+`DefaultTreeFetcher`, returns the extended `MarketplaceDTO` (now also Description/Version/PluginCount); `MarketplacePlugins(mktName)` — read-only `PreviewPlugin` per catalog entry → `[]PluginPreviewDTO`; `InstallPlugin(pluginName, mktName)` — builds `InstallOptions` with the scanner (FAIL CLOSED if none; `Force=false`), runs `Registry.InstallPlugin`, returns the new `InstalledPluginDTO`. RISKY scan aborts via `*skill.RiskyError` (no GUI force path by design). `mktName=""` lets the first marketplace listing the plugin win.
  - `pluginInstallScanner()`/`pluginInstallOptions()` — duplicate skills.go's small-model scanner ladder + fail-closed discipline (untrusted bundles must be scanned). `installedPluginDTO(reg, p)` — shared `InstalledPlugin`→DTO conversion reused by `Plugins()` and `InstallPlugin`.
  - `pluginEnabled(reg, p)` — derives enabled state from on-disk markers `SetEnabled` flips (a `.disabled`-parked component file with its active copy gone reads as disabled; MCP/hooks-only plugins have nothing to park and read as enabled).
- **Depends on:** `internal/plugin` (`NewRegistry`, `InstalledPlugin` + `Installed`/`Markets`/`SetMarketEnabled`/`RemoveMarket`/`SetEnabled`/`Uninstall`/`AddMarketplace`/`InstallPlugin`/`PreviewPlugin`/`DefaultTreeFetcher`/`InstallOptions`/`SkillsDir`/`AgentsDir`/`CommandsDir`), `internal/skill` (`Scanner`, `RiskyError`).
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
  - `SkillDTO`, `SkillProposalDTO`, `SkillsDTO`, `SkillInstallDTO` (types) — discovered skill (source = user/project/extra), a dream proposal, the snapshot, and an install result (resolved name + written path).
  - `skillDirs()` — mirrors `main.skillDirs` (`~/.eigen/skills`, `.eigen/skills`, `EIGEN_SKILLS_DIRS`).
  - `userSkillsDir()` — the per-user install target (`~/.eigen/skills`, same as `eigen skill add`).
  - `sourceOf(path)` — classifies a skill path into user/project/extra (resolves to absolute first).
  - `Skills() (*SkillsDTO, error)` — bound; discovered skills + proposals.
  - `SkillBody(name)` — bound; a skill's Markdown body (frontmatter stripped) for preview.
  - `AcceptSkill(name)`/`RejectSkill(name)` — bound; promote/discard a dream proposal.
  - `InstallSkillFromPath(path)` / `InstallSkillFromGitHub(ownerRepo)` — bound; install a skill from a local SKILL.md/dir or a GitHub `owner/repo[/subpath][@ref]`; the content is security-scanned before write (a RISKY verdict aborts; the bridge never Forces).
  - `installScanner()` — builds the install scanner on a small/cheap model (`EIGEN_SMALL_MODEL` → grok composer → Haiku, mirroring main's `smallProvider`); nil when nothing is credentialed.
  - `installOptions()` — shared `skill.InstallOptions` (user store, scan on, never Force); fails closed (error, not silent install) when no scanner is available.
- **Depends on:** `internal/skill` (`Discover`, `Proposals`, `Accept`, `Reject`, `Set.List`/`Body`, `InstallFromPath`/`InstallFromGitHub`, `ParseGitHubRef`, `DefaultFetcher`, `ProviderScanner`, `InstallOptions`, `Scanner`), `internal/llm` (`New`, `ProviderAvailable`).
- **Used by / entrypoint:** entrypoint — bound methods called from the Skills view.

### internal/gui/agents.go
- **Role:** Agent fan-out bridge; reads subtask/background-task records from `agent.TasksDir()` directly and can request cancellation.
- **Key symbols:**
  - `BgTaskDTO`, `AgentsDTO` (types) — one task (times as unix millis; carries kind/difficulty/role/attempts/escalated, steps/lastTool/lastNote, token counts, canceling flag) and the board snapshot grouped by status counts (+ `Dir`).
  - `ms(time)` — `time.Time` → unix millis (0 for zero) helper.
  - `toBgTaskDTO(t)` — `agent.BgTask` → DTO.
  - `Agents() (*AgentsDTO, error)` — bound; loads tasks newest-first + running/done/errored counts (`error`+`lost` both count as errored).
  - `CancelAgent(id)` — bound; drops a cancel marker the host observes.
  - `AgentTranscript(id)` — bound; reads `<id>.transcript.jsonl` if present (id validated via `agent.ValidTaskID` before the path join, so a crafted id can't escape the tasks dir).
  - `AgentHistory(id)` — bound; a task's full append-only state trail (attempts/escalations/overflow notes/terminal) in append order, so the board can show why a task retried/escalated.
- **Depends on:** `internal/agent` (`BgTask`, `TasksDir`, `LoadBgTasks`, `RequestCancel`, `ValidTaskID`, `ReadTaskHistory`).
- **Used by / entrypoint:** entrypoint — bound methods called from `Agents.svelte`.

### internal/gui/config.go
- **Role:** Config-form bridge; surfaces editable `~/.eigen/config.json` as typed fields and validates writes through `config.Set`.
- **Key symbols:**
  - `ConfigFieldDTO`, `ConfigDTO` (types) — one editable field (key/desc/value, with options/`Multi`/`AllowEmpty`) and the snapshot + path.
  - `emptyMeaningful` (var) — option-set fields where `""` is a real reachable value (`model`, `judge_model`); drives `AllowEmpty` so the picker keeps offering the unset state.
  - `dynamicOptions(kind)` — resolves catalog-dependent option sets (`models` → model IDs, `providers` → canonical providers).
  - `Config() (*ConfigDTO, error)` — bound; editable fields + current values + options; **skips `Secret` fields** (e.g. telegram_token) so the form never surfaces a credential.
  - `SetConfig(key, value)` — bound; validates + persists one key, returns the normalized stored value.
  - `RuleChainDTO`/`RuleChainsDTO` (types), `ruleRoleDesc` (var), `chainModelChoices()` — the per-role fallback-chain editor's data: one role's ordered chain (`role`/`desc`/`chain`/`custom`) + the model-name choices the picker offers (friendly shorthands from `DefaultRuleChain` ∪ catalog ids).
  - `RuleChains() (*RuleChainsDTO, error)` — bound; every role's current chain (`config.ChainFor`) + `custom` flag (set in `RuleChains` vs built-in default) + model choices.
  - `SetRuleChain(role, chain)` — bound; persists one role's chain (empty → revert to default via `config.SetRuleChain`), returns the stored chain.
- **Depends on:** `internal/config` (`Load`/`Fields`/`Get`/`Set`/`Save`/`Path`/`RuleRoles`/`ChainFor`/`SetRuleChain`/`DefaultRuleChain`; field `Secret`/`Dynamic`/`Multi`), `internal/llm` (`Models`).
- **Used by / entrypoint:** entrypoint — bound methods called from the Config view (form + `RuleChainsEditor`).

### internal/gui/connectors.go
- **Role:** The "superapp" integrations bridge — remote MCP servers (connectors) authorized over OAuth, with live connection status, add/connect/disconnect/remove. The slow browser-opening OAuth flow runs OFF the bound call and reports on the `eigen:connector` event.
- **Key symbols:**
  - `ConnectorDTO`/`ConnectorsDTO` (status + editor rows; `Connected`/`RequiresAuth`/`Expiry`), `connectorEventDTO` (emitted on completion).
  - `Connectors()` — bound; remote servers from `mcp.ListServers` joined with `connector.Default()` OAuth status + expiry + curated-directory display/glyph, PLUS the full `connector.Directory()` (each marked `Added` when already in mcp.json).
  - `CatalogEntryDTO` — one curated directory tile (display/glyph/url/category/added).
  - `AddConnector(name, url, desc)` — bound; writes the `mcp.json` remote entry (`mcp.SaveServer`) then `startConnect` (background OAuth, emits `eigen:connector`).
  - `AddCatalogConnector(name)` — bound; one-click add of a directory connector (URL from `connector.CatalogByName`) → `AddConnector`.
  - `ConnectConnector(name)` (re-auth) / `DisconnectConnector` (drop token, keep entry) / `RemoveConnector` (token + entry) / `SetConnectorDisabled`.
- **Depends on:** `internal/connector` (`Default`), `internal/mcp` (`ListServers`/`SaveServer`/`RemoveServer`/`SetServerDisabled`/`UserConfigPath`).
- **Used by / entrypoint:** the Connectors view; `eigen:connector` event consumed by the frontend.

### internal/gui/wiring.go
- **Role:** The full `mcp.json` server editor (stdio AND remote) so MCP servers are managed in the GUI, not by hand-editing JSON. (Connectors get the richer OAuth surface above; this is the general server list.)
- **Key symbols:** `MCPServerDTO`/`MCPServersDTO` (`EnvPairs` KEY=VALUE lines, `Remote` flag, `SecretEnvKeys` names + write-only `SecretEnvPairs`), `entryToDTO`/`dtoToEntry`/`parsePairs`; bound `MCPServers()` / `SaveMCPServer(d)` / `RemoveMCPServer(name)` / `SetMCPServerDisabled(name, disabled)` / `MCPSecretsAvailable()` (gates the GUI's secret affordance).
- **Depends on:** `internal/mcp` (the config editor + keychain secret store).
- **Used by / entrypoint:** the Connectors view (local-server section + add-server form).

### internal/gui/board.go
- **Role:** The cross-project WORK BOARD — "what's going on across all my projects" in one place (project management for the working station). One lane per project: git state (branch, dirty/unpushed/behind, TODO/FIXME count), open PRs/issues + git loose-ends (grouped from the cached proactive feed), each card one-click startable.
- **Key symbols:** `BoardDTO`/`BoardLaneDTO`/`BoardItemDTO`; bound `Board()` — groups `feed.Load()` git/github items by dir, unions with `projectDirs()`, enriches each lane via local probes (`gitBranch`/`countDirty`/`countRevs`/`countTodos` — `git grep -c TODO|FIXME`, capped `maxTodoScan`), sorts most-actionable-first. Instant (reads the feed cache; no rescan).
- **Depends on:** `internal/feed` (cached items), `os/exec` git probes.
- **Used by / entrypoint:** the Board view (`board` route); items reuse `StartFromFeed`/`NewSession`.

### internal/gui/dashboard.go
- **Role:** The working-station command-center data in ONE call — today's calendar + unread mail (Google, when linked) + machine health (always). Eigen is a working STATION, not a coding tool; Home answers "what's my day + is my machine OK".
- **Key symbols:** `DashboardDTO` (`googleConnected`/`events`/`unreadCount`/`unread`/`health`), `CalEventDTO`/`MailMsgDTO`/`SysHealthDTO`, `healthDTO`; bound `Dashboard()` — reads `syshealth.Read()` + (when `google.Default().Connected()`) `UpcomingEvents`/`UnreadCount`/`RecentUnread`, each section best-effort.
- **Depends on:** `internal/google`, `internal/syshealth`.
- **Used by / entrypoint:** Home's "Today" zone (calendar · inbox · machine panels), refreshed every 60s.

### internal/gui/google.go
- **Role:** Native Google (Calendar + Gmail) status + connect bridge — eigen's direct-REST built-in (NOT an MCP connector), authorized with the user's own Google Cloud OAuth client.
- **Key symbols:** `GoogleStatusDTO` (`configured`/`connected`/`setupHint`/`setupUrl`/`clientPath`); bound `GoogleStatus()`, `ConnectGoogle()` (loopback OAuth, blocks until linked), `DisconnectGoogle()`, `ImportGoogleClient()` (native file picker → `google.ImportClient`; the "Set up" step when not configured).
- **Depends on:** `internal/google` (`Default`/`ImportClient`/`ClientPath`/`SetupURL`).
- **Used by / entrypoint:** the Google card at the top of the Connectors view (Set up → Connect → Disconnect lifecycle).

### internal/gui/crons.go
- **Role:** Scheduled-work bridge; surfaces systemd `--user` timers + the user's crontab via shelling out, and timer control verbs.
- **Key symbols:**
  - `CronDTO`, `CronsDTO` (types) — one job (timer or crontab line) and the snapshot (with `SystemdAvail`).
  - `humanizeMicros(us)` — systemd microsecond timestamp → human "today HH:MM" / date string.
  - `loadSystemdTimers()` — runs `systemctl --user list-timers --output=json`, parses to `CronDTO`s (returns availability flag).
  - `loadCrontab()` — runs `crontab -l`, parses spec+command lines.
  - `Crons() (*CronsDTO, error)` — bound; merged timers + crontab snapshot.
  - `SetTimer(unit, verb)` — bound; `systemctl --user <start|stop|enable|disable> <unit>` with validation.
  - `AddCrontab(spec, command)` / `RemoveCrontab(spec, command)` — bound; WRITABLE crontab: validate the spec (5-field or @keyword), dedupe, and reinstall the whole crontab via `crontab -` (or `crontab -r` when empty). `currentCrontabLines`/`writeCrontab`/`validateCronSpec` helpers.
- **Depends on:** stdlib only (`os/exec`, `encoding/json`) — shells out to `systemctl`/`crontab`.
- **Used by / entrypoint:** entrypoint — `Crons`/`SetTimer`/`AddCrontab`/`RemoveCrontab` bound, called from the Crons view (which now has an add-job form + per-row Remove, no longer read-only).

### internal/gui/dreaming.go
- **Role:** Dreaming-history bridge; reconstructs the memory-consolidation timeline (rollout summaries + timestamped `.bak` snapshots) from local files for diffing.
- **Key symbols:**
  - `RolloutDTO`, `ConsolidationDTO`, `DreamingScopeDTO`, `DreamingDTO`, `DreamReportDTO` (types) — a distilled rollout (with recovered `WhenMs`), a memory `.bak` snapshot (label/whenMs/bytes), the per-scope history (+ `CurrentBytes`), the both-scopes snapshot, and an on-demand dream-run report (`Report`/`Consolidated`/`SummaryRegened`/`Changed`).
  - `dreamScope(store, scope)` — builds a `DreamingScopeDTO` (rollouts newest-first, backups newest-first).
  - `parseOutcome(s)` — pulls a leading outcome marker (success/partial/failed/skip) from a rollout.
  - `rolloutFile` / `rolloutFiles(store, limit)` — re-glob the rollout dirs (Codex-shaped `rollout_summaries/` + legacy `raw/`) retaining each file's path so its timestamp survives (RawSummaries drops filenames).
  - `parseRolloutStamp(path)` / `parseBakStamp(path)` — recover unix millis (and, for baks, a human label) from a `20060102-150405` filename stamp.
  - `Dreaming() (*DreamingDTO, error)` — bound; project + global dreaming history.
  - `DreamingForScope(scope) (*DreamingScopeDTO, error)` — bound; dreaming history for ANY scope (resolved via `b.openMemoryScope`), so the Dreaming view browses any project like Memory does (uses the same `ListMemoryScopes` picker). `CurrentMemory`/`DreamNow` also route through `openMemoryScope` now, so consolidation + diff work for any project.
  - `ConsolidationContent(path)` — bound; raw content of a `.bak` snapshot (path-guarded: must look like a memory backup).
  - `CurrentMemory(scope)` — bound; current MEMORY.md content (the "after" side of a diff).
  - `DreamNow(scope)` — bound; runs an on-demand consolidation in-GUI (no daemon round-trip): builds a `memory.Pipeline` with the same dream callbacks the CLI/daemon use, drains queued downstream jobs (`RunQueued`), and — if nothing was queued — forces `MaybeConsolidate(force)` + `RegenSummary` so a button press always does real work (Stage1 is intentionally not run). Returns a `DreamReportDTO`.
  - `dreamProvider()` — small/cheap model ladder (`EIGEN_SMALL_MODEL` → grok composer → Haiku, same as `installScanner`); errors (not nil) when nothing is credentialed so `DreamNow` fails loud.
  - `newDreamPipeline(prov, mem, idx)` — wires `dream.Stage1`/`Consolidate`/`Summarize` into a `memory.Pipeline`, matching `main.newMemoryPipeline`.
- **Depends on:** `internal/memory` (`Store`, `Open`, `OpenGlobal`, `Pipeline`, `Index`, `OpenIndex`; reuses `openScope` from memory.go), `internal/dream` (`Stage1`/`Consolidate`/`Summarize`), `internal/llm` (`Provider`, `New`, `ProviderAvailable`).
- **Used by / entrypoint:** entrypoint — bound methods called from `Dreaming.svelte`.

### internal/gui/voice.go
- **Role:** Voice bridge; drives eigen's **server-side** voice stack (the GUI runs on the host, so it shells out to the same recorder+whisper STT / Kokoro-espeak TTS the TUI uses, NOT webview `getUserMedia`). Three features mirror the TUI taxonomy: dictate, read-aloud, hands-free conversation mode. Capability-gated — degrades to "unavailable" when nothing is installed.
- **Key symbols:**
  - `eventVoice` (const `eigen:voice`) — push event name; carries `VoiceEventDTO` as the mic/speaker phase changes.
  - `errVoiceUnavailable` (var) — returned when the requested STT/TTS capability isn't installed (defensive; the frontend gates on `VoiceStatus`).
  - `VoiceStatusDTO` (`STT`/`TTS` bools), `VoiceEventDTO` (`Phase` ∈ idle|listening|transcribing|thinking|speaking|error|off, `Text`, `Mode`) — capability + live-state wire shapes.
  - `voiceCtl` (type) — lazily-built voice controller under its own mutex: detected `stt`/`tts`, the single one-shot `cancel`, the conversation-loop `modeStop`, and `speaking`. Built once via `Bridge.voice()` (`voiceOnce`) so detection (cheap PATH probes in `ensureDetected`) runs at most once.
  - `VoiceStatus() (*VoiceStatusDTO, error)` — bound; capability probe for UI gating.
  - `VoiceListen() (string, error)` — bound; records ONE VAD-endpointed utterance → transcript (dictate), emitting listening→idle; a second call supersedes the first. `VoiceCancelListen()` cancels in-flight.
  - `VoiceSpeak(text)` / `VoiceStopSpeak()` — bound; read a string aloud once (cancelable), emitting speaking→idle.
  - `VoiceModeStart(sessionID)` / `VoiceModeStop()` — bound; the hands-free loop (`voiceModeLoop`): listen → `SendInput` the transcript as a turn → `waitForReply` (poll `State` until the turn goes idle, with a start-grace + 10-min cap) → speak the reply → listen again, until stop/error. Needs BOTH STT and TTS. `VoiceModeStop` is also invoked from `Shutdown` so the loop + its subprocess never outlive the window.
  - `latestAssistant(st)` — last assistant message text in a `SessionStateDTO` (the reply to speak); distinct from bridge.go's `lastAssistantText([]llm.Message)`.
- **Depends on:** `internal/voice` (`STT`/`TTS` interfaces, `DetectSTT`/`DetectTTS`/`TTSFromArgv`), `internal/speech` (`Detect`→`Speaker.Argv()`/`Available()`); reuses `Bridge.SendInput`/`State`/`emit`.
- **Used by / entrypoint:** entrypoint — bound methods called from the voice store (`frontend/src/lib/stores/voice.svelte.ts`), Composer (dictate + voice-mode toggle), and the Chat read-aloud control.

### internal/gui/terminal.go
- **Role:** Server-side PTY terminal bridge for the right-panel "Terminal" tool tab — the GUI runs on the host, so it owns a real PTY (creack/pty) and streams raw bytes to the frontend's xterm.js (xterm IS the emulator; no VT logic here). Mirrors the TUI's `termpanel.go` recipe minus the VT step.
- **Key symbols:**
  - `eventTerminal` (const `eigen:terminal`) — output/exit stream; payload `TerminalEventDTO{ID, Data(base64 raw bytes), Exited}`.
  - `term` (type) — one live terminal: its `*os.File` PTY master, the shell `*exec.Cmd`, and a `sync.Once` making teardown idempotent across `TerminalKill`/the waiter/shutdown. Held in a package-level registry (mutex-guarded) keyed by id (the GUI hosts one Bridge).
  - `TerminalStart(cols, rows) (string, error)` — bound; starts `$SHELL` (or /bin/bash) on a `cols×rows` PTY, returns an id; spawns a reader (PTY→base64→emit `eigen:terminal`) + a waiter (Wait→emit `{exited}`+close).
  - `TerminalWrite(id, data)` — bound; writes raw keystroke bytes to the PTY. `TerminalResize(id, cols, rows)` — `pty.Setsize`. `TerminalKill(id)` — kill + close + stop, once-guarded.
  - `terminalShutdownAll()` — kills every live terminal; called from `Bridge.Shutdown` so no shell/goroutine outlives the window.
- **Depends on:** `github.com/creack/pty`, stdlib (`os/exec`, `encoding/base64`, `sync`); `b.emit`.
- **Used by / entrypoint:** entrypoint — bound methods called from `lib/components/Terminal.svelte` (the Terminal tab in Chat's tools dock).

### internal/gui/worktree.go
- **Role:** Read-only right-panel tools — the working-tree DIFF of current changes and the FILE EXPLORER tree + file viewer; all on the host's fs/git directly.
- **Key symbols:**
  - `WorkingDiffDTO{Dir,Branch,Patch,Files []DiffFileDTO,IsRepo,Clean,Truncated}`, `DiffFileDTO{Path,Adds,Dels}`; `WorkingDiff(dir)` — bound; `git -C <dir> diff HEAD` (chosen over bare `git diff` so the patch + `--numstat` stats cover ALL pending changes — staged AND unstaged — vs HEAD), 8s ctx timeout, patch capped at `maxPatchBytes` (512 KiB, `Truncated` flagged); non-repo → `IsRepo:false` (not an error).
  - `FileTreeDTO{Dir,Entries []FileEntryDTO,Truncated}`, `FileEntryDTO{Name,Path(abs),IsDir,Children}`; `FileTree(dir)` — bound; depth-limited (~3) tree, dirs-first then name, skips VCS/build noise (.git/node_modules/vendor/dist/build/.svelte-kit/target/…), capped at ~2000 nodes.
  - `ReadFileForView(path)` — bound; a file's text for click-to-view (abs path, ~256 KiB cap, NUL-sniff rejects binary).
- **Depends on:** stdlib (`os/exec`, `os`, `path/filepath`, `context`); mirrors `internal/tool/tree.go` + `internal/tui/gitpanel.go` patterns (not imported).
- **Used by / entrypoint:** entrypoint — `DiffPanel.svelte` (WorkingDiff) + `FilesPanel.svelte` (FileTree/ReadFileForView) in Chat's tools dock.

### internal/gui/newchat.go
- **Role:** New-chat working-directory picker bridge — recent dirs + the native OS folder dialog, so a new session's root can be chosen before it's created (the primary root locks at creation).
- **Key symbols:**
  - `RecentDirDTO{Dir,Name}`; `RecentDirs() ([]RecentDirDTO, error)` — bound; distinct existing session-history dirs via `b.projectDirs()`, deduped, ephemeral dirs filtered (`isEphemeralDir` from memory.go, shared with the memory scope picker — drops `/tmp`/`agent-workspace`/`-itch-` scratch), capped at `recentDirsCap`(12), `Name=filepath.Base`.
  - `PickDirectory() (string, error)` — bound; opens the native folder dialog via `b.app.Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).AttachToWindow(b.app.Window.Current())`, defaulting to `defaultPickDir()` (home→cwd); returns "" on cancel (not an error), errors "no window" when `b.app` is nil.
- **Depends on:** `wailsapp/wails/v3/pkg/application` (DialogManager), stdlib; reuses `b.projectDirs()`.
- **Used by / entrypoint:** entrypoint — Chat's "+ New chat" popover (recents quick-pick + Browse… button).

### internal/gui/sessions_extra.go
- **Role:** Session-manager extras — transcript export to disk (List/Remove/Prune are bridged in bridge.go).
- **Key symbols:**
  - `ExportSession(id) (string, error)` — bound; exports a transcript to `~/eigen-exports/<id>-<stamp>.jsonl`. Branches on the id kind (mirroring the TUI fork): a daemon-persisted session lives under `daemon.PersistedTranscriptPath` (already eigen-native JSONL → `transcript.Load`+`transcript.Save`), otherwise it's a store id served via `session.Open`→`Discover`→`Export`.
  - `fileExists(path)` — reports whether a path is an existing regular file (picks the export branch).
  - `exportStamp()` — filename-safe timestamp helper (isolated so it's the only time-dependent line).
  - `safeFileID(id)` — sanitizes a session id for use in a filename.
- **Depends on:** `internal/session` (`Open`, `Discover`, `Export`), `internal/daemon` (`PersistedTranscriptPath`), `internal/transcript` (`Load`, `Save`).
- **Used by / entrypoint:** entrypoint — `ExportSession` bound, called from the Sessions view.

### internal/gui/remote.go
- **Role:** Remote-machines bridge; lists ssh-reachable targets (saved + `~/.ssh/config` aliases) locally, and lists sessions on a remote daemon over ssh on demand. Install deliberately NOT exposed.
- **Key symbols:**
  - `MachineDTO`, `MachinesDTO` (types) — one remote target (saved/detected flags) and the snapshot.
  - `remoteDialTimeout` (const, 10s) — bounds the read-only ssh peek so an unreachable host fails fast instead of blocking on the full daemon request timeout.
  - `Machines() (*MachinesDTO, error)` — bound; saved + ssh-config-detected targets (instant, local).
  - `RemoteSessions(target) ([]SessionInfoDTO, error)` — bound; dials over ssh to list a remote daemon's sessions (slow; drill-in only), capped at `remoteDialTimeout`.
  - `remoteSessions(ctx, target)` — the cancellable core: reimplements the read-only peek (rather than `remote.ListSessions`, which has no cancel hook) so the dial's `io.Closer` is reachable — on ctx cancel/deadline it kills the ssh process so a pending `List` unblocks and nothing leaks; on a list error it Closes first to flush remote stderr into the message.
  - `firstRemoteLine(s)` — first non-empty line of remote stderr (the actionable reason).
- **Depends on:** `internal/remote` (`Machine`, `Machines`, `Dial`), `internal/daemon` (`SessionInfo`); reuses `toSessionInfoDTO` from dto.go.
- **Used by / entrypoint:** entrypoint — bound methods called from the Remote/Machines view.

## Cross-links

- **`internal/daemon`** — the session-host daemon: the control client (request/response RPCs in bridge.go) + per-session streaming pumps (pump.go); also the source of `WireEvent`/`SessionInfo`/`SessionState`/`ToolInfo`/`ShellInfo`/`ApprovalInfo`/`DaemonStats` domain types DTO'd here, and `PersistedTranscriptPath` for export (sessions_extra.go). `DaemonStats` is the one type emitted RAW (snake_case), not DTO'd — see the parity contract in bridge.go.
- **`internal/llm`** — model catalog + provider credential checks (routing.go, config.go), `Message`/`ToolCall`/`Image`/`ModelInfo`/`Role`/`Provider` types (dto.go), `FullVersion` for the gui/daemon mismatch badge (bridge.go), the small-model ladders for skill-install scanning + on-demand dreaming (skills.go, dreaming.go), and the suggester provider built in `main_gui_wails.go`.
- **`internal/feed`** — proactive-feed scan/cache/dismiss + the `Suggester` injection point (feed.go, bridge.go).
- **`internal/memory`** — local memory stores for the Memory + Dreaming views (memory.go, dreaming.go), incl. the `Ban` blocks, USER.md user/learned split, and the `Pipeline`/`Index` driving `DreamNow`.
- **`internal/dream`** — the model-facing `Stage1`/`Consolidate`/`Summarize` callbacks `DreamNow` wires into a memory pipeline (dreaming.go).
- **`internal/transcript`** — eigen-native JSONL `Load`/`Save` for exporting a daemon-persisted session (sessions_extra.go).
- **`internal/command`** — project+user custom slash commands surfaced/run by `Commands`/`RunCommand` (bridge.go).
- **`internal/workflow`** — authored workflows listed/played by `Workflows`/`RunWorkflow` (bridge.go).
- **`internal/observe`** — local metadata-only event log for the observability dashboard (observe.go).
- **`internal/plugin`** — plugin/marketplace registry (plugins.go).
- **`internal/skill`** — SKILL.md discovery + dream proposals (skills.go).
- **`internal/agent`** — background/subtask records for the fan-out board (agents.go).
- **`internal/config`** — editable `~/.eigen/config.json` form (config.go).
- **`internal/session`** — local session store transcript export (sessions_extra.go).
- **`internal/remote`** — ssh-reachable machines + remote session listing (remote.go).
- **`internal/voice` / `internal/speech`** — server-side STT/TTS stack driven by the voice bridge: dictate, read-aloud, and the hands-free conversation loop (voice.go).
- **`github.com/creack/pty`** — the host-side PTY for the right-panel terminal tool (terminal.go); the GUI streams raw bytes to xterm.js on the frontend.
- **`wailsapp/wails/v3` DialogManager** — the native OS folder dialog for the new-chat working-dir picker (newchat.go `PickDirectory`).
- **`wailsapp/wails/v3/pkg/application`** — the Wails v3 service host: `*Bridge` is registered as a service, methods → generated TS bindings, `app.Event.Emit` pushes `eigen:*` events.
- **`main` (root: `main_gui_wails.go`)** — the entrypoint that constructs the bridge (`gui.NewBridge` with `ensureDaemon`/`guiSuggester`/`guiProjectDirs`), registers it as a Wails service, calls `SetApp`, and owns `Shutdown`.
- **`internal/gui/frontend`** — the Svelte 5 frontend; generated Go→TS bindings at `frontend/bindings/.../bridge.js` wrap every exported `*Bridge` method, consumed by `frontend/src/lib/bridge.ts` and the `*.svelte` views.
