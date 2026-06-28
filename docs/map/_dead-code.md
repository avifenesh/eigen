# Dead-code suspects

These are **suspects, not confirmed dead code.** Each was flagged by an area mapper from repo-wide grep
evidence, but a static survey cannot prove non-use. **Confirm with a human (and a clean build/test run)
before deleting anything.**

Symbols that are **Wails-bound, satisfy an interface, sit behind a build tag, or are reached via
reflection / table dispatch** were deliberately excluded by the mappers — those look unreferenced to grep
but are live. The medium/low rows below are weighted toward *over-exported* or *test-only* helpers (real,
working code that is simply not called from production) rather than removable garbage; read each "Why"
before acting.

---

## High confidence

These have zero callers anywhere (production and tests), satisfy no interface, and are not table/reflection
dispatched — the strongest removal candidates.

| Symbol | File | Why | Area |
| --- | --- | --- | --- |
| `regSpinner` | internal/tui/layout.go | const in the `region` iota enum; only hit repo-wide is its own definition. Never produced by hitTest (spinner row folds into regInput localY==0) nor compared against. Vestigial enum member. | tui-core |
| `layout.spinner` | internal/tui/layout.go | `spinner rect` field; computeLayout writes it but it is never read anywhere. The spinner's clickable area is hit-tested via regInput row 0 (actBackgroundTurn), not this rect. | tui-core |
| `_observePanelNoRawContentGuard` | internal/tui/observepanel.go | Orphaned no-op returning `strings.TrimSpace("metadata-only")`; zero callers incl. tests. Satisfies no interface, no build tag, no map. Sole user of the file's `strings` import. | tui-panels |
| `findBlock` | internal/tool/patch.go | Unexported single-match locator; zero call sites incl. tests. Superseded by findHunk -> findBlockMatches (anchor-scored, drift-tolerant). Build passes without it referenced. | tool-fs |
| `filePatch.renaming` | internal/tool/patch.go | Unexported method, never called; applyPatch inlines the equivalent condition. Siblings creating()/deleting() ARE called, only renaming() is dead. | tool-fs |
| `(*GLM).prepare` | internal/llm/glm.go | Appends a search hint to the system prompt but has zero callers; live path uses package-level glmPrepare(req, search) directly. Stale unused wrapper. | llm-providers |
| `(*Grok).prepare` | internal/llm/grok.go | Same pattern as GLM.prepare — system-prompt hint wrapper with zero callers; Complete/Stream call package-level grokPrepare directly. | llm-providers |
| `(*Agent).Shelled` | internal/agent/agent.go | `func (a *Agent) Shelled() bool { return a.Shells != nil }`; only Go reference is its own def, no Wails binding. All 4 call sites read `a.Shells` directly. | agent |
| `Host.Count` | internal/daemon/host.go | Zero callers in production or tests; only matches are unrelated (RunningCount, callCount). Orphaned exported method. | daemon |
| `Host.SetBgCount` | internal/daemon/host.go | No caller ever installs the bgCount reporter, so h.bgCount stays nil and DaemonStats.BgTasks is always 0. The feeding BgRegistry is never wired into the Host. | daemon |
| `var _ = llm.RoleUser` | internal/daemon/session.go | Blank import-guard, but llm is used extensively in the same file (Image, Message, RoleUser, EffortSetter, …). Redundant; removal cannot break the build. | daemon |
| `(*Client).ServerName` | internal/mcp/client.go | Exported accessor returning cached serverName; zero readers (prod or tests). Paired Instructions() IS used; ServerName is not. internal/ pkg, no external importer. | telegram-mcp |
| `UserDir` | internal/command/command.go | Exported func, zero callers repo-wide; command dirs are enumerated via Dirs() instead. Leftover from a planned plugin-install path. | infra-misc |
| `NerdFont` | internal/theme/icons.go | `NerdFont() bool` has zero callers repo-wide incl. tests; active tier is read via NerdFontMode() and the internal nerdFont var. | infra-misc |
| `Tooltip.svelte` (whole component) | internal/gui/frontend/src/lib/components/Tooltip.svelte | Only repo hit is the file itself; no view/App/component imports it (no barrel re-exports exist). App uses native `title=` attributes instead. | gui-components |
| `wails.json frontend:dir = "internal/gui/static"` | wails.json | Stale path: internal/gui/static does not exist; the built frontend lives at internal/gui/frontend/dist and is `//go:embed`-ed in main_gui_wails.go. Real config drift (build still works — Makefile never reads frontend:dir). | docs |

## Medium confidence

Real code with a narrow or test-only caller set, or a documented-schema field — likely safe to inline /
unexport / remove, but each has a caveat in "Why".

| Symbol | File | Why | Area |
| --- | --- | --- | --- |
| `SetSearch` (no-op stub comment) | internal/llm/codex.go | Comment block (lines 149-150) describing a no-op stub SetSearch method, but no such method exists. Dangling vestigial doc comment; Codex is not registered as a Searcher. | llm-providers |
| `fromMessageDTOs` | internal/gui/dto.go | Reverse converter (DTO->llm.Message) referenced only by bridge_test.go; no bound Bridge method uses it. Exists to round-trip the lossy forward conversions. Plausible future seed-history seam. | gui-bridge |
| orphan comment `// put records (and persists) a task state.` | internal/agent/background.go | Stray doc comment (line 157) detached from `put` (defined later at line 168); sits above SeedDone's own comment. Cosmetic, not executable dead code. | agent |
| `CachePath` | internal/feed/feed.go | Exported func; callers are only inside feed.go (Load/save). Not internally dead but needlessly exported — could be unexported. | skill-feed-retrieve |
| `ProposedDir` | internal/skill/propose.go | Exported func; every reference is inside propose.go itself. Over-exported, not internally dead. | skill-feed-retrieve |
| `(*Client).CallTool` | internal/mcp/client.go | Text-only wrapper over CallToolRich; production (load.go/lazyClient) only calls CallToolRich. Only CallTool callers are client_test.go. Test-only in an internal/ pkg. | telegram-mcp |
| `wrap` | internal/mcp/load.go | wrapCaller shim for a non-lazy *Client; production wraps via wrapLazy. Only callers are client_test.go. Test-only helper. | telegram-mcp |
| `Index.BumpUsage` | internal/memory/index.go | "Forgetting signal" usage tracker; only callers are index_test.go. Pipeline never bumps usage, so usage_count/last_used are written by nothing in production. | memory-dream-orientation |
| `Index.RecordSummary` | internal/memory/index.go | Writer for the legacy `summaries` table; only index_test.go references it. Production records via RecordStage1Output into stage1_outputs. Write-dead in production. | memory-dream-orientation |
| `Index.Summarized` | internal/memory/index.go | Only index_test.go calls it; pipeline idempotency uses Stage1Summarized directly. Wrapper (with legacy-summaries fallback) has no production caller. | memory-dream-orientation |
| `Bridge.Stats` | internal/gui/frontend/src/lib/bridge.ts | Typed facade wrapper; zero frontend callers. Live KPIs are pushed via the eigen:daemon:stats event into the daemon store, so this pull wrapper is unused. Intentional full-API mirror. | gui-lib-stores |
| `Bridge.DetachBash` | internal/gui/frontend/src/lib/bridge.ts | Facade wrapper, zero frontend callers (sibling KillShell IS called). Go method is Wails-bound and live. Full-API mirror. | gui-lib-stores |
| `Bridge.MemoryBackups` | internal/gui/frontend/src/lib/bridge.ts | Facade wrapper, zero frontend callers; Memory view surfaces backup count via MemoryScopeDTO.backups instead. Go method live + Wails-bound. | gui-lib-stores |
| `Bridge.FeedFor` | internal/gui/frontend/src/lib/bridge.ts | Facade wrapper, zero frontend callers; Home consumes the whole feed via the feed store, never the per-dir FeedFor. Go method live. | gui-lib-stores |
| `PluginInterface.Capabilities` | internal/plugin/manifest.go | Exported field set by JSON unmarshalling, never read. Part of the consumed Codex `interface` schema mapping; round-trips. (Listed low by the mapper; grouped here as a schema-field caveat.) | plugin |

## Low confidence

Mostly intentional API surface, defensive branches, or test-retained helpers. Flagged for completeness;
removal is **not** recommended without an explicit policy decision.

| Symbol | File | Why | Area |
| --- | --- | --- | --- |
| `sessionDeps.Provider` | build.go | Assigned by buildSession, never read back; package-private struct doc says it holds resources the caller may "reuse". Likely intentional API surface. | root-cmd |
| `sessionDeps.Router` | build.go | Assigned, never read; same package-private struct caveat. Likely kept for future live-route control of daemon sessions. | root-cmd |
| `sessionDeps.Mem` | build.go | Assigned, never read back; the local `mem` is what gets wired. Documented as a resource the struct owns. | root-cmd |
| `sessionDeps.hooks` | build.go | Assigned, never read; local hookRunner is used directly. Package-private field kept alongside eventWrap (which IS read). | root-cmd |
| `failoverFor` | internal/tui/tui.go | NOT dead — called by nextFailover. Smell only: the `failing` parameter is ignored and the body always returns failoverChain (doc claims per-model ladders that aren't implemented). | tui-core |
| `renderDiff` | internal/tui/diffview.go | Zero-arg wrapper = renderDiffLang(s, ""); only caller is blocks_test.go. Production uses renderDiffLang directly. Test-only convenience seam. | tui-render |
| `Bash` | internal/tool/bash.go | Exported constructor without backgrounding; all production registrations use BashWithShells. Exported API a future embedder/test could use; trivial wrapper over bashWith. | tool-fs |
| `(*Converse).additionalFields` | internal/llm/converse.go | No production caller — Complete uses package-level additionalConverseFields. Only references are converse_test.go (wire-shape assertion). Candidate to inline. | llm-providers |
| `Classify` | internal/llm/classify.go | Exported, only Go callers are classify_test.go; doc says "legacy deterministic classifier retained for tests". Production routing uses a prompt-router model assessor. | llm-routing |
| `IsFrontend` | internal/llm/classify.go | Exported, only callers are classify_test.go; production frontend signal comes from the prompt-router assessor's JSON field. Part of the retained legacy classifier. | llm-routing |
| `ValidEffort` | internal/llm/llm.go | Exported, only caller is tui_test.go; production effort validation goes through ModelEffortLevels + effortSupported. Test-only public helper. | llm-routing |
| `(*BgRegistry).SeedDone` | internal/agent/background.go | Used only by daemon_test.go to inject a pre-completed task. Legitimate cross-package test-only helper — NOT flagged removable. | agent |
| `Host.Hydrate` | internal/daemon/host.go | Exported, only test callers (persist_test.go); production uses hydrateLocked. Doc says "for tests and low-risk control paths" — deliberate helper. | daemon |
| `role` (tool/toolResult/default branches) | internal/transcript/claude.go | Single callsite passes a record gated to user||assistant, so the extra branches are unreachable. Kept as a defensive/complete role-mapper. | transcript |
| `PluginInterface.DeveloperName` | internal/plugin/manifest.go | Exported field set by JSON, never read; part of the consumed Codex `interface` schema mapping. Removing drops a documented format field, not dead logic. | plugin |
| `Save` | internal/skill/skill.go | Exported func; no external-package caller, used only by sibling finishInstall + tests. Possibly intended public API — over-exported rather than dead. | skill-feed-retrieve |
| `Index.Summaries` | internal/memory/index.go | Callers only in index_test/pipeline_test/tui_test; no production call site. Its legacy-summaries fallback is doubly unreachable (RecordSummary never called outside tests). | memory-dream-orientation |
| `SummaryRow` | internal/memory/index.go | Struct used only by RecordSummary/Summaries, both test-only. Dead in lockstep with the legacy-summaries cluster; exported + test-covered. | memory-dream-orientation |
| `Index.Claim` | internal/memory/index.go | Non-scope job claimer; only test callers. Production drains via ClaimScope. Exported "drain all scopes" sibling — plausibly intended API, explicitly tested. | memory-dream-orientation |
| `Index.Stage1Outputs` | internal/memory/index.go | Stage1 lister; only test callers. Production consolidation reads via Phase2Inputs. Exported, test-covered, natural inspection API. | memory-dream-orientation |
| `Store.WriteRawMemories` | internal/memory/memory.go | IS reachable (pipeline.go:240). Noted only because the raw_memories.md scratchpad it writes is never read back — a debug/forensic artifact, not dead code itself. | memory-dream-orientation |
| `Back` | internal/theme/icons.go | `Back = "‹"` glyph const referenced only inside Swatch() (design-system display block); no production surface uses theme.Back. Part of the documented glyph vocabulary. | infra-misc |
| `ToastKind "working"` | internal/gui/frontend/src/lib/stores/toasts.svelte.ts | Rendered by ToastHost (GLYPH record + CSS) so the type member is required, but no code path emits one (no toasts.working() helper). Styling branch effectively unreachable; reserved/intended kind. | gui-lib-stores |
| `docs/hooks-example.json` | docs/hooks-example.json | Example hooks-config asset; zero references in any .md/.go. Unreferenced but plausibly an intentional copy-paste example. | docs |
| `docs/automation-example.md` | docs/automation-example.md | How-to doc not linked from README/ROADMAP/any doc. Standalone reference; "dead" only in the link-graph sense. | docs |
| `docs/research-compaction.md` | docs/research-compaction.md | Research note; zero non-self references, not linked anywhere. It links OUT to research-codex-memory.md but nothing links back. Legitimate research artifact, unlinked. | docs |

---

**Tally:** 16 high · 15 medium · 26 low — 57 suspects total across 26 areas.
