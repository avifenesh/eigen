# GUI views (A): Home, Chat, Live, Sessions, Machines, Observe, Routing, Crons

> These eight `.svelte` files under `internal/gui/frontend/src/views/` are the top-level
> route pages of the Eigen desktop GUI (Wails v3 + Svelte 5 runes). Each is a default-exported
> component mounted by `App.svelte` via a `{#if router.route === ...}` switch keyed on the hash
> router. They are pure frontend: every page reads data through the `Bridge` typed wrapper
> (`$lib/bridge`) over the Go daemon, subscribes to push events through `$lib/events`, and shares
> reactive state through the rune stores in `$lib/stores`. They render no business logic of their
> own — they orchestrate Bridge RPCs, live event streams, and the shared stores into UI, and own
> the per-view leak contract (every `$effect` that opens a stream / timer / listener returns the
> matching teardown). Home is the proactive landing page; Chat is the live conversation keystone;
> Live/Sessions/Machines are session surfaces (cockpit / archive / remote); Observe/Routing/Crons
> are read-mostly inspection surfaces (telemetry / model catalog / scheduled work).

## Files

> Note: these are Svelte single-file components, not Go files — the area name says ".go" in the
> template but the slice is `*.svelte`. None of them export named Go-style symbols; each compiles
> to a single default-exported component. "Key symbols" below lists the notable in-`<script>`
> functions, `$state`/`$derived` reactive values, and `$effect` lifecycles. All are module-private
> to the component (Svelte does not export them); they are referenced only from that file's own
> template, so none can be "dead by lack of external caller" — see the dead-code section.

### internal/gui/frontend/src/views/Home.svelte
- **Role:** The home base (not a session list): five independently-rendering zones — cockpit
  greeting + live stat strip, "Act on" proactive feed, "Ideas" LLM suggestions, "Working now" live
  sessions, "Resume" recent sessions.
- **Key symbols:**
  - `$effect(() => sessions.refresh())` — pulls the session list on mount.
  - `stats` / `cacheHit` `$derived` — daemon stats snapshot + computed cache-hit %.
  - `live` / `recent` `$derived` — split `sessions.list` into working/approval vs first 6.
  - `greeting()` — time-of-day salutation string.
  - `kindGlyph(kind)` / `kindTone(kind)` — map feed-item kind (git/github/memory/suggest) to glyph + Badge tone.
  - `startSession()` — `Bridge.NewSession("","","")` → refresh → route to chat.
  - `actOn(it)` — `Bridge.StartFromFeed(dir, task)` for a feed item → refresh → route to chat.
  - `rel(updatedNano)` (ties to `now.ms` shared clock) / `base(dir)` — relative-time + dir-basename formatters.
  - `openSession(s)` — `router.go("chat", s.id)`; `openURL(url)` — `Browser.OpenURL` with `window.open` fallback.
- **Depends on:** `$lib/stores/sessions`, `$lib/stores/daemon`, `$lib/stores/feed`, `$lib/stores/toasts`,
  `$lib/router`, `$lib/stores/clock` (`now`), `$lib/status` (`sessionDot`), `$lib/bridge` (`Bridge`),
  `@wailsio/runtime` (`Browser`), `$lib/types` (`FeedItemDTO`, `SessionInfoDTO`); components `Button`, `Badge`, `StatusDot`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Home />` when `router.route === "home"` (the default route).

### internal/gui/frontend/src/views/Chat.svelte
- **Role:** The live agent conversation and the keystone of the no-leak contract — transcript
  construction, backend subscription, event-listener start and State seed all live in ONE `$effect`
  whose cleanup disposes the transcript and `Unsubscribe`s; plus the right-hand settings dock
  (model/perm/effort/search/fast, title, goal, working dirs, shells, maintenance menu, approval gate).
- **Key symbols:**
  - `sessionId` `$derived` — route param else newest session id.
  - `missing` `$derived` — guard for a routed id that no longer exists (renders a dead-session EmptyState).
  - `loading` `$state` — true from `attach()` until the first State snapshot lands/fails; drives the
    transcript skeleton + keeps the warm starter from flashing before State resolves.
  - Per-session lifecycle `$effect` — resets `sess` on id change (drops the prior session's snapshot —
    GUI-064/066), `createTranscript(id)`, `attach()` (Subscribe + State seed), `t.start()`, registers
    `on(ev.sessionClosed(id))` + `daemon.onReconnect`; cleanup disposes + Unsubscribes.
  - `refreshState()` + two `$effect`s — refetch State when a turn ends (`running===false`) or an approval lands (`approvalSeq>0`).
  - `history` / `live` `$derived` — completed history (stable ref fed to `VirtualList`) and the in-flight
    `live` block rendered as a separate trailing row, NOT concatenated (GUI-069 — avoids re-deriving list geometry per token).
  - `send(text, images)` — routes a matched `/command` through `maybeRunCommand` first, else always tries
    `SteerInput` (inject into the running turn) and falls back to `SendInput` (fresh queued turn); returns
    true on success so the Composer clears its draft/images only then.
  - `maybeRunCommand(text)` + `commandNames` `$state` — leading `/<name> [args]` matching an authored
    custom command runs via `Bridge.RunCommand` (catalog lazy-loaded once via `Bridge.Commands`); returns
    true/false/null (null = send as normal text).
  - `interrupt()` + `interrupting` `$state` (+ guard `$effect`) — `Bridge.Interrupt`, re-entrant guarded
    so mashing Stop fires one RPC; clears on resolve or on running→false (GUI-068).
  - `detachBash()` + `detaching` `$state` — `Bridge.DetachBash` backgrounds the turn's foreground shell to
    free a wedged turn WITHOUT killing it; refreshes so the shell lands in the dock.
  - `approve(approvalID, allow)` — resolve a gated approval, then refresh.
  - `argsOpen` `$state` + `toggleArgs(id)` / `prettyArgs(raw)` — per-approval expand toggle + JSON
    pretty-print of the pending tool's raw args (shows WHAT is being allowed).
  - `pct` / `nearLimit` `$derived` — context-usage % and the >85% compact nudge gate.
  - `applyState(forId, s)` + `run(fn)` — id-guarded state reconcile + error-toasting RPC wrapper.
  - `loadModels()` + `modelsLoaded` + `$effect` — lazy-load routing model catalog when the settings panel
    opens; `effortLevels` `$derived` (model's own ladder else `EFFORT_FALLBACK`); `SEARCH_MODES` const.
  - `onModel/onPerm/onEffort/onSearch/onFast` — capability mutators (`SetModel`/`SetPerm`/`SetEffort`/`SetSearch`/`SetFast`); surfaced both in the right dock AND a top control bar (`.ctl`) above the transcript with a `+ New chat` button (`newChat()` → `Bridge.NewSession` + route).
  - `startNewChat()` + `startingNew`/`newChatOpen`/`newChatDir` `$state` — the control-bar "+ New chat" popover: a recents quick-pick (`RecentDirs()` → `recentDirs`, loaded once on open), a **Browse…** button (`PickDirectory()` → native OS folder dialog), and a free-type fallback; the chosen dir is passed to `NewSession(dir,…)` so a new session's primary root is set at creation (it locks there).
  - **Tools dock** — the right `<aside>` is a tabbed tools panel: `dockTab` (`info|terminal|diff|files|browser`, persisted to localStorage). **Info** = the session meta groups (model/context+compact/title/goal/working-dirs/shells/approvals, all original handlers preserved). **Terminal/Diff/Files** mount-when-active (`<Terminal>`/`<DiffPanel dir={primaryRoot}>`/`<FilesPanel dir={primaryRoot}>` — no PTY/git/fs call until opened). **Browser** (`<BrowserPanel>`) stays mounted once first opened (hidden via display:none) so page state survives switches. `primaryRoot = $derived(sess?.roots?.[0] ?? "")`. The dock is **collapsible** (`dockCollapsed` → a thin glyph rail; click a glyph to expand to that tab) and **drag-resizable** (`dockWidth`, a left-edge `.dock__grip` pointer-drag, clamped 240–680px); both persist to localStorage (`eigen.dockCollapsed`/`eigen.dockWidth`). Default width 340px.
  - control-bar model/effort/search use the custom `<Dropdown>` (not native `<select>` — webkit2gtk black-popup bug); `onModel/onEffort/onSearch` refactored to value-taking `setModel/setEffort/setSearch`, `loadModels()` runs once on mount.
  - voice: `toggleVoiceMode()` toggles the hands-free conversation loop against THIS session (a cleanup `$effect` calls `voice.stopMode()` when the session changes/unmounts so it never listens against a hidden session); `voicePhaseLabel` `$derived` drives the voice-mode banner above the composer (live phase + last transcript + end button); completed assistant prose carries a hover-revealed read-aloud button (`voice.speak`/`stopSpeak`), shown only when `voice.tts` exists.
  - `prettyPath(p)` — collapses a long absolute sandbox root to `…/parent/leaf` for the working-dirs dock (full path stays in the title attr).
  - `startGoal/commitGoal`, `startTitle/commitTitle` + `derivedTitle` `$derived` — inline goal/title editing (`SetGoal`/`SetTitle`).
  - `addDir()` — `Bridge.AddDir` sandbox root; `killShell(shellID)` — `Bridge.KillShell`.
  - `compact()` / `clearSession()` / `resend()` — `Bridge.Compact` / `Bridge.Clear` / `Bridge.Resend` maintenance actions.
  - `isEmpty` / `transcriptLoading` `$derived` + `starters` — warm starter chips for a fresh session vs the loading skeleton.
  - `lastAssistant` `$derived.by` — newest finalized assistant prose, mirrored into an sr-only `role=status`
    live region (VirtualList windows rows, so a SR user otherwise gets no turn-finished cue).
  - `rowLabel(kind)` — SR aria-label per block kind; `noteTone(text)` — derives error vs info note tone
    from the text prefix (`interrupted` / `error:`), since the daemon emits abnormal turn ends as plain notes (GUI-093).
- **Depends on:** `$lib/bridge`, `$lib/stores/daemon`, `$lib/stores/sessions`, `$lib/stores/toasts`,
  `$lib/stores/voice`, `$lib/router`, `$lib/events` (`on`, `ev`), `$lib/stores/transcript` (`createTranscript`, `Transcript`),
  `$lib/types` (`SessionStateDTO`, `ModelDTO`, `ImageDTO`, `RecentDirDTO`); components `Composer`, `ToolCallCard`,
  `Markdown`, `VirtualList`, `Badge`, `Button`, `EmptyState`, `StatusDot`, `Popover`, `Dropdown`, and the
  tools-dock panels `Terminal`/`DiffPanel`/`FilesPanel`/`BrowserPanel`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Chat param={router.param} />` when
  `router.route === "chat"`. Reached from Home/Live/Sessions/Machines via `router.go("chat", id)`.

### internal/gui/frontend/src/views/Live.svelte
- **Role:** The working-now command surface — every session sorted so running / approval floats to
  the top, a 4-status KPI line, per-row Open / Interrupt / Remove (inline confirm) plus an inline
  approval gate (resolve a block without leaving Live). Polls the shared `sessions` store on a ~2s
  self-scheduling timer, gated on online + tab-visible.
- **Key symbols:**
  - Polling `$effect` — self-scheduling `setTimeout(tick, 2000)` that only `sessions.refresh()`es while
    `daemon.status === "online"` and `!document.hidden`; an immediate catch-up fires on `daemon.onReconnect`
    and on `visibilitychange`; cleanup clears the timer + both listeners.
  - `rank` map + `ordered` `$derived.by` — sort copy of `sessions.list` (working/approval/idle/error, newest within bucket).
  - `counts` `$derived.by` — per-status tallies for the KPI line.
  - `isLive(s)` — working or approval; `rel(nano)` (ties to `now.ms` shared clock) / `base(dir)` formatters.
  - `startSession()` — `Bridge.NewSession` → refresh → chat.
  - `open(s)` — route to chat; `interrupt(s)` — `Bridge.Interrupt`; `remove(s)` — `Bridge.RemoveSession`
    (per-id `interrupting`/`removing`/`confirmRemove` guards).
  - Inline gate — `gateOpen`/`gatePending`/`gateLoading`/`gateError`/`acting` `$state` maps; `openGate(s)`
    fetches `Bridge.State(s.id).pending` (falls back to opening Chat when nothing inline to resolve),
    `decide(s, approvalID, allow)` resolves via `Bridge.Approve` then refreshes, `closeGate(id)` clears the maps.
- **Depends on:** `$lib/stores/sessions`, `$lib/stores/daemon`, `$lib/router`, `$lib/bridge`,
  `$lib/stores/toasts`, `$lib/stores/clock` (`now`), `$lib/status` (`sessionDot`), `$lib/types`
  (`SessionInfoDTO`, `ApprovalInfo`); components `Button`, `Badge`, `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Live />` when `router.route === "live"`.

### internal/gui/frontend/src/views/Sessions.svelte
- **Role:** The full session manager / archive — type-to-search across title+dir, newest-first,
  batched "show more", per-row Resume / Export / Delete (inline confirm), header "Prune empty" + total.
  Mutating actions (Export/Delete/Prune) are gated on the daemon connection.
- **Key symbols:**
  - `online` `$derived` (`daemon.status === "online"`) — disables Export/Delete/Prune with a reason when offline.
  - `PAGE`/`shown` + `$effect` — reveal in 40-row batches; reset window when the query changes.
  - `filtered` `$derived.by` — newest-first sort + title/dir substring filter; `visible` slice.
  - `rel(nano)` (uses `now.ms`) / `base(dir)` — formatters.
  - `resume(s)` — route to chat; `exportSession(s)` — `Bridge.ExportSession` → toast the path.
  - `del(s)` — `Bridge.RemoveSession` → refresh; `prune()` — `Bridge.PruneSessions` → toast count → refresh.
- **Depends on:** `$lib/stores/sessions`, `$lib/stores/daemon`, `$lib/router`, `$lib/bridge`,
  `$lib/stores/toasts`, `$lib/stores/clock` (`now`), `$lib/status` (`sessionDot`), `$lib/types`;
  components `Button`, `Badge`, `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Sessions />` when `router.route === "sessions"`
  (also reached via Home's "All sessions" and Chat's dead-session EmptyState).

### internal/gui/frontend/src/views/Machines.svelte
- **Role:** Remote targets — lists hosts the daemon knows (saved eigen remotes + detected ssh-config
  Hosts) as cards; clicking a card opens a slide-over `Sheet` and dials that host over ssh for its
  live session list (slow call, spinner + graceful "couldn't reach" failure). A remote session row's
  "attach" copies the `eigen --remote <ssh>` command (no GUI pump for remote sessions — honest affordance).
- **Key symbols:**
  - `data`/`loading`/`error` + `load()` (`loadSeq` alive-guard) + `$effect` — `Bridge.Machines()` once on mount.
  - `refreshOnReturn` + `$effect` — re-read remotes on window focus / tab-visible (no `eigen:machines`
    push event) so a terminal `eigen remote add` lands without a GUI restart; skips while loading or a
    drill-in sheet is open; header also has a manual refresh button calling `load()`.
  - `openMachine`/`remote`/`remoteLoading`/`remoteError` + `drill(m)` (`remoteSeq` guard) — `Bridge.RemoteSessions(m.ssh)` slide-over.
  - `closeDrill()` — invalidate in-flight dial + reset sheet state.
  - `attach(s)` / `attachCmd(ssh)` + `copiedId` `$state` (+ `attachCmdTimer` cleared in a guard `$effect`) —
    copy the `eigen --remote <ssh>` attach command to the clipboard + toast, with a 1.6s "copied" flash.
  - `base(dir)` formatter (normalizes slash styles, keeps a bare root); `machines` `$derived` from `data.machines`.
- **Depends on:** `$lib/bridge`, `$lib/stores/toasts`, `$lib/status` (`sessionDot`), `$lib/types`
  (`MachinesDTO`, `MachineDTO`, `SessionInfoDTO`); components `Card`, `Badge`, `Button`, `StatusDot`, `Sheet`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Machines />` when `router.route === "machines"`.

### internal/gui/frontend/src/views/Observe.svelte
- **Role:** Telemetry — a live OVERVIEW tab from the 1Hz `daemon.stats` stream (also the leak HUD:
  views/goroutines/heap, with a runtime panel surfacing the daemon's eigen `version` + `vcs_revision`/
  `vcs_modified` + `go_version`) plus lazily-fetched historical sub-views (Routes / Tools / Models /
  Hooks / Subagents / Errors) from the local metadata-only observability log.
- **Key symbols:**
  - `tab` `$state` (type `Tab`) + `tabs` list — tablist switching overview vs historical (now including a `subagents` tab).
  - `s` `$derived` — daemon stats snapshot.
  - `summary`/`summaryLoading`/`disposed` + `loadSummary()` + `$effect`s — `Bridge.ObserveSummary(5000)`
    fetched once on first non-overview tab, disposed-guarded.
  - `mb`/`dur`/`ms`/`k` — byte/duration/latency/count formatters; `shortRev(rev)` — 7-char git SHA for the runtime panel.
  - `cacheHit` `$derived` + `arcR`/`arcCirc`/`arcLen`/`arcGap` — cache-hit gauge geometry (270° SVG arc).
  - `maxCount(items)` — max for proportional bar widths. The Subagents tab reads `summary.subagents`
    (`SubagentStatsDTO`: task/group/mutating calls + errors, backgroundDone).
- **Depends on:** `$lib/stores/daemon`, `$lib/bridge`, `$lib/stores/toasts`, `$lib/types`
  (`ObserveSummaryDTO`); components `Card`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Observe />` when `router.route === "observe"`
  (also reached from Home's stat strip click).

### internal/gui/frontend/src/views/Routing.svelte
- **Role:** The route/model catalog — a provider rail (credential status + model count) filtering a
  model grid that surfaces each model's capabilities (context window, cache, 1M, reasoning/effort,
  search) and availability; topped by a live routing-health strip from the observability log. Read-only.
- **Key symbols:**
  - `data`/`loading`/`error` + `load()` (`loadSeq` guard) — `Bridge.Routing()` catalog.
  - `routes`/`disposed` + `loadRoutes()` — `Bridge.ObserveSummary(5000).routes` for the health strip (best-effort).
  - `routeTotal`/`routeStages`/`routeModelMax` `$derived` — decision totals + stage flow + per-model bar scale.
  - `models` `$derived.by` — provider/availability/text filter over `data.models`.
  - `win(n)` — context-window number formatter; `caps(m)` — capability chip list (cache, 1M, an
    effort RANGE from `m.effortLevels` else `m.effort`, `thinkingBudget`, search, vision, social).
  - `query`/`provFilter`/`onlyAvailable` `$state` — filter controls.
- **Depends on:** `$lib/bridge`, `$lib/stores/toasts`, `$lib/types` (`RoutingDTO`, `ModelDTO`,
  `RouteStatsDTO`); components `Card`, `Badge`, `Button`, `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Routing />` when `router.route === "routing"`.

### internal/gui/frontend/src/views/Crons.svelte
- **Role:** Scheduled work — systemd `--user` timers (controllable start/stop/enable/disable) and the
  user's crontab (read-only). Renders as a schedule: timers ordered by next run with relative ledes,
  a 24h day-position track, and the soonest live run teal-lit; crontab specs decoded to human cadence.
- **Key symbols:**
  - `data`/`loading`/`error` + `load()` (`loadSeq` guard) + `$effect` — `Bridge.Crons()` on mount.
  - `now` `$state` + `$effect` — 1-minute interval keeping "next in …" honest (cleared on cleanup).
  - `parseWhen(s)` — decode "today HH:MM" / "YYYY-MM-DD HH:MM" → timestamp; `relative(ts, ref)` — "in 2h"; `dayPos(ts)` — 0..1 position.
  - `timers` `$derived.by<TimerRow[]>` — timer rows sorted soonest-first; `leadUnit` `$derived` — the
    soonest run that is both active and future (teal-lit lead row).
  - `DOW` + `dowPhrase(dow)` + `cadence(spec)` — decode a crontab day-of-week field to day names, and a
    5-field / `@named` spec to a human cadence string (null when undecodable → "custom schedule").
  - `crontab` `$derived<CronRow[]>` — crontab entries with decoded cadence.
  - `ctl(c, verb)` — `Bridge.SetTimer(unit, verb)` then reload (start/stop/enable/disable).
- **Depends on:** `$lib/bridge`, `$lib/stores/toasts`, `$lib/types`; components `Card`, `Button`, `Badge`,
  `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Crons />` when `router.route === "crons"`.

## Dead code

No dead code found in this slice. Every in-`<script>` function, `$derived`, `$state`, and `$effect`
in all eight files is referenced from that file's own template (the Svelte compiler would flag an
unused declaration; manual cross-check confirms each is wired in). There are no commented-out code
blocks (only explanatory comments) and no unreachable branches. The shared imports they reach into
were verified present and used:
- `$lib/stores/clock` `now.ms` is genuinely consumed (`void now.ms;`) in `Home.svelte`, `Live.svelte`
  and `Sessions.svelte` to tie relative-time labels to the shared 1Hz clock — not dead. (Crons has its
  own local 1-minute `now` `$state`, not the shared clock store.)
- `daemon.onReconnect`, `daemon.status`, `daemon.stats`, `feed.actOn/ideas/fresh/dismiss`, and the
  `Transcript` surface (`history`/`live`/`running`/`truncated`/`approvalSeq`/`seed`/`start`/`dispose`)
  all exist in their stores and are exercised by these views.
- Every `Bridge.*` method called here (`NewSession`, `StartFromFeed`, `RemoveSession`, `PruneSessions`,
  `ExportSession`, `Subscribe`/`Unsubscribe`, `State`, `SendInput(id,text,images,allowTools)`/`SteerInput`,
  `Interrupt`, `DetachBash`, `Resend`, `Approve`, `Compact`, `Clear`, `Commands`, `RunCommand`,
  `SetModel/Perm/Goal/Title/Effort/Search/Fast`, `AddDir`, `KillShell`, `Routing`, `ObserveSummary`,
  `Crons`, `SetTimer`, `Machines`, `RemoteSessions`) is defined in `$lib/bridge` (which wraps the
  generated Wails bindings on the Go `*Bridge`). These are NOT dead even where the wrapper looks thin —
  the underlying Go methods are Wails-bound and called across the TS↔Go seam.

## Cross-links
- **`$lib/bridge` (`bridge.ts`) → Go `*Bridge`** — every view's data and mutations flow through the
  generated Wails bindings into the daemon (`internal/daemon`, `internal/gui` Go side).
- **`$lib/events` + `@wailsio/runtime` Events** — Chat consumes `eigen:session:<id>:event` /
  `:closed`; Observe/Home consume the daemon stats + feed push streams (mirror of Go `pump.go`/`bridge.go`).
- **`$lib/stores`** — `sessions` (Home/Live/Sessions/Chat), `daemon` (Home/Chat/Observe/Live/Sessions —
  `status`/`stats`/`onReconnect`), `feed` (Home), `transcript` (Chat), `clock` (Home/Live/Sessions),
  `toasts` (all). These stores are the reactive seam to the daemon.
- **`$lib/router`** — all views are reached through the hash router switch in `App.svelte`; cross-view
  navigation uses `router.go(...)` (Home/Live/Sessions/Machines → chat).
- **`$lib/status` (`sessionDot`)** — shared session-status→dot mapping used by Home/Live/Sessions/Machines.
- **Shared components (`$lib/components`)** — `Button`, `Badge`, `StatusDot`, `EmptyState`, `Card`,
  `Popover`, `Sheet`, `Composer`, `ToolCallCard`, `Markdown`, `VirtualList` — the GUI components slice.
- **`App.svelte`** — the shell that mounts these views and bootstraps the daemon/feed streams; sibling
  views not in this slice (Agents, Memory, Dreaming, Skills, Profile, Plugins, Config) share the same seam.
