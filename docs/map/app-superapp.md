# app/ — the TUI superapp pages

> `internal/app` is Eigen's terminal "superapp" shell: the paged dashboard you land on when you run bare `eigen`. It is a single Bubble Tea `Model` that draws a left page rail + a bordered content panel (+ an optional right inspector on wide terminals), and routes keyboard/mouse input to one of fourteen pages (home, live, projects, machines, sessions, config, skills, models, providers, observe, memory, crons, plugins, profile). Each page is a small `*State` struct with `init/update/view` (and most also `clickAt`). The shell never opens chats itself — it gathers a `Result` (open chat / resume / attach / remote) and hands it back to `main`/`daemon` to act on. All data is loaded once at startup into a `*Data` value (sessions, projects, config, skills, daemon live list, feed, machines, observe rollup, custom providers) and refreshed via Bubble Tea ticks and async commands.

## Files

### internal/app/app.go
- **Role:** The shell core — the top-level Bubble Tea `Model`, page enum, input routing (keys/mouse/palette), the framed-view composition (`View`), and the public `Run*`/`New*` entrypoints.
- **Key symbols:**
  - `Page` (int enum) + `pages []pageSpec` — the 14 surfaces in rail order, each with a name, quick-jump key, purpose/action copy.
  - `Action` (enum: `ActionQuit/OpenChat/Resume/Attach/Remote`) and `Result{Action,Dir,SessionID,Task,Host}` — the exit intent returned to `main`.
  - `Model` — holds width/height, active page, per-page state structs, `*Data`, palette, live-spinner frame (`liveSpin`), content scroll, a `pendingG` jump-prefix flag, the exit `result`/`quitting` flags, and a `ctx`/`cancel` that gates background work (cancelled on quit). `quitWith(Result)` stores the intent, cancels the context, and quits.
  - `New(*Data)` / `NewAt(*Data, Page)` — build the shell; `NewAt` selects an initial page and calls each page's `init` (every page except `live`, which has no `init`).
  - `isKnownPage`, `PageByName(name)` (resolves page name/alias for integrations), `newAtPageName` + `applyInitialPageName` (used by `RunPage` to deep-link e.g. plugins/hooks tab).
  - `Init()` — kicks off background session titling, live polling, and feed rescan + tick.
  - `scanFeed`, `feedTick`/`feedTickMsg`, `titleTick`/`titleRefreshMsg`, `feedMsg` — async feed/title refresh plumbing; `feedRefreshEvery = 10m`.
  - `Update(msg)` — the message switch: window resize, mouse, `livePollMsg`, feed/machine/install/marketplace/preview/consolidate done messages, and the big `tea.KeyMsg` router (palette intercept, ctrl+c, text-capture guard via `capturingInput`, `g`-prefix jumps, content scroll, tab cycling, page delegation).
  - `capturingInput`, `cycle(d)`, `jumpKey`, `handleContentScrollKey`, `scrollContent` — input mode + navigation helpers.
  - `handleMouse`, `contentClick`, `contentWheel` — mouse routing through `hitTest`; delegates content-local clicks to each page's `clickAt`.
  - `updatePage`, `renderPage` — dispatch update/view to the active page state.
  - `View`, `overlayPalette`, `padLeft`, `renderTitleBar`, `renderRailBox`, `renderContentBox`, `renderInspectorBox`, `renderStatusBar`, `railContent` — the composed frame; `liveGlyph`/`liveLabel` render live-session status (breathing λ on WORKING) in rail + home.
  - `clipTextWindow`/`clipTextHeight`/`splitRenderableLines`, `wrapText`, `helpLine`/`helpLineText`, `contentMissionLine`/`contentMissionHeight`/`contentBodyHeight`, `titleStats`, `activeSpec/Name/Purpose/Action`, `setActive` — chrome/layout text helpers.
  - `Run(*Data)` / `RunAt` / `RunPage` / `runModel` — open the alt-screen program and return the final `Result`.
- **Depends on:** `internal/theme`, `internal/daemon`, `internal/feed`, `internal/plugin`, `internal/remote`; bubbletea + lipgloss.
- **Used by / entrypoint:** **entrypoint into the whole slice.** `main.go` calls `app.Load()` then `app.Run(data)`/`app.RunPage(data,page)`; `daemon.go` calls `app.RunPage`; `smoke_hooks_smoke.go` calls `app.RunAt(app.LoadEmpty(), app.PageHome)`. `New`/`NewAt` are also the test constructors.

### internal/app/data.go
- **Role:** The data model loaded once at startup and the read funcs/derivations each page renders from.
- **Key symbols:**
  - `Data` — the aggregate: `Sessions`, `Projects`, `Config`, `Skills`, `GlobalMem`, `Store`, `Titler`, `Small`, `Daemon`, `Live`, `Feed`/`FeedFresh`, `Machines`, `Observe`/`ObserveErr`/`ObservePath`, `CustomProviders`/`CustomErr`.
  - `SessionRow`, `ProjectRow`, `ModelRow`, `ProviderRow` — row view structs.
  - `Load()` — gathers everything (session store, skills, daemon dial, global memory, feed cache, machines, observe summary, custom providers), degrading gracefully on failure; `LoadEmpty()` — deterministic side-effect-free data for smoke tests.
  - `openAction(SessionRow)` — maps a row to ATTACH (daemon) vs RESUME (store) intent.
  - `(*Data).reloadSessions`, `refreshLive`, `projectDirs`, `feedItems`, `feedFor`, `suggester`; `groupProjects`, `skillDirs`, `Models()`, `Providers()`, `customProviderByName`, `suggestProvider` — derivations and provider/model catalog builders.
- **Depends on:** `internal/config`, `internal/daemon`, `internal/feed`, `internal/llm`, `internal/memory`, `internal/observe`, `internal/remote`, `internal/session`, `internal/skill`.
- **Used by / entrypoint:** `Load`/`LoadEmpty` called from `main.go`/`daemon.go`/smoke; every page state reads from `m.data`.

### internal/app/layout.go
- **Role:** Single source of truth for shell geometry — named rects derived from terminal size + breakpoint, shared by rendering and hit-testing.
- **Key symbols:** `rect{x,y,w,h}` + `empty`/`contains`; `breakpoint` enum (`bpNarrow/bpNormal/bpWide`) + constants (`railWidthNormal=18`, `railWidthWide=22`, `inspectorWidth=34`, `bpWideMin=130`, `bpNarrowMax=72`); `appLayout` (title/rail/content/inner/inspector/status rects); `(*Model).computeLayout()`; `breakpointFor`; `railFrame`/`contentFrame`; the box styles `sRailBox`/`sContentBox` + `sContentPadH=2`.
- **Depends on:** lipgloss only.
- **Used by / entrypoint:** `computeLayout` is called all over app.go (`View`, scroll, mouse) and by `hit.go`.

### internal/app/hit.go
- **Role:** Mouse hit-testing — resolves an absolute (x,y) click to a region + target against the same rects `computeLayout` produced.
- **Key symbols:** `hitRegion` enum (`hitNone/hitTitle/hitRail/hitRailLive/hitContent/hitInspector/hitStatus`); `appHit{region,page,liveID,localX,localY}`; `(*Model).hitTest(x,y)`; `(*Model).railHitAt` (maps a rail click to a page row or a live-session entry, mirroring `railContent` layout).
- **Depends on:** none beyond the package (uses `pages`, `railLiveMax`, `m.data.Live`).
- **Used by / entrypoint:** `app.go:handleMouse`. Note: `hitTitle`/`hitStatus`/`hitInspector` regions are produced but the click handler only acts on rail/rail-live/content; the rest are intentional no-op chrome zones.

### internal/app/style.go
- **Role:** Package doc + the shared color/style palette (sourced from `internal/theme` so the shell and chat read as one product) and `sectionLabel`.
- **Key symbols:** color vars `cAccent/cText/cDim/cFaint/cTitle/cOk/cWarn/cErr/cViolet/cFocus/cSel/cWorking/cSurface`; lipgloss style vars `sText/sDim/sFaint/sTitle/sAccent/sWorkingText/sOk/sWarn/sErr/sViolet/sRailActive/sRailIdle/sRowSel/sRowDim`; `sectionLabel(label,w)` ("label ─────" header matching the chat sidebar).
- **Depends on:** `internal/theme`, lipgloss.
- **Used by / entrypoint:** every page's `view`.

### internal/app/surface.go
- **Role:** Canvas painting — fills the whole terminal rectangle with the Base surface so transparent terminal backgrounds never leak through gaps (mirrors the chat TUI's canvas contract).
- **Key symbols:** `bgSeq(hex)`, `hexRGB`, `surfaceHex(AdaptiveColor)`, `fillBG(content,hex,width)`, `paintBase(view,width,height)`.
- **Depends on:** `internal/theme`, lipgloss, `charmbracelet/x/ansi`.
- **Used by / entrypoint:** `app.go:View` (final `paintBase(...)`). `surfaceHex`/`fillBG`/`bgSeq` names are duplicated in `internal/tui` (separate package copy — not shared symbols).

### internal/app/list.go
- **Role:** The reusable list primitive (cursor + window) and shared row/text rendering helpers embedded by every page.
- **Key symbols:** `list{cursor,count,top}` + `clamp`/`move`/`key`/`window`; `clickMap{line2idx}` + `reset`/`mark`/`at` (renderer records which content-local line each item occupies — geometry authority for clicks); `lineCount`; `pageTitle(title,sub,w)`; `row(selected,text)`; `truncate(s,w)`; `pad(s,w)`; `min`; `countLabel`.
- **Depends on:** lipgloss, `charmbracelet/x/ansi`.
- **Used by / entrypoint:** embedded/called by every page state.

### internal/app/home.go
- **Role:** The landing page — brand banner, quick stats, the proactive action feed (one-key session starters), working-now live sessions, and recent sessions. One cursor walks feed items then sessions.
- **Key symbols:** `homeState{list,feed,feedN,sessionN,expanded,clicks}`; const `homeFeedLimit/homeRecentLimit/homeLiveLimit`; `init`/`syncFeed`/`update`/`view`/`clickAt`; `homeObserveSignal`, `renderTask`, `kindGlyph`, `relTime`, `homeGreeting`, `workingCount`.
- **Depends on:** `internal/daemon`, `internal/feed`.
- **Used by / entrypoint:** `app.go` (PageHome dispatch). `enter` on a feed item returns `ActionOpenChat` with the offered task; `enter` on a session returns `openAction`.

### internal/app/live.go
- **Role:** The live page — the daemon's running sessions; attach/new/interrupt/stop with confirm; polls every 1.2s.
- **Key symbols:** `liveState{list,confirmStop,notice,clicks}`; `livePollMsg`/`livePoll()` (1200ms tick); `update`/`clickAt`/`view`; `liveSummaryLine`, `liveSelectedDetail`.
- **Depends on:** `internal/daemon`.
- **Used by / entrypoint:** `app.go` (PageLive). `enter`/click → `ActionAttach`; `n` creates a daemon session via `d.Daemon.New` then attaches.

### internal/app/pages.go
- **Role:** Four pages in one file — **config** (view+edit persistent defaults with dropdown/cycle/inline editors), **skills** (list + preview + inline install), **models** (catalog with availability/caps), **providers** (credential status + add-custom-provider form), plus **memory** (global notes: read/delete/consolidate). Largest file in the slice.
- **Key symbols:**
  - `configState` + `optionsFor`, `setAndSave`, `update`, `clickAt`, `view`, `wrapTo`.
  - `skillsState` + `init/update/view`, `skillsSummaryLine`, `skillSelectedDetail`.
  - `modelsState` + `init/update/view`, `modelsSummaryLine`, `modelSelectedDetail`.
  - `providersState` (+ `providerDraft`, `providerAddField`/`providerAddFields`) + `startAdd`/`updateAdd`/`visibleAddFields`/`draftValue`/`setDraft`/`saveDraft`/`view`/`viewAdd`; `providerSubtitle`, `providersSummaryLine`, `providerSelectedDetail`.
  - `memoryState{list,bullets,loaded,confirm,status,consoling,open,detailScroll,clicks}` (+ `consolidateDoneMsg`) + `load`/`update`/`view`/`detailView`/`clickAt`/`deleteSelected`/`selectedNote`/`scrollDetail`/`clampDetailScroll`; `memoryBullets`, `memoryDetailLines`, `memorySummaryLine`.
- **Depends on:** `internal/config`, `internal/dream` (memory consolidation), `internal/llm`, `internal/skill`.
- **Used by / entrypoint:** `app.go` dispatch for PageConfig/PageSkills/PageModels/PageProviders/PageMemory. Memory `C` runs `dream.Consolidate` async; `consolidateDoneMsg` handled in `app.go:Update`.

### internal/app/sessions.go
- **Role:** Two pages — **sessions** (flat all-sources list with type-to-search, source filter, recency cutoff, resume/export/delete) and **projects** (sessions grouped by dir, drill-in with per-project feed items).
- **Key symbols:** `sessionsState{list,filter,visIdx,hidden,confirmDel,notice,clicks}` + `init/refresh/update/view/clickAt`, `sessionsSummaryLine`, `sessionSelectedDetail`, `exportPath`, `slug`; `projectsState{list,inside,proj,inner,feedN,clicks}` + `init/update/view/clickAt`, `projectsSummaryLine`, `projectSelectedDetail`.
- **Depends on:** `internal/daemon`, `internal/transcript` (export).
- **Used by / entrypoint:** `app.go` PageSessions/PageProjects. Delete uses `daemon.DeletePersisted`/`d.Store.Delete`; export writes eigen-native JSONL.

### internal/app/filter.go
- **Role:** Session-list search + filters (Tier 13): fuzzy type-to-search over title+dir+id, a source-filter cycle, and a recency cutoff with show-all tail.
- **Key symbols:** `sessionFilter{searching,query,source,showAll}`; const `recencyCutoff = 7 days`; `filtered(rows)` (returns visible indices + hidden count, fuzzy-ranked when searching), `active`, `statusLine`, `key`.
- **Depends on:** `internal/fuzzy`.
- **Used by / entrypoint:** embedded in `sessionsState`.

### internal/app/machines.go
- **Role:** The machines page — remote eigen targets (saved hosts + auto-detected `~/.ssh/config`). Drill into a machine to list/open its remote sessions; one-click install of eigen over ssh.
- **Key symbols:** `machinesState` (+ drill-in/install fields); `machineSessionsMsg`, `machineInstallMsg`; `fetchMachineSessions`, `installMachine` (async commands); `update`/`updateInside`/`openMachine`/`view`/`viewInside`/`clickAt`; helpers `machinesSummaryLine`, `machineSelectedDetail`, `machineSource`, `machineBadges`, `machineAddr`.
- **Depends on:** `internal/daemon`, `internal/remote`.
- **Used by / entrypoint:** `app.go` PageMachines; `machineSessionsMsg`/`machineInstallMsg` handled in `app.go:Update`. Opening a machine/session returns `ActionRemote`.

### internal/app/observe.go
- **Role:** The observe page — a metadata-only telemetry command center (routing decisions, subagents, errors/notes, model+token usage, skills/tools/hooks, runtime stress). Renders aggregates, never transcript content.
- **Key symbols:** `observeState{list}` + `init/sync/update/view`; section renderers `observeHero`/`observeRoutes`/`observeSubagents`/`observeCounts`/`observeModels`/`observeSkills`/`observeTools`/`observeHooks`/`observeRuntime`; shared utils `countItem`/`nonZeroCounts`, `inlineCounts`, `countTotal`, `sortedKeys[V]`, `avg64`, `observeBytes` (these utils are reused by home/plugins/profile too).
- **Depends on:** `internal/observe`.
- **Used by / entrypoint:** `app.go` PageObserve. `sortedKeys`/`avg64`/`countTotal`/`countItem` are shared helpers used across the slice.

### internal/app/crons.go
- **Role:** The crons page — read-mostly view of scheduled jobs (systemd `--user` timers + the user's crontab); start/stop/trigger timers via `systemctl --user`.
- **Key symbols:** `CronRow`; `loadCrons`/`loadSystemdTimers`/`loadCrontab`/`humanizeMicros`; `cronsState{list,rows,loaded,status}` + `init`/`load` (lazy)/`update`/`view`; `timerCtl`, `cronsSummaryLine`, `cronSelectedDetail`.
- **Depends on:** stdlib `os/exec` only (no internal pkgs).
- **Used by / entrypoint:** `app.go` PageCrons. Loads lazily on first view (avoids shelling out at app start).

### internal/app/install.go
- **Role:** Shared install machinery for the plugins + skills pages — an inline text-prompt widget and the plugin/marketplace/skill install/preview/update runners (so the shell can install without the CLI). Also defines the async "done" messages.
- **Key symbols:**
  - `installPrompt{active,label,kind,input,status,busy,busyText}` + `open`/`close`/`startBusy`/`finish`/`key`/`render`; `renderPromptStatus`.
  - Messages: `installDoneMsg`, `marketplaceRefreshDoneMsg`, `pluginPreviewDoneMsg`.
  - `appPluginRegistry`; plugin runners `runMarketplaceAdd`, `runPluginInstall`/`runPluginInstallFrom`/`runPluginInstallArgs`, `runPluginBatchInstall`, `installOnePlugin`, `runPluginPreview`, `runMarketplaceUpdate`; status formatters `formatPluginInstallStatus`, `pluginInstallFailureStatus`, `pluginInstallResultLine`; input parsing `pluginInstallInput`/`parsePluginInstallInput`/`splitPluginMarket`.
  - Skill install: `skillInstallArgs`/`parseSkillInstallInput`, `runSkillInstall`, `installSkillSource`.
- **Depends on:** `internal/plugin`, `internal/skill`.
- **Used by / entrypoint:** `pluginsState`/`skillsState` (prompt + runners run inside async `tea.Cmd`s); done messages handled in `app.go:Update`.

### internal/app/plugins.go
- **Role:** The plugins page — Eigen's "Skills & Apps" surface (Codex-desktop-style): segmented tabs (Plugins / Marketplace / Wiring / Hooks), install/enable/disable/delete, marketplace catalog browse + batch install + preview, and the raw extension-wiring view. Largest page file.
- **Key symbols:**
  - `ExtRow` + `loadExtensions`/`loadMCPRows`/`loadPluginRows`/`loadLSPRows`/`loadHookRows` (read-only parse of mcp/plugins/lsp/hooks json, user+project).
  - `pluginsState` (tabs, installed/markets/catalog, prompt/confirm, click maps); `pluginConfirm`, `pluginsTab` enum.
  - Lifecycle: `init`/`load`/`reload`/`filterInstalledCatalog`/`syncListCount`/`setTab`/`selectPluginWithAgent`.
  - Input: `update`, `visibleRows`, `applyConfirm`, `updateInstalled`/`removePlugin`, `updateMarketplace`/`setMarketplaceEnabled`/`removeMarketplace`/`refreshMarketplace`/`updateMarketplaceFromRemote`, catalog focus group (`focusCatalog`, `updateCatalog`, `selectedCatalogEntry`, `toggle/marked/installSelected/installMarked/previewSelected CatalogPlugin`, `catalogMarketName`, `catalogVisibleRows`, `catalogEntryKey`/`catalogPreviewKey`), `updateExtension`/`confirmUninstallSelected`.
  - View: `view`/`hero`/`tabs`/`viewInstalled`/`pluginCard`/`pluginDetail`/`installedPluginPreview`/`viewMarketplace`/`marketCard`/`marketDetail`/`viewExtensions`/`viewHooks`/`hookTelemetryBlock`/`viewExtensionRows`/`pluginPreviewBlock`.
  - Click: `clickAt`/`clickCatalog`; `pluginTabClick`/`pluginTabClickMap` (x-range tab hit map); `markClickBlock`.
  - Formatting: `pluginCounts`/`pluginInstallCounts`/`pluginScanBadge`/`pluginScanVerdict`/`pluginScanStatus`/`pluginTaskRoleLine`/`pluginComponentNames`/`pluginPreviewCounts`/`firstNonEmptyApp`/`catalogEntryMeta`/`pluginEnabled`/`emptyCard`/`dateLabel`/`kindStyle`/`minPluginTab`/`maxPluginTab`/`hookRows`/`extensionRowsForTab`/`selectedExtensionRow`.
- **Depends on:** `internal/plugin`, lipgloss; (toggle via `toggle.go`, registry via `install.go`).
- **Used by / entrypoint:** `app.go` PagePlugins; reached directly via `RunPage("plugins"/"hooks"/...)` deep-link and the palette's "plugin agent role" commands; `installDoneMsg`/`marketplaceRefreshDoneMsg`/`pluginPreviewDoneMsg` handled in `app.go:Update`.

### internal/app/toggle.go
- **Role:** Extension enable/disable — flips the `"disabled"` field on one entry of an extension config file by editing the raw JSON tree (preserves all other fields).
- **Key symbols:** `toggleDisabled(path,kind,idx)` (writes via temp file + rename), `entryList(root,kind)` (locates the entry list for mcp/lsp/plugin/hook shapes).
- **Depends on:** stdlib `encoding/json`/`os` only.
- **Used by / entrypoint:** `plugins.go:updateExtension` (space/enter toggles the selected wiring row).

### internal/app/inspector.go
- **Role:** The wide-breakpoint right inspector — a contextual key/value detail of the active page's selected row.
- **Key symbols:** `(*Model).inspectorDetail(w)`; `kv{k,v}`; `(*Model).inspectorFor()` (big switch over the active page returning title + kv rows + body for sessions/models/providers/crons/plugins/projects/skills/home/machines/live/memory/config/profile); `dirLabel`, `projShort`.
- **Depends on:** lipgloss only (reads each page's state via the `Model`).
- **Used by / entrypoint:** `app.go:renderInspectorBox` (only drawn at `bpWide`, ≥130 cols).

### internal/app/palette.go
- **Role:** The command palette — a fuzzy launcher over pages, plugin agent roles, and global actions, opened with `:` or `ctrl+k`, overlaid on the active page.
- **Key symbols:** `paletteCmd{name,hint,run}`; `palette{open,query,cursor,matches,cmds}` + `build`/`openPalette`/`filter`/`update`/`view`; `fuzzyScore(s,q)` (subsequence match with streak + word-boundary bonuses); `max(a,b)` (package-wide int max helper).
- **Depends on:** `internal/agent` (`agent.PluginRoleNames()` for role commands).
- **Used by / entrypoint:** `app.go:Update` (palette intercepts keys when open) and `app.go:View`/`overlayPalette` (draws the box). `build` enumerates `pages` + plugin roles + new-session/quit.

### internal/app/profile.go
- **Role:** The profile page — cross-session usage totals (from observe) plus one editable personalization prompt stored in global memory as USER.md.
- **Key symbols:** `profileState{editing,input,status,err}` + `init/update/view/clickAt`; `profileUsageSummary`, `profilePromptView`, `profilePromptSummary`, `profileTopModelKeys`.
- **Depends on:** `internal/observe` (usage rollup); reads/writes `Data.GlobalMem` (`UserProfile`/`WriteUserProfile`).
- **Used by / entrypoint:** `app.go` PageProfile; reachable via `RunPage("profile")` and the palette.

## Cross-links

- **internal/daemon** — live session list/polling, attach/new/interrupt/remove, `ListPersisted`/`DeletePersisted`/`PersistedTranscriptPath`, status glyphs (home/live/rail/sessions/machines).
- **internal/feed** — the proactive action feed: `Load`/`Scan`/`Top`/`FilterDismissed`/`Dismiss`/`Suggester` (home/projects/data).
- **internal/session** + **internal/transcript** — the session store (list/discover/delete/export) and transcript load/save for session export (data/sessions).
- **internal/config** — persistent defaults: `Fields`/`Keys`/`Get`/`Set`/`Save`/`Path` (config page, data load).
- **internal/skill** — skill discovery, preview, and install (skills page, install.go, data load).
- **internal/plugin** — the plugin registry: marketplaces, installed plugins, install/preview/uninstall/enable, scan verdicts (plugins.go, install.go, inspector).
- **internal/llm** — model/provider catalog + custom providers (models/providers pages, suggester, data).
- **internal/remote** — remote machines: list, ssh session peek, one-click install, credential snapshot (machines page).
- **internal/observe** — metadata-only telemetry summary (observe page, home signal, profile usage, plugins hook telemetry).
- **internal/memory** — global memory store + USER.md profile (memory + profile pages, data). USER.md has a learned/user split: the profile page reads the full file via `UserProfile()` and saves the editable text via `WriteUserProfile()`, which preserves the eigen-maintained learned block (the split's `UserProfileUser`/`UserProfileLearned`/`SetLearnedProfile` are not called from this slice).
- **internal/dream** — small-model memory consolidation (memory page `C`).
- **internal/agent** — `PluginRoleNames()` for palette role commands and plugin task-role selection.
- **internal/fuzzy** — session-search ranking (filter.go); note the palette has its own `fuzzyScore`.
- **internal/theme** — shared color palette + status glyphs (style.go, surface.go), so the shell and the chat TUI read as one product.
- **main.go / daemon.go / smoke_hooks_smoke.go** — the callers: build `*Data` via `app.Load()`/`LoadEmpty()`, run via `app.Run`/`RunAt`/`RunPage`, and act on the returned `Result` (open/resume/attach/remote a chat). The desktop GUI (Wails) is a *separate* path that does not go through this package.
- **internal/tui** — the chat TUI is a sibling surface (not imported here) that shares `internal/theme` and re-implements the same `surfaceHex`/`fillBG`/`bgSeq` canvas contract as separate copies.

## Dead-code audit

No dead code found. Almost every unexported func/type/const has a live production caller; every exported symbol either has an external caller (`Run`/`RunAt`/`RunPage`/`Load`/`LoadEmpty`/`Page*`/`Action*`/`Result`) or is the in-package/test constructor surface (`New`/`NewAt` used by the test sites and internally). `PageByName` is used by `newAtPageName`. `clipTextHeight` (app.go) is a thin `clipTextWindow(s,h,0)` wrapper used only from `app_test.go` — a test-only convenience, not a production path. The `hitTitle`/`hitStatus`/`hitInspector` regions are produced by `hitTest` but deliberately fall through to no-op in `handleMouse` (chrome click zones), so they are inert-by-design, not dead. No commented-out code blocks exist in the slice.
