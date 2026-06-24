# GUI views (B): Memory, Dreaming, Skills, Agents, Plugins, Profile, Config

> The "data + control" half of the Eigen desktop GUI's view layer. These seven
> Svelte 5 single-file components are full-page routes mounted by `App.svelte`'s
> `{#if router.route === …}` switch. Each one is a thin, read-mostly window onto
> a slice of agent state that lives on the local filesystem or in the daemon:
> durable memory (Memory), the memory-consolidation timeline (Dreaming), the
> SKILL.md capability gallery (Skills), the multi-agent fan-out board (Agents),
> the plugin/marketplace manager (Plugins), identity + lifetime usage (Profile),
> and the typed `config.json` editor (Config). They share one architecture: load
> a `*DTO` via the `Bridge` facade (`$lib/bridge`, which wraps Wails-generated
> bindings to Go `*Bridge` methods in `internal/gui/*.go`), hold it in Svelte
> runes (`$state`/`$derived`), guard against late async resolutions with a
> monotonic `loadSeq` token bumped in the `$effect` cleanup, and surface errors
> through the `toasts` store. Mutations (save note, accept skill, cancel agent,
> remove plugin, set config) round-trip back through the same bridge and
> re-`load()`. There is no client-side state store for these views — the DTO is
> the source of truth and is re-fetched after every write. The push-event-less
> views (Skills, Plugins) additionally re-`load()` on window focus / tab-visible
> so out-of-band changes (a CLI install, a dream-proposed skill) land without a
> manual reload; Agents instead polls on an adaptive cadence.

## Files

### internal/gui/frontend/src/views/Memory.svelte
- **Role:** Durable-notes browser for project/global scope: distilled summary, append-only notes (virtualized), ad-hoc manual saves, banned-behavior rules, MEMORY.md snapshot history, and (global only) the editable user profile split into an eigen-learned block + the user's own additions.
- **Key symbols:**
  - `load()` — fetches `Bridge.Memory()` into `data`, alive-guarded by `loadSeq`.
  - `saveNote()` — trims `draft`, calls `Bridge.AppendMemory(scope, note)`, then reloads. Cmd/Ctrl+Enter in the composer also fires it; an `$effect` autofocuses the `composeEl` textarea when `composing` flips on.
  - `addBan(scope,title,rule)` / `removeBan(title)` — `Bridge.AddBan` / `Bridge.RemoveBan` for banthis hard-prohibition rules; AddBan returns whether it replaced an existing same-title rule.
  - `loadBackups()` / `toggleBackups()` — lazily fetch `Bridge.MemoryBackups(scope)` (reversed to newest-first) on first reveal, alive-guarded by its own `backupsSeq`; a scope-switch `$effect` collapses the list and drops stale paths.
  - `backupName(path)` / `backupWhen(path)` — last path segment and a readable timestamp parsed from the `MEMORY.md.YYYYMMDD-HHMMSS.bak` filename.
  - `startProfile()` / `saveProfile()` — seed the profile editor from `current.profile`; save via `Bridge.WriteUserProfile`.
  - `shortDir(d)` — collapses a directory path to its last segment for the scope chip.
  - `current` (`$derived`) — the active `MemoryScopeDTO` (project vs global).
  - `bans` (`$derived`) — narrow-cast of `current.banList` (the typed `BanDTO[] = {title,rule}` the Go DTO carries but `types.ts` only types as the raw `bans` blob).
  - `isEmpty` (`$derived`) — scope with nothing injected (no summary, notes, ad-hoc, bans, or profile/profileLearned); `hasBackupHistory` (`$derived`) gates a distinct empty state when a consolidated-to-nothing scope still has snapshot history.
  - State runes: `data`, `scope`, `loading`, `composing`, `draft`, `saving`, `composeEl`, `editingProfile`, `profileDraft`, `savingProfile`, `addingBan`, `banTitle`, `banRule`, `savingBan`, `removingBan`, `backupsOpen`, `backupPaths`, `backupsLoading`.
- **Depends on:** `$lib/bridge` (`Memory`, `AppendMemory`, `AddBan`, `RemoveBan`, `MemoryBackups`, `WriteUserProfile`), `$lib/router.svelte` (`router` — the empty-state jump to Dreaming), `$lib/stores/toasts.svelte`, `$lib/types` (`MemoryDTO`, `MemoryScopeDTO`); components `Card`, `Button`, `Badge`, `Markdown`, `VirtualList`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Memory />` when `router.route === "memory"`.

### internal/gui/frontend/src/views/Dreaming.svelte
- **Role:** Memory-consolidation timeline with two strands per scope — per-session rollout summaries and consolidation snapshots; a consolidation opens a slide-over diffing that snapshot against current memory.
- **Key symbols:**
  - `load()` — fetches `Bridge.Dreaming()`; sets `error` on failure (alive-guarded).
  - `openDiff(c)` — fetches `Bridge.ConsolidationContent(c.path)` and `Bridge.CurrentMemory(scope)` in parallel, builds a unified diff for `DiffView`; on failure sets `diffError` (surfaced in-sheet with a Retry) and toasts.
  - `closeDiff()` / `onkeydown(e)` — slide-over teardown (clears `diffError` too); Escape closes the diff.
  - `outcomeTone(o)` — maps rollout outcome string to a `Badge` tone.
  - `relTime(ms)` — humanizes an epoch-ms timestamp ("3h ago").
  - `title(text)` — derives a one-line heading from the first non-blank markdown line.
  - `makeUnifiedDiff(before, after, aLabel, bLabel)` — **exported** from a `<script module>`; minimal O(n·m) LCS line diff producing a unified-diff string (kept local since memory files are small).
  - `current` (`$derived`) — active `DreamingScopeDTO`. State: `data`, `scope`, `strand`, `loading`, `error`, `openCons`, `diffPatch`, `diffLoading`, `diffError`.
- **Depends on:** `$lib/bridge` (`Dreaming`, `ConsolidationContent`, `CurrentMemory`), `$lib/stores/toasts.svelte`, `$lib/actions` (`trapFocus`), `$lib/types` (`DreamingDTO`, `DreamingScopeDTO`, `ConsolidationDTO`); components `Card`, `Button`, `Badge`, `Markdown`, `DiffView`, `VirtualList`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Dreaming />` when `router.route === "dreaming"`. `makeUnifiedDiff` is consumed only inside this file (line 67); not imported elsewhere.

### internal/gui/frontend/src/views/Skills.svelte
- **Role:** SKILL.md capability gallery — active skills grouped into source shelves (user/project/extra), a pinned "awaiting review" strip for dream-proposed drafts, an inline install control (local path or GitHub owner/repo), click-to-preview slide-over, with paging for large lists.
- **Key symbols:**
  - `load()` — fetches `Bridge.Skills()` (alive-guarded). A second `$effect` re-runs `load()` on `window` focus / `visibilitychange` (skipping while one is in flight) so a skill proposed/installed in another window lands without a manual reload; both listeners torn down on unmount.
  - `install()` / `onAddKey(e)` — install from `addMode` (`"path"` → `Bridge.InstallSkillFromPath`, `"github"` → `Bridge.InstallSkillFromGitHub`); on success toasts the installed name+path and reloads. `addPlaceholder` (`$derived`) swaps the input hint per mode. State: `addMode`, `addInput`, `installing`.
  - `preview(s)` / `closePreview()` — open slide-over, lazily fetch `Bridge.SkillBody(s.name)`.
  - `accept(name)` / `reject(name)` — `Bridge.AcceptSkill` / `Bridge.RejectSkill` on a proposal, with per-name in-flight guard `acting`, then reload.
  - `sourceTone(src)` — maps source to brand/info/neutral tint.
  - `onkeydown(e)` — Escape closes the preview; `onPropFocus(e)` scrolls a focused proposal card into the horizontal review band so keyboard/AT tabbing never leaves a control clipped.
  - `filtered` (`$derived.by`) — case-insensitive name/description filter on `query`.
  - `allShelves` (`$derived.by`) — stable grouping of ALL filtered skills by source in `SHELF_ORDER` (user→project→extra), with a fallback shelf for unknown sources; `shelves` (`$derived.by`) then pages across those stable shelves by walking an `activeShown` budget so "Show N more" fills the current shelf before spilling into the next.
  - `proposals` / `visibleProposals` — paged proposal slice; paging counters `PAGE = 24`, `proposalsShown`, `activeShown` (the latter reset to `PAGE` whenever `query` changes).
- **Depends on:** `$lib/bridge` (`Skills`, `SkillBody`, `AcceptSkill`, `RejectSkill`, `InstallSkillFromPath`, `InstallSkillFromGitHub`), `$lib/stores/toasts.svelte`, `$lib/actions` (`trapFocus`), `$lib/types` (`SkillsDTO`, `SkillDTO`); components `Card`, `Button`, `Badge`, `Markdown`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Skills />` when `router.route === "skills"`.

### internal/gui/frontend/src/views/Agents.svelte
- **Role:** Multi-agent fan-out board — polls persisted background subtasks (task/task_group delegations), showing live status (incl. a `canceling` interim), role/model/route, current tool + step count + last note, elapsed time, token totals, attempts/escalation, with cancel and a parsed-transcript slide-over.
- **Key symbols:**
  - `load()` — fetches `Bridge.Agents()`; updates `data`/`loading`/`error` (alive-guarded).
  - Polling `$effect` — self-scheduling `tick()` reading the cadence fresh after each load (1500 ms while `data.running > 0`, 4000 ms idle) without re-running the effect; cleanup clears the timeout and bumps `loadSeq`.
  - `tone(s)` / `elapsed(t)` — status→Badge tone (`lost` folds into error, `canceling`→warn); live elapsed from the shared `now` clock (uses `finishedMs` once terminal).
  - `cancel(id)` — `Bridge.CancelAgent(id)` with per-id `acting` guard, then reload.
  - `openTranscript(t)` / `closeTranscript()` / `onkeydown(e)` — slide-over; lazily fetch `Bridge.AgentTranscript(t.id)`.
  - `txEntries` (`$derived.by`) — parses the transcript `.jsonl` line-by-line into `TxEntry` cards (role/text/reasoning/toolCalls/toolName/toolError), guarding `JSON.parse` per line so a corrupt line degrades to a verbatim `raw` entry; helpers `str`, `asArgs`, `field` (case-tolerant Go `llm.Message` key reader). `txTotal` / `txShown` / `txElided` tail-bound the render to `TX_MAX = 200` cards; `roleTone(role)` tints each. Falls back to a `CodeBlock` of the raw blob if parsing yields nothing usable.
  - `tasks` (`$derived.by`) — applies the `all/running/done/error` filter (errored folds in `lost`).
  - `filters` — the filter tab table. State: `data`, `loading`, `error`, `filter`, `openTask`, `transcript`, `transcriptLoading`, `acting`.
- **Depends on:** `$lib/bridge` (`Agents`, `CancelAgent`, `AgentTranscript`), `$lib/stores/toasts.svelte`, `$lib/stores/clock.svelte` (`now`), `$lib/status` (`taskDot`), `$lib/actions` (`trapFocus`), `$lib/types` (`AgentsDTO`, `BgTaskDTO`); components `Card`, `Button`, `Badge`, `StatusDot`, `CodeBlock`, `VirtualList`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Agents />` when `router.route === "agents"`.

### internal/gui/frontend/src/views/Plugins.svelte
- **Role:** Manager for installed plugins (wired-in skills/agents/mcp/commands/hooks counts, version, source marketplace, install date, install-scan status with an expandable per-component findings list) and configured marketplaces (enable/disable/remove). Install is intentionally CLI-only; destructive actions (uninstall, marketplace-remove) take an inline confirm.
- **Key symbols:**
  - `load()` — fetches `Bridge.Plugins()` (alive-guarded); also clears any dangling inline `confirming`. A second `$effect` re-runs it on `window` focus / `visibilitychange` (skip while in flight) so a CLI-installed plugin lands without restart; listeners torn down on unmount.
  - `removePlugin(name)` — `Bridge.RemovePlugin`, keyed `acting["p:"+name]`, then reload.
  - `toggleMarket(name, enabled)` — `Bridge.SetMarketEnabled`, keyed `acting["m:"+name]`, then reload.
  - `removeMarket(name)` — `Bridge.RemoveMarketplace`, then reload.
  - `toggleScans(name)` — adds/removes a plugin name in the `expandedScans` `SvelteSet`, toggling its forced-install scan-findings disclosure.
  - `components(p)` — builds the count chips for the components a plugin wired in; `consequence(p)` turns those into the uninstall blast-radius confirm string ("Remove 3 skills, 2 agents?").
  - `scanTone(s)` — maps scan status (clean/forced/other) to a Badge/border tone; `relMs(ms)` scales unix-ms `installedMs`/`addedMs` to nanos for the shared `relTime`, rendering nothing for a 0 stamp.
  - State: `data`, `loading`, `error`, `acting`, `confirming` (key of the row awaiting confirm — `p:`/`m:` prefixed), `expandedScans`.
- **Depends on:** `$lib/bridge` (`Plugins`, `RemovePlugin`, `SetMarketEnabled`, `RemoveMarketplace`), `$lib/status` (`relTime`), `svelte/reactivity` (`SvelteSet`), `$lib/stores/toasts.svelte`, `$lib/types` (`PluginsDTO`, `InstalledPluginDTO` — incl. `version`/`marketplace`/`scanCount`/`scans: ScanFindingDTO[]`/`warnings`/`installedMs`, and `MarketplaceDTO.owner`/`addedMs`); components `Card`, `Button`, `Badge`, `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Plugins />` when `router.route === "plugins"`. (Note: a `Bridge.SetPluginEnabled` Go method exists but is not yet wired into this view.)

### internal/gui/frontend/src/views/Profile.svelte
- **Role:** Identity + usage page — lifetime usage KPIs (turns, tokens in/out, cache-hit %, errors) summed from the DURABLE `ObserveSummary` log so they survive daemon restarts, the one volatile counter (session count, labeled "since daemon start") from the live `daemon.stats` stream, a top-models table, and the global USER.md profile editor (same backing file as Memory's global-scope editor).
- **Key symbols:**
  - `load()` — fires `Bridge.ObserveSummary(5000)` and `Bridge.Memory()` independently (so a slow log read doesn't block the fast local memory read), each alive-guarded by one shared `loadSeq`. The summary catch sets `summaryError` (a distinct in-section failure surface with Retry — never turns=0/errors=0, which would read as a clean log); the memory catch toasts.
  - `startEdit()` / `cancelEdit()` / `save()` — profile editor; `save()` calls `Bridge.WriteUserProfile(draft)` then reloads.
  - `k(n)` — compact number formatter (k/M).
  - `modelTotals` (`$derived`) — reduces `summary.models[*]` to lifetime in/out/cacheRead totals; `inTokens`/`outTokens`/`cacheHit`/`turns`/`errorCount` derive from it + `summary.records`/`summary.errors`. `sessionCount` is the volatile `stats.sessions`; `profile` reads `memory.global.profile`; `stats` is `$derived(daemon.stats)`.
  - State: `summary`, `summaryLoading`, `summaryError`, `memory`, `memoryLoading`, `editing`, `draft`, `saving`.
- **Depends on:** `$lib/stores/daemon.svelte` (`daemon`), `$lib/bridge` (`ObserveSummary`, `Memory`, `WriteUserProfile`), `$lib/stores/toasts.svelte`, `$lib/types` (`ObserveSummaryDTO`, `MemoryDTO`); components `Card`, `Button`, `Badge`, `Markdown`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Profile />` when `router.route === "profile"`.

### internal/gui/frontend/src/views/Config.svelte
- **Role:** Typed form over `~/.eigen/config.json` — each field renders by its option shape (boolean toggle / select / space-separated chip multi-select for `route_providers` / free text-number input); every change validates and persists via the bridge, reverting on rejection.
- **Key symbols:**
  - `load()` — fetches `Bridge.Config()`, seeds the editable `values` map from field values; guarded by both `alive` and `loadSeq` (so late commits can't fire toasts after nav-away).
  - `commit(key, value)` — `Bridge.SetConfig`, stores the returned canonical value or reverts to stored value and toasts the error.
  - `isBool(f)` — detects a boolean field (options are exactly `["true","false"]`).
  - `allowsEmpty(f)` — narrow-cast read of the bridge's `allowEmpty` flag (fields where `""` is a real, reachable choice, e.g. `judge_model`/`model`); drives an extra `(automatic)`/clear option in the select.
  - `toggleMulti(f, opt)` / `multiHas(key, opt)` — add/remove an option in the space-separated multi-select set (guarded by the field's `multi` flag); `toggleMulti` flips the chip optimistically, with `commit` reverting on reject.
  - `commitIfChanged(f)` — commits a text/number field on blur/Enter only when it differs from stored.
  - State: `data`, `loading`, `error`, `saving` (per-key, shown as an inline spinner+label that also disables the control), `values`, `alive`, `loadSeq`.
- **Depends on:** `$lib/bridge` (`Config`, `SetConfig`), `$lib/stores/toasts.svelte`, `$lib/types` (`ConfigDTO`, `ConfigFieldDTO`); components `Card`, `Button`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Config />` when `router.route === "config"`.

## Cross-links
- **`internal/gui/frontend/src/lib/bridge.ts`** — the `Bridge` facade every view calls; wraps the Wails-generated TS bindings.
- **`internal/gui` (Go `*Bridge`)** — backing Wails methods: `memory.go` (Memory/AppendMemory/AddBan/RemoveBan/WriteUserProfile/MemoryBackups), `dreaming.go` (Dreaming/ConsolidationContent/CurrentMemory), `skills.go` (Skills/SkillBody/AcceptSkill/RejectSkill/InstallSkillFromPath/InstallSkillFromGitHub, returning `SkillInstallDTO`), `agents.go` (Agents/CancelAgent/AgentTranscript), `plugins.go` (Plugins/SetMarketEnabled/RemoveMarketplace/SetPluginEnabled/RemovePlugin), `observe.go` (ObserveSummary), `config.go` (Config/SetConfig). DTO shapes live in `dto.go`.
- **`internal/gui/frontend/src/lib/types.ts`** — shared DTO type definitions (`MemoryDTO`/`MemoryScopeDTO` (`profile`, `profileLearned`, raw `bans`), `DreamingDTO`, `SkillsDTO`/`SkillDTO`, `AgentsDTO`/`BgTaskDTO`, `PluginsDTO`/`InstalledPluginDTO`/`MarketplaceDTO`/`ScanFindingDTO`, `ObserveSummaryDTO`, `ConfigDTO`/`ConfigFieldDTO` (`multi`), …) mirrored from the Go side. Memory's `banList` and Config's `allowEmpty` are carried by the Go DTO but not yet typed here, so those views narrow-cast locally.
- **`internal/gui/frontend/src/App.svelte`** — the router/outlet that mounts each of these views by `router.route`.
- **`$lib/stores`** — `toasts.svelte` (error/success/info notifications, all views), `daemon.svelte` (live stats stream, Profile), `clock.svelte` (`now` tick for Agents' live elapsed).
- **`$lib/components`** — shared UI primitives: `Card`, `Button`, `Badge`, `Markdown`, `EmptyState`, `VirtualList`, `DiffView`, `StatusDot`, `CodeBlock`.
- **`$lib/actions`** (`trapFocus`) and **`$lib/status`** (`taskDot` for Agents, `relTime` for Plugins) — focus-trap for slide-overs, the agent status→dot mapping, and the nanos-based relative-time formatter.
- **GUI views (A)** (Home / Chat / Observe / Live / Sessions / Routing / Machines / Crons) — sibling views in the same router switch; Profile's usage table mirrors Observe's table styling and shares `ObserveSummary`.
