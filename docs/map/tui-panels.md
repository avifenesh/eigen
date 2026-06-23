# TUI panels

> The right-hand side panels and chrome of Eigen's terminal chat (the
> Bubble Tea `internal/tui` `model`). This slice owns the tabbed **right panel**
> (changes / git / term / tasks / observe / goal / shells / notes) plus several
> adjacent surfaces: the full-screen in-session **config editor**, the bottom
> **status bar** + pinned **plan** panel, the notifications **tray** overlay, the
> live config **switches** (perm/effort/model/search/fast/input-mode/failover),
> the in-chat **workflow** runner, the panel **toggle** helpers, and persisted
> window-layout **prefs**. These files are almost all methods on the central
> `*model` (defined elsewhere in `internal/tui`); they are invoked from the main
> Update/View loop in `tui.go`, the action registry in `action.go`, and the
> slash-command dispatcher in `commands.go`. Each panel follows the rail's
> "row-model" convention: one builder (`xLines`) is walked by both the renderer
> and the click hit-test so screen geometry can never drift.

## Files

### internal/tui/rightpanel.go
- **Role:** The tabbed right-panel skeleton — tab enum, labels, tab ordering, tab switching, and the header tab-bar render + click hit-test shared by every other panel in this slice.
- **Key symbols:**
  - `rightPanelTab` (int enum) + consts `rightTabChanges`, `rightTabGit`, `rightTabTerminal`, `rightTabTasks`, `rightTabObserve`, `rightTabGoal`, `rightTabShells`, `rightTabNotepad` — the tab identity.
  - `(rightPanelTab).label()` / `.shortLabel()` — full and compressed tab labels (compressed used when the bar overflows width).
  - `(*model).rightTabs()` — the live ordered tab set; observe/goal/shells appear conditionally (observe only when active, goal only when one is set, shells only when the backend hosts background shells and there are some).
  - `(*model).nextRightTab()` — cycles to the next tab, returning its activation `tea.Cmd`.
  - `(*model).setRightTab(t)` — selects a tab; lazily starts the terminal PTY, refreshes git/tasks, kicks shells/notepad tick chains, saves the notepad / unfocuses term on the way out.
  - `(*model).termRows()` — emulator row count for the terminal tab.
  - `(*model).tabsFit(width, short)` — whether the tab bar fits at a given label mode (renderer + hit-test share this so click math can't drift).
  - `(*model).rightPanelTitleLine(width)` — renders the tab bar + close glyph (full → short → single-title fallback).
  - `(*model).tabLabel(t, short)` — picks full vs compressed label for one tab.
  - `(*model).rightPanelTabAt(localX, localY, width)` — maps a header click to a tab switch + activation cmd.
- **Depends on:** charmbracelet `bubbletea`, `x/ansi`; calls into sibling panel files (`refreshGitSummary`, `startTerm`, `refreshTasks`/`tasksTick`, `shellsTick`, `loadNotepad`/`saveNotepad`/`notepadAutosaveTick`).
- **Used by / entrypoint:** `rightPanelTitleLine` is the title row every `xLines()` renderer in this slice prepends; `setRightTab`/`nextRightTab`/`rightPanelTabAt` are reached from `tui.go` (key/mouse handling), `action.go`, and `commands.go`.

### internal/tui/configpanel.go
- **Role:** The in-session `/config` editor — a live, editable full-screen settings panel over `config.Fields()` (defaults for NEW sessions; live session knobs go through `/model` `/perm` `/effort`).
- **Key symbols:**
  - `confPanel` (struct) — transient panel state: cursor idx, inline-editor text, dropdown choices/index, multi-select map, last error/saved-key.
  - `(*model).openConfigPanel()` — resets + shows the panel.
  - `confOptionsFor(f)` — resolves a field's option set, including dynamic catalogs (`providers`, `models`) from `llm.Models()`.
  - `(*model).confSetAndSave(key, value)` — validates via `config.Set` + persists via `config.Save`, updating panel feedback.
  - `(*model).confPanelKey(key)` — full keyboard state machine (dropdown / inline editor / cursor / space-cycle small enums / multi-select for route_providers).
  - `(*model).configPanelView()` — renders the panel (field list, dropdown overlay, description pane, footer hints).
- **Depends on:** `internal/config` (Fields/Get/Set/Save/Load/Path), `internal/llm` (Models).
- **Used by / entrypoint:** `openConfigPanel` from the `/config` command / action registry; `confPanelKey` from `tui.go`'s key router when the config panel is active; `configPanelView` from the View path.

### internal/tui/gitpanel.go
- **Role:** Read-only `[git]` tab — cheap, local git status (branch, ahead/behind, staged/unstaged/untracked, diff stat). Never fetches, never mutates, and (critically) never spawns git from the render path.
- **Key symbols:**
  - `gitSummary` (struct) — the snapshot rendered (Repo/Branch/Ahead/Behind/counts/DiffStat/Err).
  - `gitPanelTimeout` const + `gitPanelCommandCount` (atomic) — 2s per-command timeout and a process-spawn counter.
  - `(*model).gitLines(h)` — renders the tab body from the cached summary (View-safe; returns a placeholder until Update refreshes the cache).
  - `(*model).gitSummaryCached()` — returns the cached summary or a cheap placeholder; never spawns git in View.
  - `(*model).refreshGitSummaryIfVisible()` / `refreshGitSummary()` — Update-owned cache refresh (only when the git tab is visible).
  - `gitSummaryFor(dir)` — runs the actual git porcelain commands and assembles the summary.
  - `gitPanelIn(dir, args...)` — runs one `git` command with a context timeout, bumping the spawn counter.
- **Depends on:** stdlib only (`os/exec`, `context`, `sync/atomic`); shares render helpers (`changesPad`, `ansiTrunc`, `rightPanelTitleLine`, `rightCols`, `sessionDir`) with the model.
- **Used by / entrypoint:** `gitLines` from the right-panel View; `refreshGitSummaryIfVisible` from `tui.go` (Update); `refreshGitSummary` from `setRightTab`. `gitPanelCommandCount` is asserted by `resource_test.go` / `chrome_test.go` (proving git never spawns from View).

### internal/tui/observepanel.go
- **Role:** Read-only `[observe]` tab — a compact rollup of the metadata-only telemetry log (`internal/observe`): event/error/tool counts, routing decisions, subagent/spawn stats, per-model token usage, skills, runtime memory/goroutines.
- **Key symbols:**
  - `(*model).observeLines(h)` — reads `observe.ReadSummary(observe.DefaultPath(), 5000)` and renders the rollup (or a "no telemetry yet" hint).
  - `addCountSection(...)` — appends a titled section of sorted name→count rows.
  - `padObserveLines(...)` — clamps/pads the body to exactly `h` lines.
  - `observeCount` (struct) + `observeCountItems(m)` — sort count maps by value desc, name asc.
  - `sortedObserveKeys[V](m)` — generic sorted-keys helper.
  - `sumCounts(m)` / `observeBytes(n)` — total-counts and human-byte formatting helpers.
  - `_observePanelNoRawContentGuard()` — orphaned no-op (see dead-code).
- **Depends on:** `internal/observe` (DefaultPath, ReadSummary, summary types).
- **Used by / entrypoint:** `observeLines` from the right-panel View when `rightTab == rightTabObserve`.

### internal/tui/shellspanel.go
- **Role:** `[shells]` tab — a human view of the agent's backgrounded bash commands (`bash background=true` / detach). Rows are id/status/command/last-output; a running shell can be killed from the panel. Data comes live from the backend (no disk), so shells live exactly as long as their daemon session.
- **Key symbols:**
  - `shellsRefresh` const (1.5s) + `shellsState` (struct) + `shellsTickMsg` — poll cadence, UI state (expanded/sel/ticking/gen), gen-guarded tick message.
  - `(*model).backendShells()` — nil-safe `backend.Shells()`.
  - `(*model).shellsTick()` — schedules the next poll while the tab is visible.
  - `shellRow` (struct) + `shellRowItem`/`shellRowDetail`/`shellRowKill`/`shellRowEmpty` consts + `(*model).shellsRows()` — the row model shared by renderer + hit-test.
  - `shellGlyph(status)` — maps running/killed/exited to the shared status glyph language.
  - `(*model).shellsLines(h)` — renders the panel body.
  - `(*model).selectedShellID()` — id of the selected shell.
  - `(*model).killBgShell(id)` — stops a shell via the backend, flashing feedback.
  - `(*model).shellsRowAt(localY)` / `shellsClick(localY)` — click hit-test → select/expand a shell or kill via the `[kill]` row.
- **Depends on:** `internal/chat` (ShellInfo, backend `Shells()`/`KillShell()`), `internal/theme` (status glyphs), `bubbletea`.
- **Used by / entrypoint:** `shellsLines` from View; `shellsTick`/`shellsClick` from `tui.go` Update; `rightTabs()` shows the tab only when `backendShells()` is non-empty.

### internal/tui/taskspanel.go
- **Role:** `[tasks]` tab — live visibility into background delegations. Surface is the durable on-disk task store (`agent.LoadBgTasks`): the TUI is a separate process from the daemon hosting the goroutines, so the jsonl records ARE the protocol. Enter/click expands a task; `c`/`[cancel]` drops a cross-process cancel marker.
- **Key symbols:**
  - `tasksRefresh` const (2s) + `tasksState` (struct, last disk read / expanded / sel / scroll / gen) + `tasksTickMsg`.
  - `(*model).tasksTick()` — gen-guarded periodic refresh while visible.
  - `(*model).storeDir()` — task store dir (real `agent.TasksDir()`; tests override).
  - `(*model).refreshTasks()` — re-reads the store, keeps selection/expansion valid.
  - `taskRowKind` + `trTask`/`trDetail`/`trCancel`/`trEmpty` consts + `taskRow` (struct) + `(*model).tasksRows(contentW)` — the row model.
  - `(*model).clampTasksScroll(...)` / `scrollTasks(delta)` — wheel scrolling of the row model.
  - `taskGlyph(status, canceling)` (+ `styleErrTask` alias) — status → glyph.
  - `taskSummaryLine(t, now, w)` / `taskDetailLines(t, w)` — one-line and expanded renders of a task.
  - `shortTaskID(id)` / `compactDuration(d)` / `oneLineTrunc(s, n)` — display helpers.
  - `(*model).tasksLines(h)` — renders the tab (header + scrollable rows).
  - `(*model).tasksRowAt(localY)` / `tasksClick(localY)` — click hit-test → expand or confirm-cancel.
  - `(*model).toggleTaskExpand(i)` / `cancelSelectedTask()` — expand/collapse; confirm overlay → `agent.RequestCancel`.
- **Depends on:** `internal/agent` (BgTask, LoadBgTasks, RequestCancel, TasksDir), `bubbletea`, `x/ansi`.
- **Used by / entrypoint:** `tasksLines` from View; `tasksTick`/`tasksClick`/`scrollTasks` from `tui.go` Update; `refreshTasks`/`tasksTick` from `setRightTab`.

### internal/tui/termpanel.go
- **Role:** `[term]` tab — a REAL embedded terminal: a PTY running the user's shell (`creack/pty`) interpreted by a VT emulator (`x/vt`) and rendered as panel lines. The shell lives in the view process (one per window), torn down on window close; ctrl+g releases focus while keeping the shell running.
- **Key symbols:**
  - `termRefresh` const (70ms) + `termState` (struct: pty/cmd/emu/focused/started/exited/ticking/cols/rows/gen) + `termExitedMsg` / `termTickMsg` (gen-tagged).
  - `(*model).termFocused()` — whether the terminal currently owns keystrokes.
  - `(*model).startTerm(rows)` — lazily launches (or resizes) the shell on a PTY; returns reader+waiter+repaint commands.
  - `drainPTY(f, emu)` — copies PTY output into the thread-safe emulator until EOF.
  - `(*model).termCols()` — emulator column count inside the gutter.
  - `(*model).termTick()` — schedules the next paced repaint (single chain per gen).
  - `(*model).ensureTermSize(rows)` — reshapes PTY + emulator on resize (Update-only).
  - `(*model).killShell()` — tears down the current shell (close PTY → SIGTERM→SIGKILL the process group), bumps gen; idempotent.
  - `(*model).stopTerm()` — full teardown on TUI exit.
  - `(*model).termLines(h)` — renders the emulator grid as panel lines (pure read).
  - `termShellName()` — basename of `$SHELL`.
  - `(*model).termKey(key, msg)` — encodes keystrokes to the PTY when focused; ctrl+g releases; enter restarts after exit.
  - `encodeKey(key, msg)` — Bubble Tea key event → PTY byte sequence (arrows, ctrl chords, runes, alt-prefix).
- **Depends on:** `creack/pty`, `charmbracelet/x/vt`, `bubbletea`, stdlib (`os/exec`, `syscall`, `io`).
- **Used by / entrypoint:** `startTerm` from `setRightTab` and `tui.go` (resize); `termLines` from View; `termFocused`/`termKey` from `tui.go` key routing; `stopTerm` from TUI teardown.

### internal/tui/notepad.go
- **Role:** `[notes]` tab — a freeform per-session scratch pad persisted to `~/.eigen/notepad[-instance]/<id>.md`, surviving detach/restart. Minimal-but-real editing (type/newline/backspace/arrows/home/end); grabs keys only when focused; ctrl+g/esc release + flush (same contract as the terminal tab).
- **Key symbols:**
  - `notepadState` (struct: loaded/loadedFor/lines/cx,cy/scroll/focused/dirty) + `notepadHint` const + `notepadSaveMsg`.
  - `notepadDir()` / `notepadPath(id)` — instance-aware dir and sanitized per-session file path.
  - `(*model).notepadSessionID()` — session key (daemon SessionID or `"local"` fallback).
  - `(*model).loadNotepad()` / `saveNotepad()` — load once per session; atomic-rename flush (removes the file when blank).
  - `splitNoteLines(text)` — text → line buffer (always ≥1 line).
  - `(*model).notepadFocused()` — whether the pad owns keystrokes.
  - `(*model).notepadKey(key, msg)` — editing key handler; ctrl+c saves+quits, ctrl+g/esc release+flush.
  - `notepadInsertText` / `notepadInsertNewline` / `notepadBackspace` / `notepadMoveLeft` / `notepadMoveRight` — buffer edits + cursor moves.
  - `(*model).notepadEnsureCursorVisible()` / `notepadBodyHeight()` — scroll + body-height math.
  - `(*model).notepadLines(h)` — renders body + status row (focus state + char count + dirty dot).
  - `notepadWithCursor(s, cx)` — reverse-video cursor cell.
  - `(*model).notepadClickFocus(localY)` — focuses the pad on a body click.
  - `noteClamp` / `itoaTUI` — tiny helpers.
  - `(*model).notepadAutosaveTick()` — 3s autosave tick.
- **Depends on:** `bubbletea`, `x/ansi` (kept alive via `var _ = ansi.StringWidth`), stdlib (`os`, `path/filepath`).
- **Used by / entrypoint:** `notepadLines` from View; `notepadKey`/`notepadFocused`/`notepadClickFocus` from `tui.go` key/mouse routing; `loadNotepad`/`notepadAutosaveTick` from `setRightTab`; `notepadSaveMsg` handled in `tui.go` Update; `saveNotepad` also called on tab-away in `setRightTab`.

### internal/tui/plan.go
- **Role:** Two pinned chrome surfaces: the **plan** panel above the transcript (live mirror of the agent's todo tool) and the **status bar** below the input (model / perm / input-mode / effort / search / fast / context usage / tok-rate / goal / loop / vision / route / read-aloud), including its clickable segment hit-test and context-budget nudge.
- **Key symbols:**
  - `todoItem` (struct) + `maxTodoRows` const + `(*model).updateTodos(args)` — parse a todo tool call into the plan panel.
  - `(*model).topHeight()` — rows the header + plan occupy (0 in sidebar mode); drives transcript click rebasing.
  - `todoGlyphStyled(status)` — colored todo marker.
  - `(*model).planView()` — renders the pinned plan panel.
  - `(*model).statusBarView()` / `statusBarHeight()` / `statusBarLines()` — render the 1–2 row status bar.
  - `statusSeg` / `statusSegBox` (structs) + `(*model).statusBarParts()` — assemble colored segments with their click actions.
  - `(*model).statusBarLayout()` — packs segments into rows recording column ranges for clicks.
  - `(*model).statusActionAt(x, y)` — maps a status-bar click to its `actionID`.
  - `(*model).ctxStyle()` / `ctxIndicator()` / `refreshCtx()` + `ctxNudgeFrac` const — context-usage color, "~Nk/Mk" indicator, cached-token refresh + one-shot compaction nudge.
  - `kfmt` / `modelShort` / `humanToks` — compact formatters.
  - `(*model).finishTurnStats()` / `liveTokRate()` — per-turn token/rate accounting for the status bar.
- **Depends on:** `internal/agent` (PermAuto), `internal/llm` (HasVision), `charmbracelet/lipgloss`, `x/ansi`; uses many `actX` action IDs (defined in `action.go`).
- **Used by / entrypoint:** `planView`/`statusBarView` from the main View; `topHeight`/`statusBarHeight` from layout math; `statusActionAt` from `tui.go` mouse routing; `updateTodos` from the agent-event handler; `refreshCtx`/`finishTurnStats` from turn lifecycle in `tui.go`.

### internal/tui/goal.go
- **Role:** `[goal]` tab + persistent-goal lifecycle — a "north star" that prevents the session going idle until `goal_achieved` is judge-confirmed. Daemon sessions self-drive; local in-process sessions are auto-continued by the TUI here.
- **Key symbols:**
  - `(*model).goalActive()` — the current persistent goal text ("" = none).
  - `(*model).goalBackendDrives()` — whether the backend (daemon `*chat.Remote`) enforces wakeups itself.
  - `(*model).goalJudgeAvailable()` — whether the `goal_achieved` tool is present.
  - `(*model).maybeStartGoalOnInit()` — wakes a resumed/attached idle session that already has a goal.
  - `(*model).maybeContinueGoal(turnErr)` — re-submits the continue instruction after each turn for local sessions.
  - `(*model).markGoalBackendWorking()` — flips local state to "working on goal" when the daemon is already running it.
  - `(*model).openGoalPanel()` / `openGoalEditor()` / `clearGoalFromPanel()` — open the tab; edit (text overlay → SetGoal + saveMeta + maybe submit); clear.
  - `goalRowKind` + `grStatus`/`grText`/`grBlank`/`grActionEdit`/`grActionClear`/`grHint` consts + `goalRow` (struct) + `(*model).goalRows(contentW)` — the row model.
  - `(*model).goalLines(h)` — renders the tab.
  - `(*model).goalClick(localY)` — click hit-test → edit / clear actions.
- **Depends on:** `internal/agent` (GoalStartInstruction, GoalContinueInstruction), `internal/chat` (Remote), `bubbletea`, `x/ansi`.
- **Used by / entrypoint:** `goalLines` from View; `goalClick`/`maybeContinueGoal`/`maybeStartGoalOnInit`/`markGoalBackendWorking` from `tui.go` lifecycle; `openGoalPanel`/`openGoalEditor` from the `/goal` command, action registry, and status-bar "goal active" click; `goalActive` consulted by `rightTabs()` and `statusBarParts`.

### internal/tui/switches.go
- **Role:** Live (current-session) config switches and the overload-failover window: perm toggle, effort/search/fast cycling, live model switching, input steer↔queue, and detach-running-bash — all persisting to session meta so they survive rebuild/resume.
- **Key symbols:**
  - `(*model).togglePerm()` — gated↔auto (ctrl+a / `/perm`).
  - `(*model).cycleEffort()` — step reasoning effort within the model's level set (ctrl+e / `/effort`).
  - `(*model).switchModelTo(provName, id)` — live model switch reusing the `/model` resolution path; returns an error string.
  - `(*model).cycleModel()` — next model in the catalog (ctrl+o / `/model`).
  - `(*model).startFailover(fallback)` / `endFailover()` — switch to a fallback model for `failoverTurns`, then back (overload handling).
  - `(*model).cycleSearch()` — off→auto→on live search (grok only).
  - `(*model).toggleFast()` — Codex fast/priority tier (refreshes remote state first).
  - `(*model).contextBudgetFor(model)` / `compactorFor(np)` — shared budget rule + compactor chain for switched providers.
  - `(*model).effortLevels()` — effort set valid for the current model.
  - `normalizeInputMode(s)` / `(*model).steering()` / `toggleInputMode()` — input-mode coercion + steer↔queue toggle (alt+q / `/steer` / `/queue`).
  - `(*model).steerOrQueue(task)` — inject mid-turn vs queue for the next turn.
  - `(*model).detachRunningBash()` — background the running foreground bash command (alt+d / ctrl+b) → shells panel.
- **Depends on:** `internal/agent` (PermAuto/PermGated), `internal/chat` (Remote), `internal/llm` (Models/ParseRef/ResolveProvider/ContextBudget/ModelEffortLevels/EffortLevels/CompactorChain/NewCompactor/Provider/Compactor), `bubbletea`.
- **Used by / entrypoint:** reached from `tui.go` key handling, `action.go` (status-bar segment clicks: actPermPicker/actEffortCycle/actSearchCycle/actFastToggle/actInputModeToggle), and `commands.go` slash commands; `startFailover`/`endFailover` from the overload-retry path.

### internal/tui/workflow.go
- **Role:** In-chat `/workflow <name> [k=v …]` runner — loads an authored workflow and plays its prompts in order in the live session (submit step 1, queue the rest). The judged checks / on_failure / exit codes are headless-only (`eigen run`).
- **Key symbols:**
  - `(*model).runWorkflowCmd(arg)` — list workflows (no arg) or load + interpolate + queue/submit the steps.
  - `dedupe(s)` — de-duplicates the reported missing-var list.
- **Depends on:** `internal/workflow` (List, Load, Interpolate), `bubbletea`.
- **Used by / entrypoint:** `runWorkflowCmd` from `commands.go` (the `/workflow` slash command).

### internal/tui/tray.go
- **Role:** Notifications/approvals **tray** overlay — an at-a-glance "needs you" list of sibling daemon sessions (approval-blocked or errored) plus this window's own pending approval and a ring of recent notifications. Selecting a sibling hops the window there.
- **Key symbols:**
  - `trayItem` (struct) — one actionable row (sessionID/title/dir/status/current).
  - `(*model).openTray()` — builds items and shows the overlay.
  - `(*model).buildTrayItems()` — gathers this window's pending approval + sibling sessions in approval/error status.
  - `(*model).trayActivate()` — acts on the selected row (returns handled/quit; quit=true means hop to another session).
  - `(*model).trayView()` — renders the overlay ("needs you" + recent notifications).
  - `projBase(dir)` — base name of a project dir.
- **Depends on:** `internal/chat` (SessionLister, anonymous `SessionID()` interface).
- **Used by / entrypoint:** `openTray` from the tray key/action; `trayActivate`/`trayView` from `tui.go` overlay handling.

### internal/tui/panel_toggles.go
- **Role:** Toggle helpers for the side panels so close/reopen behavior is identical across slash commands, palette entries, shortcuts, and clickable `[x]`/`[◧]`/`[◨]` chrome. When a panel won't fit the width, the toggle asks the surrounding multiplexer (zellij/tmux) to stretch the pane.
- **Key symbols:**
  - `(*model).toggleRail()` — show/hide the session rail (`growToWidth` when it doesn't fit; noted when there's no daemon-hosted chat).
  - `(*model).toggleChanges()` — show/hide the right panel (with first-use hint + grow-to-fit).
  - `(*model).toggleMouse()` — suspend/restore eigen's mouse capture so the terminal's native select-copy works.
- **Depends on:** `bubbletea` (DisableMouse/EnableMouseCellMotion); calls `relayout`, `growToWidth`, `note`, `railVisible`/`changesVisible`.
- **Used by / entrypoint:** all three from `tui.go` key handling, `action.go` (clickable chrome), `commands.go` (`/rail` `/changes` `/mouse`), and the palette.

### internal/tui/uiprefs.go
- **Role:** Window-layout preferences persisted across eigen runs (side-panel widths the user dragged), stored at `~/.eigen/ui.json` — distinct from `config.json` (defaults for NEW agent sessions).
- **Key symbols:**
  - `uiPrefs` (struct) — `RailW` / `RightW` (0 = default).
  - `uiPrefsPath()` — `~/.eigen/ui.json`.
  - `loadUIPrefs()` — best-effort read (any error → zero prefs).
  - `saveUIPrefs(p)` — atomic temp+rename write, best-effort.
  - `(*model).persistPanelWidths()` — saves current rail/right widths after a resize settles.
- **Depends on:** stdlib only (`encoding/json`, `os`, `path/filepath`).
- **Used by / entrypoint:** `loadUIPrefs` from `tui.go` startup; `persistPanelWidths` from `action.go` (keyboard resize steps) and `tui.go` (drag release).

## Cross-links
- **internal/chat** — backends behind the panels: `Shells()`/`KillShell()` (shells), `SessionLister`/`Sessions()` (tray), `*Remote` (goal-backend-drives, fast-mode refresh), `SessionID()` (notepad key, tray current-session). Heaviest dependency of this slice.
- **internal/agent** — durable background-task store (`LoadBgTasks`/`RequestCancel`/`TasksDir`/`BgTask`) for the tasks panel; goal instructions + `PermAuto`/`PermGated` for goal/switches/status bar.
- **internal/llm** — model catalog + provider construction for the config dropdown, live model/effort/search switching, vision flag, and context budget rule (switches, configpanel, plan).
- **internal/config** — field schema + get/set/save/load for the in-session config editor (configpanel).
- **internal/observe** — metadata-only telemetry summary read by the observe panel.
- **internal/workflow** — authored-workflow list/load/interpolate for the in-chat workflow runner.
- **internal/theme** — shared status glyphs (shells panel) and the broader style constants (`styleAccent`, `styleAsk`, `dim`, etc., defined elsewhere in `internal/tui`).
- **internal/tui core (tui.go / action.go / commands.go / layout)** — the *consumers* of this slice: the Update/View loop, the action registry (`actionID` clicks), and the slash-command dispatcher all call into these panel methods; shared render helpers (`changesPad`, `ansiTrunc`, `rightCols`, `relayout`, `note`, `showFlash`, `openConfirm`, `openText`, `saveMeta`) live in those core files.
- **internal/daemon** — indirect: the daemon hosts the sessions/goroutines whose shells (`session.killShell`) and background tasks the shells/tasks panels surface cross-process.
- External libs: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/x/{ansi,vt}`, `creack/pty`.

## Dead code
- `internal/tui/observepanel.go:163` `_observePanelNoRawContentGuard()` — orphaned no-op returning `strings.TrimSpace("metadata-only")`. Grep across the whole repo (including tests) finds zero callers; it satisfies no interface and is behind no build tag. It is also the *only* use of the `strings` import in that file, so removing it requires dropping that import too. High confidence dead.
