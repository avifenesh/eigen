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
> the source of truth and is re-fetched after every write.

## Files

### internal/gui/frontend/src/views/Memory.svelte
- **Role:** Durable-notes browser for project/global scope: distilled summary, append-only notes (virtualized), ad-hoc manual saves, bans, and (global only) the editable user profile.
- **Key symbols:**
  - `load()` — fetches `Bridge.Memory()` into `data`, alive-guarded by `loadSeq`.
  - `saveNote()` — trims `draft`, calls `Bridge.AppendMemory(scope, note)`, then reloads.
  - `startProfile()` / `saveProfile()` — seed the profile editor from `current.profile`; save via `Bridge.WriteUserProfile`.
  - `shortDir(d)` — collapses a directory path to its last segment for the scope chip.
  - `current` (`$derived`) — the active `MemoryScopeDTO` (project vs global).
  - State runes: `data`, `scope`, `loading`, `composing`, `draft`, `saving`, `editingProfile`, `profileDraft`, `savingProfile`.
- **Depends on:** `$lib/bridge` (`Memory`, `AppendMemory`, `WriteUserProfile`), `$lib/stores/toasts.svelte`, `$lib/types` (`MemoryDTO`, `MemoryScopeDTO`); components `Card`, `Button`, `Badge`, `Markdown`, `VirtualList`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Memory />` when `router.route === "memory"`.

### internal/gui/frontend/src/views/Dreaming.svelte
- **Role:** Memory-consolidation timeline with two strands per scope — per-session rollout summaries and consolidation snapshots; a consolidation opens a slide-over diffing that snapshot against current memory.
- **Key symbols:**
  - `load()` — fetches `Bridge.Dreaming()`; sets `error` on failure (alive-guarded).
  - `openDiff(c)` — fetches `Bridge.ConsolidationContent(c.path)` and `Bridge.CurrentMemory(scope)` in parallel, builds a unified diff for `DiffView`.
  - `closeDiff()` / `onkeydown(e)` — slide-over teardown; Escape closes the diff.
  - `outcomeTone(o)` — maps rollout outcome string to a `Badge` tone.
  - `relTime(ms)` — humanizes an epoch-ms timestamp ("3h ago").
  - `title(text)` — derives a one-line heading from the first non-blank markdown line.
  - `makeUnifiedDiff(before, after, aLabel, bLabel)` — **exported** from a `<script module>`; minimal O(n·m) LCS line diff producing a unified-diff string (kept local since memory files are small).
  - `current` (`$derived`) — active `DreamingScopeDTO`. State: `data`, `scope`, `strand`, `loading`, `error`, `openCons`, `diffPatch`, `diffLoading`.
- **Depends on:** `$lib/bridge` (`Dreaming`, `ConsolidationContent`, `CurrentMemory`), `$lib/stores/toasts.svelte`, `$lib/actions` (`trapFocus`), `$lib/types` (`DreamingDTO`, `DreamingScopeDTO`, `ConsolidationDTO`); components `Card`, `Button`, `Badge`, `Markdown`, `DiffView`, `VirtualList`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Dreaming />` when `router.route === "dreaming"`. `makeUnifiedDiff` is consumed only inside this file (line 67); not imported elsewhere.

### internal/gui/frontend/src/views/Skills.svelte
- **Role:** SKILL.md capability gallery — active skills grouped into source shelves (user/project/extra), a pinned "awaiting review" strip for dream-proposed drafts, click-to-preview slide-over, with paging for large lists.
- **Key symbols:**
  - `load()` — fetches `Bridge.Skills()` (alive-guarded).
  - `preview(s)` / `closePreview()` — open slide-over, lazily fetch `Bridge.SkillBody(s.name)`.
  - `accept(name)` / `reject(name)` — `Bridge.AcceptSkill` / `Bridge.RejectSkill` on a proposal, with per-name in-flight guard `acting`, then reload.
  - `sourceTone(src)` — maps source to brand/info/neutral tint.
  - `onkeydown(e)` — Escape closes the preview.
  - `filtered` (`$derived.by`) — case-insensitive name/description filter on `query`.
  - `shelves` (`$derived.by`) — buckets visible active skills by source in `SHELF_ORDER`, with a fallback shelf for unknown sources.
  - `proposals` / `visibleProposals` / `visibleActive` — paged slices (`PAGE = 24`, `proposalsShown`, `activeShown`).
- **Depends on:** `$lib/bridge` (`Skills`, `SkillBody`, `AcceptSkill`, `RejectSkill`), `$lib/stores/toasts.svelte`, `$lib/actions` (`trapFocus`), `$lib/types` (`SkillsDTO`, `SkillDTO`); components `Card`, `Button`, `Badge`, `Markdown`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Skills />` when `router.route === "skills"`.

### internal/gui/frontend/src/views/Agents.svelte
- **Role:** Multi-agent fan-out board — polls persisted background subtasks (task/task_group delegations), showing live status, current tool, elapsed time, tokens, with cancel and a transcript slide-over.
- **Key symbols:**
  - `load()` — fetches `Bridge.Agents()`; updates `data`/`loading`/`error` (alive-guarded).
  - Polling `$effect` — self-scheduling `tick()` with adaptive cadence (1500 ms while running, 4000 ms idle); cleanup clears the timeout and bumps `loadSeq`.
  - `tone(s)` / `elapsed(t)` — status→Badge tone; live elapsed from the shared `now` clock (uses `finishedMs` once terminal).
  - `cancel(id)` — `Bridge.CancelAgent(id)` with per-id `acting` guard, then reload.
  - `openTranscript(t)` / `closeTranscript()` / `onkeydown(e)` — slide-over; lazily fetch `Bridge.AgentTranscript(t.id)`.
  - `tasks` (`$derived.by`) — applies the `all/running/done/error` filter (errored folds in `lost`).
  - `filters` — the filter tab table. State: `data`, `loading`, `error`, `filter`, `openTask`, `transcript`, `transcriptLoading`, `acting`.
- **Depends on:** `$lib/bridge` (`Agents`, `CancelAgent`, `AgentTranscript`), `$lib/stores/toasts.svelte`, `$lib/stores/clock.svelte` (`now`), `$lib/status` (`taskDot`), `$lib/actions` (`trapFocus`), `$lib/types` (`AgentsDTO`, `BgTaskDTO`); components `Card`, `Button`, `Badge`, `StatusDot`, `CodeBlock`, `VirtualList`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Agents />` when `router.route === "agents"`.

### internal/gui/frontend/src/views/Plugins.svelte
- **Role:** Manager for installed plugins (showing wired-in skills/agents/mcp/commands/hooks and install-scan status) and configured marketplaces (enable/disable/remove). Install is intentionally CLI-only; destructive actions take an inline confirm.
- **Key symbols:**
  - `load()` — fetches `Bridge.Plugins()` (alive-guarded).
  - `removePlugin(name)` — `Bridge.RemovePlugin`, keyed `acting["p:"+name]`, then reload.
  - `toggleMarket(name, enabled)` — `Bridge.SetMarketEnabled`, then reload.
  - `removeMarket(name)` — `Bridge.RemoveMarketplace`, then reload.
  - `components(p)` — builds the count chips for the components a plugin wired in.
  - `scanTone(s)` — maps scan status (clean/forced/other) to a Badge/border tone.
  - State: `data`, `loading`, `error`, `acting`, `confirming` (key of the row awaiting confirm).
- **Depends on:** `$lib/bridge` (`Plugins`, `RemovePlugin`, `SetMarketEnabled`, `RemoveMarketplace`), `$lib/stores/toasts.svelte`, `$lib/types` (`PluginsDTO`, `InstalledPluginDTO`); components `Card`, `Button`, `Badge`, `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Plugins />` when `router.route === "plugins"`.

### internal/gui/frontend/src/views/Profile.svelte
- **Role:** Identity + usage page — lifetime usage KPIs stitched from the live `daemon.stats` stream plus the historical `ObserveSummary` log, a top-models table, and the global USER.md profile editor (same backing file as Memory's global-scope editor).
- **Key symbols:**
  - `load()` — fires `Bridge.ObserveSummary(5000)` and `Bridge.Memory()` independently (so a slow log read doesn't block the fast local memory read), each alive-guarded by one shared `loadSeq`.
  - `startEdit()` / `cancelEdit()` / `save()` — profile editor; `save()` calls `Bridge.WriteUserProfile(draft)` then reloads.
  - `k(n)` — compact number formatter (k/M).
  - Usage `$derived` rollups: `inTokens`, `outTokens`, `cacheHit`, `turns`, `errorCount`, `sessionCount`, `profile`; `stats` is `$derived(daemon.stats)`.
- **Depends on:** `$lib/stores/daemon.svelte` (`daemon`), `$lib/bridge` (`ObserveSummary`, `Memory`, `WriteUserProfile`), `$lib/stores/toasts.svelte`, `$lib/types` (`ObserveSummaryDTO`, `MemoryDTO`); components `Card`, `Button`, `Badge`, `Markdown`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Profile />` when `router.route === "profile"`.

### internal/gui/frontend/src/views/Config.svelte
- **Role:** Typed form over `~/.eigen/config.json` — each field renders by its option shape (boolean toggle / select / space-separated chip multi-select for `route_providers` / free text-number input); every change validates and persists via the bridge, reverting on rejection.
- **Key symbols:**
  - `load()` — fetches `Bridge.Config()`, seeds the editable `values` map from field values; guarded by both `alive` and `loadSeq` (so late commits can't fire toasts after nav-away).
  - `commit(key, value)` — `Bridge.SetConfig`, stores the returned canonical value or reverts to stored value and toasts the error.
  - `isBool(f)` — detects a boolean field (options are exactly `["true","false"]`).
  - `toggleMulti(f, opt)` / `multiHas(key, opt)` — add/remove a provider in the space-separated multi-select set.
  - `commitIfChanged(f)` — commits a text/number field on blur/Enter only when it differs from stored.
  - State: `data`, `loading`, `error`, `saving` (per-key), `values`, `alive`, `loadSeq`.
- **Depends on:** `$lib/bridge` (`Config`, `SetConfig`), `$lib/stores/toasts.svelte`, `$lib/types` (`ConfigDTO`, `ConfigFieldDTO`); components `Card`, `Button`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Config />` when `router.route === "config"`.

## Cross-links
- **`internal/gui/frontend/src/lib/bridge.ts`** — the `Bridge` facade every view calls; wraps the Wails-generated TS bindings.
- **`internal/gui` (Go `*Bridge`)** — backing Wails methods: `memory.go` (Memory/AppendMemory/WriteUserProfile/MemoryBackups), `dreaming.go` (Dreaming/ConsolidationContent/CurrentMemory), `skills.go` (Skills/SkillBody/AcceptSkill/RejectSkill), `agents.go` (Agents/CancelAgent/AgentTranscript), `plugins.go` (Plugins/SetMarketEnabled/RemoveMarketplace/RemovePlugin), `observe.go` (ObserveSummary), `config.go` (Config/SetConfig).
- **`internal/gui/frontend/src/lib/types.ts`** — shared DTO type definitions (`MemoryDTO`, `DreamingDTO`, `SkillsDTO`, `AgentsDTO`, `PluginsDTO`, `ObserveSummaryDTO`, `ConfigDTO`, …) mirrored from the Go side.
- **`internal/gui/frontend/src/App.svelte`** — the router/outlet that mounts each of these views by `router.route`.
- **`$lib/stores`** — `toasts.svelte` (error/success/info notifications, all views), `daemon.svelte` (live stats stream, Profile), `clock.svelte` (`now` tick for Agents' live elapsed).
- **`$lib/components`** — shared UI primitives: `Card`, `Button`, `Badge`, `Markdown`, `EmptyState`, `VirtualList`, `DiffView`, `StatusDot`, `CodeBlock`.
- **`$lib/actions`** (`trapFocus`) and **`$lib/status`** (`taskDot`) — focus-trap for slide-overs and the agent status→dot mapping.
- **GUI views (A)** (Home / Chat / Observe / Live / Sessions / Routing / Machines / Crons) — sibling views in the same router switch; Profile's usage table mirrors Observe's table styling and shares `ObserveSummary`.
