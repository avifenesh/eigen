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
  - `rel(updatedNano)` / `base(dir)` — relative-time + dir-basename formatters.
  - `openSession(s)` — `router.go("chat", s.id)`; `openURL(url)` — `Browser.OpenURL` with `window.open` fallback.
- **Depends on:** `$lib/stores/sessions`, `$lib/stores/daemon`, `$lib/stores/feed`, `$lib/stores/toasts`,
  `$lib/router`, `$lib/status` (`sessionDot`), `$lib/bridge` (`Bridge`), `@wailsio/runtime` (`Browser`),
  `$lib/types`; components `Button`, `Badge`, `StatusDot`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Home />` when `router.route === "home"` (the default route).

### internal/gui/frontend/src/views/Chat.svelte
- **Role:** The live agent conversation and the keystone of the no-leak contract — transcript
  construction, backend subscription, event-listener start and State seed all live in ONE `$effect`
  whose cleanup disposes the transcript and `Unsubscribe`s; plus the right-hand settings dock
  (model/perm/effort/search/fast, title, goal, working dirs, shells, maintenance menu, approval gate).
- **Key symbols:**
  - `sessionId` `$derived` — route param else newest session id.
  - `missing` `$derived` — guard for a routed id that no longer exists (renders a dead-session EmptyState).
  - Per-session lifecycle `$effect` — `createTranscript(id)`, `attach()` (Subscribe + State seed),
    `t.start()`, registers `on(ev.sessionClosed(id))` + `daemon.onReconnect`; cleanup disposes + Unsubscribes.
  - `refreshState()` + two `$effect`s — refetch State when a turn ends (`running===false`) or an approval lands (`approvalSeq>0`).
  - `rows` `$derived.by` — completed history + in-flight `live` block as one keyed list for `VirtualList`.
  - `send(text)` — steers into the running turn (`SteerInput`) else `SendInput`; `interrupt()` — `Bridge.Interrupt`.
  - `approve(approvalID, allow)` — resolve a gated approval, then refresh.
  - `pct` / `nearLimit` `$derived` — context-usage % and the >85% compact nudge gate.
  - `applyState(forId, s)` + `run(fn)` — id-guarded state reconcile + error-toasting RPC wrapper.
  - `loadModels()` + `$effect` — lazy-load routing model catalog when the settings panel opens; `effortLevels` `$derived` from the model.
  - `onModel/onPerm/onEffort/onSearch/onFast` — capability mutators (`SetModel`/`SetPerm`/`SetEffort`/`SetSearch`/`SetFast`).
  - `startGoal/commitGoal`, `startTitle/commitTitle` — inline goal/title editing (`SetGoal`/`SetTitle`).
  - `addDir()` — `Bridge.AddDir` sandbox root; `killShell(shellID)` — `Bridge.KillShell`.
  - `compact()` / `clearSession()` / `resend()` — `Bridge.Compact` / `Bridge.Clear` / `Bridge.Resend` maintenance actions.
  - `isEmpty` `$derived` + `starters` — warm starter chips for a fresh session.
- **Depends on:** `$lib/bridge`, `$lib/stores/daemon`, `$lib/stores/sessions`, `$lib/stores/toasts`,
  `$lib/router`, `$lib/events` (`on`, `ev`), `$lib/stores/transcript` (`createTranscript`, `Transcript`),
  `$lib/types`; components `Composer`, `ToolCallCard`, `Markdown`, `VirtualList`, `Badge`, `Button`,
  `EmptyState`, `StatusDot`, `Popover`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Chat param={router.param} />` when
  `router.route === "chat"`. Reached from Home/Live/Sessions/Machines via `router.go("chat", id)`.

### internal/gui/frontend/src/views/Live.svelte
- **Role:** The working-now command surface — every session sorted so running / approval floats to
  the top, a 4-status KPI line, per-row Open / Interrupt / Remove (inline confirm). Polls the shared
  `sessions` store on a ~2s self-scheduling timer.
- **Key symbols:**
  - Polling `$effect` — self-scheduling `setTimeout(tick, 2000)` calling `sessions.refresh()`; cleanup clears the timer.
  - `rank` map + `ordered` `$derived.by` — sort copy of `sessions.list` (working/approval/idle/error, newest within bucket).
  - `counts` `$derived.by` — per-status tallies for the KPI line.
  - `isLive(s)` — working or approval; `rel(nano)` (ties to `now.ms` shared clock) / `base(dir)` formatters.
  - `startSession()` — `Bridge.NewSession` → refresh → chat.
  - `open(s)` — route to chat; `interrupt(s)` — `Bridge.Interrupt`; `remove(s)` — `Bridge.RemoveSession`.
- **Depends on:** `$lib/stores/sessions`, `$lib/router`, `$lib/bridge`, `$lib/stores/toasts`,
  `$lib/stores/clock` (`now`), `$lib/status` (`sessionDot`), `$lib/types`; components `Button`, `Badge`,
  `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Live />` when `router.route === "live"`.

### internal/gui/frontend/src/views/Sessions.svelte
- **Role:** The full session manager / archive — type-to-search across title+dir, newest-first,
  batched "show more", per-row Resume / Export / Delete (inline confirm), header "Prune empty" + total.
- **Key symbols:**
  - `PAGE`/`shown` + `$effect` — reveal in 40-row batches; reset window when the query changes.
  - `filtered` `$derived.by` — newest-first sort + title/dir substring filter; `visible` slice.
  - `rel(nano)` (uses `now.ms`) / `base(dir)` — formatters.
  - `resume(s)` — route to chat; `exportSession(s)` — `Bridge.ExportSession` → toast the path.
  - `del(s)` — `Bridge.RemoveSession` → refresh; `prune()` — `Bridge.PruneSessions` → toast count → refresh.
- **Depends on:** `$lib/stores/sessions`, `$lib/router`, `$lib/bridge`, `$lib/stores/toasts`,
  `$lib/stores/clock` (`now`), `$lib/status` (`sessionDot`), `$lib/types`; components `Button`, `Badge`,
  `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Sessions />` when `router.route === "sessions"`
  (also reached via Home's "All sessions" and Chat's dead-session EmptyState).

### internal/gui/frontend/src/views/Machines.svelte
- **Role:** Remote targets — lists hosts the daemon knows (saved eigen remotes + detected ssh-config
  Hosts) as cards; clicking a card opens a slide-over `Sheet` and dials that host over ssh for its
  live session list (slow call, spinner + graceful "couldn't reach" failure).
- **Key symbols:**
  - `data`/`loading`/`error` + `load()` (`loadSeq` alive-guard) + `$effect` — `Bridge.Machines()` once on mount.
  - `openMachine`/`remote`/`remoteLoading`/`remoteError` + `drill(m)` (`remoteSeq` guard) — `Bridge.RemoteSessions(m.ssh)` slide-over.
  - `closeDrill()` — invalidate in-flight dial + reset sheet state.
  - `base(dir)` formatter; `machines` `$derived` from `data.machines`.
- **Depends on:** `$lib/bridge`, `$lib/stores/toasts`, `$lib/status` (`sessionDot`), `$lib/types`;
  components `Card`, `Badge`, `Button`, `StatusDot`, `Sheet`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Machines />` when `router.route === "machines"`.

### internal/gui/frontend/src/views/Observe.svelte
- **Role:** Telemetry — a live OVERVIEW tab from the 1Hz `daemon.stats` stream (also the leak HUD:
  views/goroutines/heap) plus lazily-fetched historical sub-views (Routes / Tools / Models / Hooks /
  Errors) from the local metadata-only observability log.
- **Key symbols:**
  - `tab` `$state` + `tabs` list — tablist switching overview vs historical.
  - `s` `$derived` — daemon stats snapshot.
  - `summary`/`summaryLoading`/`disposed` + `loadSummary()` + `$effect`s — `Bridge.ObserveSummary(5000)`
    fetched once on first non-overview tab, disposed-guarded.
  - `mb`/`dur`/`ms`/`k` — byte/duration/latency/count formatters.
  - `cacheHit` `$derived` + `arcR`/`arcCirc`/`arcLen`/`arcGap` — cache-hit gauge geometry (270° SVG arc).
  - `maxCount(items)` — max for proportional bar widths.
- **Depends on:** `$lib/stores/daemon`, `$lib/bridge`, `$lib/stores/toasts`, `$lib/types`; components `Card`, `EmptyState`.
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
  - `win(n)` — context-window number formatter; `caps(m)` — capability chip list.
  - `query`/`provFilter`/`onlyAvailable` `$state` — filter controls.
- **Depends on:** `$lib/bridge`, `$lib/stores/toasts`, `$lib/types`; components `Card`, `Badge`, `Button`,
  `StatusDot`, `EmptyState`.
- **Used by / entrypoint:** entrypoint: `App.svelte` renders `<Routing />` when `router.route === "routing"`.

### internal/gui/frontend/src/views/Crons.svelte
- **Role:** Scheduled work — systemd `--user` timers (controllable start/stop/enable/disable) and the
  user's crontab (read-only). Renders as a schedule: timers ordered by next run with relative ledes,
  a 24h day-position track, and the soonest live run teal-lit; crontab specs decoded to human cadence.
- **Key symbols:**
  - `data`/`loading`/`error` + `load()` (`loadSeq` guard) + `$effect` — `Bridge.Crons()` on mount.
  - `now` `$state` + `$effect` — 1-minute interval keeping "next in …" honest (cleared on cleanup).
  - `parseWhen(s)` — decode "today HH:MM" / "YYYY-MM-DD HH:MM" → timestamp; `relative(ts, ref)` — "in 2h"; `dayPos(ts)` — 0..1 position.
  - `timers` `$derived.by<TimerRow[]>` — timer rows sorted soonest-first; `leadUnit` `$derived` — the lead row's unit.
  - `DOW` + `cadence(spec)` — decode crontab 5-field / `@named` specs to a human cadence string.
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
- `$lib/stores/clock` `now.ms` is genuinely consumed (`void now.ms;`) in `Live.svelte` and
  `Sessions.svelte` to tie relative-time labels to the shared 1Hz clock — not dead.
- `daemon.onReconnect`, `daemon.status`, `daemon.stats`, `feed.actOn/ideas/fresh/dismiss`, and the
  `Transcript` surface (`history`/`live`/`running`/`truncated`/`approvalSeq`/`seed`/`start`/`dispose`)
  all exist in their stores and are exercised by these views.
- Every `Bridge.*` method called here (`NewSession`, `StartFromFeed`, `RemoveSession`, `PruneSessions`,
  `ExportSession`, `Subscribe`/`Unsubscribe`, `State`, `SendInput`/`SteerInput`, `Interrupt`, `Resend`,
  `Approve`, `Compact`, `Clear`, `SetModel/Perm/Goal/Title/Effort/Search/Fast`, `AddDir`, `KillShell`,
  `Routing`, `ObserveSummary`, `Crons`, `SetTimer`, `Machines`, `RemoteSessions`) is defined in
  `$lib/bridge` (which wraps the generated Wails bindings on the Go `*Bridge`). These are NOT dead even
  where the wrapper looks thin — the underlying Go methods are Wails-bound and called across the TS↔Go seam.

## Cross-links
- **`$lib/bridge` (`bridge.ts`) → Go `*Bridge`** — every view's data and mutations flow through the
  generated Wails bindings into the daemon (`internal/daemon`, `internal/gui` Go side).
- **`$lib/events` + `@wailsio/runtime` Events** — Chat consumes `eigen:session:<id>:event` /
  `:closed`; Observe/Home consume the daemon stats + feed push streams (mirror of Go `pump.go`/`bridge.go`).
- **`$lib/stores`** — `sessions` (Home/Live/Sessions/Chat), `daemon` (Home/Chat/Observe), `feed` (Home),
  `transcript` (Chat), `clock` (Live/Sessions), `toasts` (all). These stores are the reactive seam to the daemon.
- **`$lib/router`** — all views are reached through the hash router switch in `App.svelte`; cross-view
  navigation uses `router.go(...)` (Home/Live/Sessions/Machines → chat).
- **`$lib/status` (`sessionDot`)** — shared session-status→dot mapping used by Home/Live/Sessions/Machines.
- **Shared components (`$lib/components`)** — `Button`, `Badge`, `StatusDot`, `EmptyState`, `Card`,
  `Popover`, `Sheet`, `Composer`, `ToolCallCard`, `Markdown`, `VirtualList` — the GUI components slice.
- **`App.svelte`** — the shell that mounts these views and bootstraps the daemon/feed streams; sibling
  views not in this slice (Agents, Memory, Dreaming, Skills, Profile, Plugins, Config) share the same seam.
