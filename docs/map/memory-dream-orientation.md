# memory/, dream/, orientation/

> This slice is Eigen's **long-term knowledge layer** — how a terminal-first Go
> coding agent gets better at a project over time. **`internal/memory`** is the
> durable store: per-scope tiered markdown directories under `~/.eigen/memory/`
> (curated `MEMORY.md`, a small injected `memory_summary.md`, hard `bans.md`, a
> global `USER.md` profile, append-only `rollout_summaries/`, manual `ad_hoc/`
> notes) plus a pure-Go SQLite **index** that tracks per-session Stage1 outputs
> and drives a leased background **job queue** (stage1 → consolidate → summary).
> **`internal/dream`** is the model-facing "reflection" process: it turns
> transcripts into structured per-session summaries (Stage1), consolidates them
> plus ad-hoc notes into a smaller `MEMORY.md` (Phase 2), distils the small
> injected summary, builds the cross-project user profile, and proposes reusable
> skills. The two are decoupled by callback seams on `memory.Pipeline` so memory
> never imports dream (no cycle); callers (TUI idle "dream", daemon nightly tick,
> `eigen dream`, GUI dreaming page) wire dream's functions into the pipeline.
> **`internal/orientation`** is an independent native history/provenance harness:
> a hook ingests Eigen session transcripts into a per-project JSON episode graph
> under `~/.eigen/orientation`, and a small CLI answers "who touched this file,
> is it in-flight or stale, what's coupled to it" — feeding judgement about code
> not written this session.

## Files

### internal/memory/memory.go
- **Role:** The `Store` type — one memory scope (global or per-project) backed by a tiered directory; all read/write/inject/backup/ban/profile logic.
- **Key symbols:**
  - `Store` — one scope; fields `dir`, `global`.
  - `Open(projectDir)` / `OpenGlobal()` — return the project / cross-project store (project keyed by abs-path hash); each runs `migrateFlat`.
  - `migrateFlat(flat)` — one-time non-destructive migration of a pre-v2 flat `<key>.md` into `<dir>/MEMORY.md` (renames old file, moves `.bak`s).
  - Path accessors: `Dir`, `MemoryPath`/`Path`, `SummaryPath`, `legacySummaryPath`, `BansPath`, `UserProfilePath`, `RawMemoriesPath`, `RawDir`, `legacyRawDir`, `ExtensionsDir`, `AdHocDir`, `AdHocNotesDir`, `adHocInstructionsPath`; `IsGlobal`, `ensureDir`.
  - `Snapshot()` / `Backups()` / `pruneBackups()` — timestamped `.bak` of `MEMORY.md`, capped at `maxBackups`(10); the safety net for consolidation rewrites.
  - `Rewrite(content)` — atomic replace of `MEMORY.md` (snapshots first).
  - `Read()` / `readFile(p)` — read curated memory / any scope file.
  - `UserProfile()` / `WriteUserProfile(content)` — read/atomically-write the global `USER.md` (redacted; empty removes).
  - `Append(note)` → `AddAdHocNote(note, when)` — record a manual save as a redacted ad-hoc note, then `enqueueMaintenance()` (queues consolidate+summary jobs); `ensureAdHocInstructions()` writes a "treat as data" README once.
  - `Bans()`, `ListBans()`, `AddBan(title,rule)`, `RemoveBan(title)`, `writeBans()` — the native "banthis" layer: hard prohibitions as `### title` blocks in `bans.md`; `Ban` struct.
  - Injection: `Injected()` (returns `memory_summary.md`, else legacy `SUMMARY.md`, else `MEMORY.md`, each clamped to `maxInjectedBytes`=8 KiB), `clampMemoryTail()` (keeps newest tail at a line boundary), `Section()` (frames memory as stale-data + bans as system-priority), and package func `Sections(global, project)` (global-then-project for the prompt).
  - Workspace ops: `ListFiles()`, `ReadRelative(rel)` (path-traversal guarded), `Search(query,limit)` → `[]SearchHit`.
  - Tiers for the pipeline: `WriteRollout(slug,body,when)`, `RawSummaries(limit)` (reads `rollout_summaries/` + legacy `raw/`), `AdHocNotes(limit)`, `WriteRawMemories(content)`, `writeSummary(content)`.
  - Helpers: `baseDir()`, `key(abs)` (base name + sha1[:8]), `slugify`.
- **Depends on:** stdlib only (crypto/sha1, os, filepath, regexp, sort) + sibling files (`Redact` from redact.go, `OpenIndex`/`Enqueue`/job consts from index.go/pipeline.go).
- **Used by:** `main.go` (memory cmd, prompt build), `daemon.go`, `build.go` (chat session prompt), `internal/tui/tui.go`, `internal/gui/memory.go` + `dreaming.go`, `internal/feed/{memory,suggest}.go`, `internal/app/data.go`, `remote_session.go`, `internal/tool/memory.go` (via `MemoryStore`/`memoryReader`/`memorySearcher` interfaces).

### internal/memory/index.go
- **Role:** `Index` — the pure-Go SQLite bookkeeping store (`~/.eigen/memory/index.sqlite`): per-session Stage1 outputs, legacy per-session summaries, and the leased job queue; plus best-effort git versioning of the whole memory tree.
- **Key symbols:**
  - `Index` (mutex + `*sql.DB`), `IndexPath()`, `OpenIndex()` (opens WAL, `SetMaxOpenConns(1)`, migrates), `Close()`, `migrate()` / `ensureColumn()` (creates `summaries`, `jobs`, `stage1_outputs` tables + indexes; adds missing columns).
  - Stage1: `Stage1Output` struct; `RecordStage1Output(r)` (upsert, newer `source_updated_at` wins), `UpdateStage1RolloutPath()`, `Stage1Summarized()` (idempotency check), `Stage1Outputs(scope,limit)` (lister), `Phase2Inputs(scope,limit)` (selects consolidation inputs, favoring un-selected then recent/used), `MarkSelectedForPhase2(rows)`.
  - Legacy summaries: `SummaryRow` struct, `RecordSummary(r)`, `Summarized()` (delegates to `Stage1Summarized`, then legacy table), `BumpUsage(scope, ids...)` (the forgetting signal), `Summaries(scope)` (reads `stage1_outputs`, falls back to legacy `summaries`).
  - Jobs queue: `Job` struct; `Enqueue`/`EnqueueWatermark` (deduped by kind+scope+job_key, watermark-aware re-pend), `Claim(leaseSecs)`/`ClaimScope(scope,leaseSecs)`/`claim()` (leases one pending/lease-expired job, kind-priority ordering), `Finish(j, err)` (done or retry/error with `retry_remaining`), `workerID()`, `truncErr()`.
  - Git: `CommitMemory(message)` (init-on-first-use local git, gitignores the sqlite index; never pushed), `runGit()`.
- **Depends on:** stdlib + `modernc.org/sqlite` (cgo-free; ships in the static binary). Job-kind constants come from pipeline.go.
- **Used by:** `internal/memory/pipeline.go` (`ClaimScope`, `Phase2Inputs`, `RecordStage1Output`, `MarkSelectedForPhase2`, etc.); `Store.enqueueMaintenance`; `main.go`/`daemon.go`/`internal/tui/tui.go` open it for dream pipelines; `CommitMemory` called from `main.go`, `daemon.go`, `pipeline.Run`.

### internal/memory/pipeline.go
- **Role:** `Pipeline` — orchestrates the memory generation stages over a scope (stage1 → materialize rollouts → consolidate `MEMORY.md` → regen injected summary → git commit), with the model-facing steps injected as callbacks so memory needn't import dream.
- **Key symbols:**
  - Job-kind consts (`JobStage1`/`JobConsolidate`/`JobSummary`), `scopeJobKey`, chunking limits.
  - `Pipeline` struct: `Store`, `Index`, callbacks `Stage1`/`Consolidate`/`Summarize`, size knobs `ConsolidateBytes`/`Phase2ChunkBytes`; `Stage1Result`, `Session` structs.
  - `scopeKey()` / `baseName(dir)` — readable index scope ("global" or dir base name).
  - `Stage1Sessions(ctx, sessions)` — summarize each new session (skip already-summarized via watermark; a flaky `ok=false` skip is NOT persisted), redact + record to SQLite, materialize rollout markdown, enqueue downstream jobs.
  - `RunQueued(ctx, maxJobs)` — drain this scope's consolidate/summary jobs via `ClaimScope` + `Finish`.
  - `MaybeConsolidate(ctx, force)` — rewrite `MEMORY.md` when over threshold (default 24 KB) or forced; builds `phase2Input()` (current memory + Stage1 raw memories + ad-hoc notes), writes raw scratchpad, runs `consolidatePhase2`, rewrites, marks selected.
  - `consolidatePhase2` / `consolidatePhase2Chunked` / `splitPhase2Chunks` / `splitAtRuneBoundary` — bound each consolidate call by chunk size (recursive map-reduce up to `maxPhase2ChunkDepth`=4) so the callback's shrink guard stays meaningful.
  - `RegenSummary(ctx)` — regenerate `memory_summary.md` from `MEMORY.md` via the `Summarize` callback.
  - `Run(ctx, sessions)` — the full best-effort per-scope dream; `itoa(n)` helper.
- **Depends on:** the `Store`/`Index` siblings + `Redact`; takes dream functions as callbacks (no import of dream).
- **Used by:** constructed in `main.go` (`newMemoryPipeline`), `internal/tui/tui.go` (`newTUIDreamPipeline`), and `daemon.go`'s nightly dreamer — each wiring `dream.Stage1`/`Consolidate`/`Summarize`.

### internal/memory/redact.go
- **Role:** Secret scrubbing — memory is plaintext on disk and injected into every future prompt, so credential-looking tokens are redacted before storage.
- **Key symbols:** `Redacted` placeholder const; regexes `tokenPatterns` (AWS/GitHub/`sk-`/xAI/Slack/Google), `assignPattern` (api_key=/token:/password=… value scrubbed, name kept), `authHeaderPattern` (Bearer/Basic), `pemBlock` (inline PEM private keys); `Redact(s)` applies them all.
- **Depends on:** stdlib `regexp` only.
- **Used by:** `Store.AddAdHocNote`, `WriteUserProfile`, `AddBan` (memory.go); `Pipeline.Stage1Sessions` redacts Stage1 output before recording; exported as `memory.Redact` (e.g. `internal/app/pages.go` consolidation path).

### internal/dream/dream.go
- **Role:** Package doc + the reflection prompts/parsers for free-form note distillation and skill synthesis.
- **Key symbols:**
  - `reflectPrompt` const + `Distill(ctx, p, transcripts, existing)` — extract durable bulleted notes from sessions, deduped against existing memory.
  - `parseBullets(s)` / `dedupe(notes, existing)` (case-insensitive substring drop) / `RenderSession(msgs)` (flatten messages to text).
  - `synthPrompt` const + `SkillDraft` struct + `SynthesizeSkill(ctx, p, transcripts)` — propose a reusable skill only when a recurring workflow recurs; `parseSkillDraft(s)` parses the `NAME:/DESCRIPTION:/BODY:` block (`ok=false` on `NONE`).
  - `maxReflectInput` (60000) bounds input.
- **Depends on:** `internal/llm` (`Provider`, `Request`, `Message`, `RoleUser`).
- **Used by:** `daemon.go` (`RenderSession`, `SynthesizeSkill`, `DistillGlobal`), `main.go` (`runDream`: `RenderSession`, `SynthesizeSkill`, `DistillGlobal`), `internal/tui/tui.go` (`RenderSession`).

### internal/dream/consolidate.go
- **Role:** Phase 2 consolidation — the memory-writing prompt + destructive-output guards.
- **Key symbols:** `consolidatePrompt` (recency-wins, merge-duplicates, preserve-precision, user-rules-outrank, keep `[REDACTED_SECRET]`); `maxConsolidateInput` (120k); `Consolidate(ctx, p, current)` — rewrites memory and **fails closed**: refuses empty/unstructured output or a >90% shrink (>99% for sectional Phase 2 chunk/merge inputs, via `isSectionalPhase2Input`).
- **Depends on:** `internal/llm`.
- **Used by:** wired as the `Pipeline.Consolidate` callback (`main.go`, `internal/tui/tui.go`); called directly in `main.go` (`runMemoryCmd` consolidate) and `internal/app/pages.go`.

### internal/dream/stage1.go
- **Role:** Per-session (S1) reflection — the structured rollout-summary prompt, the `RolloutSummary` data model, its renderers, and the parser.
- **Key symbols:** `stage1Prompt` (TITLE/OUTCOME/PREFERENCES/KEY/FAILURES/REUSABLE, min-signal `skip` gate, transcript-is-data); `RolloutSummary` struct + `Empty()` (skip/no-sections), `Markdown(sessionID,when)` (rollout file body), `RawMemory(sessionID,when)` (denser candidate for `stage1_outputs.raw_memory`), `Slug()`; `Stage1(ctx, p, transcript)` (returns summary + `ok`); `parseRollout(s)`; `nonEmpty`, `slugStrip`.
- **Depends on:** `internal/llm`.
- **Used by:** wired as `Pipeline.Stage1` (`main.go`, `internal/tui/tui.go`) — the callback adapts `Stage1` output into `memory.Stage1Result`.

### internal/dream/summarize.go
- **Role:** Two distillers — the small injected per-scope summary, and the cross-project global user profile.
- **Key symbols:** `summarizePrompt` + `Summarize(ctx, p, memory)` — distil `MEMORY.md` into the small injected `memory_summary.md` (refuses a summary not smaller than input); `maxSummarizeInput` (200k); `globalProfilePrompt` + `DistillGlobal(ctx, p, summaries, existingGlobal)` — extract project-independent user-profile bullets from many projects' summaries (deduped).
- **Depends on:** `internal/llm`; reuses `dedupe`/`parseBullets` from dream.go.
- **Used by:** `Summarize` wired as `Pipeline.Summarize`; `DistillGlobal` called by `daemon.go` nightly dreamer + `main.go` `runDream` to build the global profile.

### internal/orientation/orientation.go
- **Role:** The whole native history/provenance engine — identity/keys, transcript ingestion into a per-project JSON episode graph, the provenance/related/coupled/query/status/threads readers, hook + CLI dispatch, and hook installation.
- **Key symbols:**
  - Paths/setup: `Paths`, `DefaultPaths()`, `EnsureHome()` (creates `~/.eigen/orientation`, allowlist), `ReadAllowlist()`, `Allowlisted(cwd, prefixes)`.
  - Identity: `Identity`/`Manifest` structs, `inspectProject(cwd)` (git remote/root/head/branch → `projectKey` via `projectKeyVersion`), `projectKeyCandidates()`, `gitOut()` (best-effort, 2 s timeout via the `commandContext` var seam), `shortHash()`.
  - Data model: `Episode`, `EpisodesFile`, `Run`, `Evidence`, `Graph`/`GraphNode`/`GraphEdge`, `transcriptRow`/`toolCall` (dual JSON casings via `role()/text()/timestamp()/id()/name()/args()`).
  - Ingest: `IngestSource(...)` → `readTranscript` → `episodesFromRows` (user msg = new episode/intent, assistant msg = prose + tool-derived `FilesTouched`/`Runs`) → `mergeProjectEpisodes` (dedupe by id, write `.manifest.json`/`episodes.json`/`graph.json` atomically). Helpers: `parseArgs`, `argString`, `baseToolName`, `filesFromTool`, `filesFromPatch`(`patchFileRe`), `runFromTool`, `canonicalFile`, `cleanText`, `sortedUnique`, `inferCWDFromSource`, `sessionIDFromSource`.
  - Graph + queries: `BuildGraph(eps)`; readers `Provenance`, `Related`, `Coupled`, `Query`, `Threads`, `Status`, `Refresh` (write to an `io.Writer`); coupling helpers `makeMatcher`, `coupledPairs`/`coupledLines`/`fileWeight`, `committed`, `ms`, `fmtAge`, `boolScore`, `firstN`, `latestEpisodeTime`.
  - Source discovery: `FindEigenSource(session, task)` (newest matching `.jsonl` under `~/.eigen/sessions`, daemon session dirs, or tasks).
  - Hooks/CLI: `Hook(r,w,args)` (parse hook JSON, gate `note` events, ingest), `stringField`/`valueAfter`, `InstallHooks(wrapper)`/`HooksStatus(w)` (manage `~/.eigen/hooks.json` for `eigenEvents`), `RunCLI(ctx,args,...)` (dispatch all subcommands), `argOrCwd`, `PrintUsage`, `contains`.
  - I/O: `readJSON`, `writeJSONAtomic` (temp-file + rename, 0o600).
- **Depends on:** stdlib only; `commandContext` indirection defers the actual `os/exec` to exec.go.
- **Used by / entrypoint:** reached from `internal/harness/orientation.go` (`OrientationHome`/`InstallOrientation`/`InstallOrientationHooks`/`RunOrientation` → `orientation.RunCLI`), which `main.go` calls (`runOrientationCmd`, install harness). `internal/app/home.go` references the orientation home/project dir. Hooks invoke the installed wrapper → `eigen orientation hook`. The internal-only readers (`Provenance`, `Related`, etc.) are all dispatched by `RunCLI`'s switch.

### internal/orientation/exec.go
- **Role:** The one real `os/exec` call, isolated so the rest of orientation can be tested by swapping the `commandContext` var.
- **Key symbols:** `osExecOutput(ctx, name, args...)` — `exec.CommandContext(...).Output()`; backs `runOutput` → the `osExecCmd.Output()` impl in orientation.go.
- **Depends on:** stdlib `os/exec`.
- **Used by / entrypoint:** `orientation.go`'s `runOutput`/default `osExecCmd` (i.e. `gitOut`). Not called outside the package.

## Cross-links
- **`internal/llm`** — dream's only dependency: every `dream.*` function calls `llm.Provider.Complete` with `llm.Request`/`llm.Message`. (See `llm-providers.md` / `llm-routing.md`.)
- **`internal/tool`** — `internal/tool/memory.go` exposes the memory tool to the agent via `MemoryStore`/`memoryReader`/`memorySearcher` interfaces (`Append`, `AddBan`, `ReadRelative`, `Search`) satisfied by `*memory.Store`. (See `tool-actions.md`.)
- **TUI (`internal/tui`)** — opens the index, builds a dream pipeline (`newTUIDreamPipeline`), runs idle "dream"; reads memory for panels. (See `tui-core.md`.)
- **GUI (`internal/gui`)** — `memory.go`/`dreaming.go` build DTOs from `*memory.Store` (read, bans, ad-hoc notes, rollouts, backups, user profile) for the desktop app. (See `gui-bridge.md` / `gui-views-*.md`.)
- **Daemon (`daemon.go`)** — nightly dreamer: opens global + per-project stores, builds pipelines, runs `dream.SynthesizeSkill`/`DistillGlobal`, commits memory. (See `daemon.md`.)
- **`internal/feed`** — `feed/memory.go` + `feed/suggest.go` read project `MEMORY.md` tails for cheap local suggestions. (See `skill-feed-retrieve.md`.)
- **`internal/app`** — `app/data.go` holds a `GlobalMem *memory.Store`; `app/pages.go` calls `dream.Consolidate`; `app/home.go` surfaces the orientation home. (See `app-superapp.md`.)
- **`internal/harness`** — sole gateway into orientation (install wrapper/hooks, `RunOrientation` → `RunCLI`). (See `root-cmd.md`.)
- **Root command (`main.go` / `build.go` / `remote_session.go`)** — open stores, render `memory.Sections` into the system prompt for chat sessions, run `eigen dream` / `eigen memory` / `eigen orientation`.
