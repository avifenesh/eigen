# TUI core loop & layout

> The terminal-first chat UI for Eigen, built on Bubble Tea (`charmbracelet/bubbletea`) + lipgloss. This slice owns the Elm-architecture core — the `model` struct, `Init`/`Update`/`View` loop, the agent-event → transcript-block translation, and the *geometry/chrome* layer: screen-rectangle layout, mouse hit-testing, the auto-growing input box, side rails, the headerless command sidebar, the top header, side-panel chrome, the bottom-line overlay (confirm/rename), the fuzzy command palette, surface-tint background painting, and markdown-table rendering. It does **not** own the individual content renderers (blocks, diff/json views, plan/status bar, voice, panels for git/changes/tasks/shells/terminal/notepad, commands) — those live in sibling files of the same `internal/tui` package and are referenced here as collaborators. The TUI is launched by `tui.Run(backend chat.Backend, o Options)` from `main.go` / `daemon.go` / `remote_session.go`; it drives a `chat.Backend` (local in-process or daemon-hosted) and consumes the agent's `agent.Event` stream.

## Files

### internal/tui/tui.go
- **Role:** Package core — the `model` struct (all UI state), the Bubble Tea `Init`/`Update` loop, the entrypoint `Run`, message types, autosave, idle-dream + compact commands, rebuild/build, and provider-failover logic.
- **Key symbols:**
  - `model` — the giant central state struct: viewport/spinner/textarea, `backend`, transcript `blocks` + selection, all modal flags (`picking`/`switching`/`tray`/`modelPicking`/`conf`/`ov`/`pal`), steer/queue, voice, rails/panels widths, history, ctx tokens, failover.
  - `Run(backend chat.Backend, o Options) (Result, error)` — entrypoint: builds the textarea/spinner, restores history+title, wires the agent event sink + autosave persist hook, runs the Bubble Tea program, fires session hooks, and returns a `Result` (rebuild/switch/openapp + live config).
  - `Options` — run config (initial task, history, store, provider/model, memory, skills, dream-on-idle, router, hooks, no-session-file, input mode, loop restore).
  - `Result` — exit report: `Rebuild`/`SessionPath`/`BinPath`, in-window nav (`SwitchTo`/`OpenApp`/`OpenAppPage`), and live session config (provider/model/perm/effort/search). Read by `main.go`/`daemon.go`/`remote_session.go`.
  - `Router` (interface) — auto-router surface (Enabled/SetEnabled/Providers/Route) for delegated subtasks.
  - `(*model) Init` — seeds rail/tasks, schedules goal/loop, kicks off the initial task.
  - `(*model) Update` — the monster reducer: WindowSize, Key (with modal capture order: term → notepad → voice → ctrl+c → overlay → palette → switcher → tray → picker → conf → model-picker → pending approval → completion → global nav → state-specific), Mouse (resize drag / chrome dispatch / rail+panel clicks / drag-select), and all the async msgs (agentEvent, turnDone, voice, idle, dream, compact, term, build, spinner tick, rail/tasks/shells ticks, flash clear).
  - `(*model) submit`/`resend` — start/restart a turn: render the user block, go `stRunning`, attach images, spawn the backend `Send`/`Resend` goroutine (panic-recovered) → `turnDoneMsg`.
  - `(*model) sync` — rebuilds the viewport content from `blocks`, tracking `blockStart` (line→block map for clicks) and `plainLines` (ANSI-stripped, for drag-copy); shows the welcome wordmark when empty.
  - `(*model) push`/`note`/`text` — append a block (with note-ring bookkeeping).
  - `(*model) showFlash`/`showFlashTone` — transient bottom-right banner with auto-clear timer.
  - `(*model) autosave`/`saveMeta` — persist transcript JSONL + config sidecar (panic-safe).
  - `(*model) dreamCmd`/`scheduleIdleDream`/`newTUIDreamPipeline` — idle memory-v2 reflection into project memory.
  - `(*model) compactCmd` — on-demand `/compact` off the UI goroutine.
  - `(*model) buildCmd`/`findGo`/`eigenSrcDir` — `/rebuild`: build a staging binary, smoke-test, atomically swap.
  - Failover: `failoverChain`/`failoverFor`/`nextFailover`/`failoverOrigin`/`failoverTurns`, `isOverloaded`/`isGPTRoutingError`/`isRateLimit` — provider-overload/routing failover ladder.
  - `renderHistory` — pre-fills the transcript from resumed `llm.Message`s (collapsed thinking/tool blocks).
  - `styleInputBox` — pins every textarea sub-style to the Base bg (avoids terminal-bg leak).
  - `selectLine` — the single unified list-row selection treatment (`▎ ` Sel bar) reused by every picker/palette/tray.
  - Style vars (`styleUser`/`styleText`/`styleTool`/… markdown styles) sourced from `internal/theme`.
  - `compact`/`firstLineOf`/`sb` — small string helpers.
- **Depends on:** `internal/agent`, `internal/chat`, `internal/clipboard`, `internal/dream`, `internal/hook`, `internal/llm`, `internal/memory`, `internal/session`, `internal/skill`, `internal/speech`, `internal/theme`, `internal/transcript`, `internal/voice`; bubbletea/bubbles/lipgloss/x/ansi.
- **Used by / entrypoint:** `tui.Run` is the package entrypoint, called from `main.go`, `daemon.go`, `remote_session.go`, `smoke_hooks_smoke.go`. `model`/`sync`/`push` are used by every other file in the package.

### internal/tui/view.go
- **Role:** Top-level `View` composition (the final render), the agent-event → block translator, and the picker/switcher/model-picker views.
- **Key symbols:**
  - `(*model) View` — assembles the screen: routes to a modal view (picker/switcher/tray/model-picker/config/palette) or builds `header + plan + transcriptBand + bottom + statusBar` (or the headerless sidebar variant), then runs it through `paintBase`.
  - `(*model) renderEvent` — turns each `agent.Event` (TextDelta/ReasoningDelta/ToolStart/ToolResult/Done/Note) into transcript-block mutations; feeds streamed speech; routes `todo` tool calls to the plan panel.
  - `(*model) collapseThinking` — collapses the live "thinking" block once real output follows.
  - `(*model) flashBanner` — right-aligned transient confirmation pill (ok/warn/err tones).
  - `(*model) queuedHint` — "[n queued]" steer/queue indicator.
  - `(*model) pickerView`/`switcherView`/`modelPickerView` — the session-resume picker, in-window session switcher, and `/model` chooser renderers.
  - `statusGlyph` — maps daemon session status → `●○◆✗` rail-language glyph.
  - `dim` — package-wide dim-style shorthand.
- **Depends on:** `internal/agent`, `internal/chat`, `internal/theme`; lipgloss/x/ansi.
- **Used by / entrypoint:** `View` is called by Bubble Tea each frame; `renderEvent` by `Update`'s agentEvent case; `dim`/`statusGlyph`/`selectLine` used across the package.

### internal/tui/layout.go
- **Role:** Screen-geometry foundation — named screen rectangles computed from model state, plus mouse hit-testing with explicit z-order. The single source of truth shared by rendering offsets and mouse mapping.
- **Key symbols:**
  - `rect` + `contains`/`empty` — an absolute 0-based screen rectangle; zero-size = "absent".
  - `region` (iota enum) — region names for hit-testing (`regNone`, `regPlan`, `regTranscript`, `regSpinner`, `regComp`, `regInput`, `regComposer`, `regStatus`, `regHeader`, `regLeftRail`, `regRightPanel`).
  - `layout` — struct holding the rect of every screen area.
  - `(*model) computeLayout` — derives all rects from model state, mirroring the `topHeight`/`inputRows`/`bottomHeight` accounting so read-side geometry never drifts from write-side sizing.
  - `hit` — resolved mouse target (region + action + region-local coords).
  - `(*model) hitTest` — resolves an absolute (x,y) to a region+action in z-order (header/status → rails/panels → composer → input → comp → transcript → plan).
- **Depends on:** none external (pure geometry over `model`); calls `headerActionAt`/`statusActionAt`/`composerActionAt`/`sidebarRowAt`/`panelCloseAt`/`canBackgroundTurn` (sibling files).
- **Used by / entrypoint:** `hitTest` called from `Update`'s mouse handling (tui.go) and `resize.go`; `computeLayout` from `hitTest`, `resize.go`, `plan.go`, and tests.

### internal/tui/surface.go
- **Role:** Surface/elevation background painting — re-asserts a truecolor bg after every ANSI reset so a tint runs edge-to-edge, and guarantees every screen cell sits on the Base canvas (no terminal-bg leak).
- **Key symbols:**
  - `bgSeq`/`hexRGB` — hex → truecolor `48;2;r;g;b` SGR.
  - `fillBG(content, hex, width)` — paints content on a surface bg, padded/truncated, re-asserting the bg after both `\x1b[0m` and the `\x1b[m` shorthand resets.
  - `surfaceHex(c lipgloss.AdaptiveColor)` — the dark-resolved hex for a theme color (note: the doc-comment above it stale-names it "onSurface").
  - `paintBase(view, width, height)` — runs the final composed View through `fillBG` line-by-line on the Base canvas, clamping overflow to keep the interactive bottom rows on tiny terminals.
- **Depends on:** `internal/theme`; lipgloss/x/ansi.
- **Used by / entrypoint:** `paintBase` is the last step of `View`; `fillBG`/`surfaceHex` used by rail.go, sidebar.go, table.go, blocks.go, changes.go.

### internal/tui/loop.go
- **Role:** `/loop` — a prompt that auto-resubmits every interval while the session is idle (the inverse of steer/queue); defers a fire when a turn is running.
- **Key symbols:**
  - `loopMsg` — fires on the loop schedule, `gen`-guarded against stale timers.
  - `defaultLoopInterval`/`minLoopInterval` — 10m default, 30s floor.
  - `(*model) scheduleLoop` — arms the next fire (nil when no loop configured).
  - `(*model) handleLoop` — fires the prompt if still idle, else re-arms.
  - `parseLoopArgs` — splits `[interval] <prompt>` (leading duration token).
- **Depends on:** bubbletea only.
- **Used by / entrypoint:** `scheduleLoop`/`handleLoop` from `Init` + `Update` (loopMsg, turnDone); `parseLoopArgs` from `commands.go` (`/loop`).

### internal/tui/nav.go
- **Role:** Transcript navigation — block selection/expand-collapse, shell-style input history, find, copy, and the in-window session switcher + background-turn helpers.
- **Key symbols:**
  - `(*model) collapsibleIdx`/`moveSel`/`toggleSel` — block-selection cursor over collapsible blocks.
  - `(*model) recordHistory`/`historyPrev`/`historyNext` — ↑/↓ input-history recall with live-draft save.
  - `(*model) copySelected`/`copyTarget` — copy the selected block / input draft / last answer to clipboard.
  - `(*model) findBlocks` — case-insensitive transcript search (`/find`).
  - `(*model) scrollToSelected` — scroll viewport to the selected block.
  - `(*model) toggleAtRow` — map a screen row to a block and toggle it (click handler).
  - `(*model) openSwitcher`/`switchFiltered` — open + fuzzy-filter the daemon session switcher.
  - `(*model) canBackgroundTurn`/`isDaemonBacked`/`backgroundTurn` — move a running daemon turn to the background (detach without interrupting).
- **Depends on:** `internal/chat`, `internal/fuzzy`; bubbletea.
- **Used by / entrypoint:** called from `Update` (key/mouse handlers in tui.go), `commands.go` (`/find`, `/copy`, `/sessions`, `/background`), `action.go`, `rail.go`, `layout.go`.

### internal/tui/input.go
- **Role:** Input-box geometry — row math for the auto-growing textarea, soft-wrap replication (to match bubbles), click-to-position, paste, and the viewport `relayout`.
- **Key symbols:**
  - `(*model) inputRows`/`visualInputRows` — terminal-row height of the input box incl. border, counting soft-wrapped visual rows.
  - `wrappedRowCount`/`splitKeepingSpaces`/`wrapSegments` — replicate bubbles/textarea word-wrap for sizing and click mapping.
  - `(*model) inputTopRow`/`inputPromptWidth`/`clickInInput`/`positionCursorAt` — map a screen click into the textarea (visual row+col → logical line+offset).
  - `(*model) inputCursorCanMoveUp`/`inputCursorCanMoveDown` — decide ↑/↓ = move cursor vs recall history (handles soft-wrapped rows via `LineInfo`).
  - `(*model) pasteIntoInput` — right-click clipboard paste into the input.
  - `(*model) bottomHeight` — total rows the bottom UI occupies (input + status + composer + spinner + comp + overlay).
  - `(*model) resizeInput` — grow/shrink the box to content and relayout.
  - `(*model) relayout` — size the viewport around top chrome + bottom UI + rail/panel widths, then `sync`.
- **Depends on:** x/ansi only.
- **Used by / entrypoint:** `relayout`/`resizeInput` called pervasively from `Update`; click helpers from the mouse handler; `bottomHeight` from `relayout` + tests.

### internal/tui/composer.go
- **Role:** The composer bar (Tier 15) — one row of voice controls anchored under the input (⏺ speak · ▶ read · ◉ voice · ⊘ mute), with renderer and click hit-test sharing one segment layout.
- **Key symbols:**
  - `composerSeg` — one clickable control (text/lit/action).
  - `(*model) composerParts` — assembles segments with live mic state (dictate becomes "stop · listening…", mute appears in conversation mode).
  - `(*model) composerBarVisible` — only on terminals ≥12 rows × ≥40 cols.
  - `(*model) composerBarView` — renders the right-aligned bar row.
  - `(*model) composerActionAt` — maps a bar-local x → segment action (mirrors composerBarView's column math).
- **Depends on:** x/ansi; relies on voice.go (`micGlyph`, voiceState) + action ids.
- **Used by / entrypoint:** `composerBarView` from `View`; `composerActionAt` from `hitTest`; `composerBarVisible` from layout/input/view.

### internal/tui/header.go
- **Role:** The top header bar (Tier 9 Wave 2) — session title + project breadcrumb on the left, right-aligned `[home][sessions][+new][config][◧][◨]` buttons, all click-dispatched through the action registry. Hidden entirely in sidebar mode.
- **Key symbols:**
  - `(*model) headerHeight` — 3 (bordered) / 1 (short terminal) / 0 (sidebar mode); `headerBorderMinRows` threshold.
  - `headerButton` + `(*model) headerButtons` — the right-aligned action set in draw order.
  - `(*model) headerToggleOn` — whether a header button is a panel toggle currently shown (lit vs dim).
  - `(*model) headerTitle`/`headerBreadcrumb`/`sessionDir` — left-side labels (title is the rename target).
  - `(*model) visibleHeaderButtons`/`headerButtonsText` — width-fit button dropping + the right-aligned button string/start-col (shared by render + hit-test).
  - `(*model) headerView` — renders the header (bordered or single-line).
  - `(*model) headerActionAt` — resolves a header-local click → action (title region → rename; a button → its action).
  - `ansiTrunc` — width-aware truncate with ellipsis (used package-wide).
- **Depends on:** `internal/transcript`; x/ansi; consults `actionRegistry` (action.go).
- **Used by / entrypoint:** `headerView` from `View`; `headerActionAt` from `hitTest`; `ansiTrunc` used by rail.go/sidebar.go/header.go/panels.go.

### internal/tui/rail.go
- **Role:** The left session rail (Tier 9 Wave 3) — a persistent narrow column listing the daemon's sibling sessions with live status glyphs; one click hops the window there (reusing the switcher's Detach path). Also owns `transcriptBand` (the rail+transcript+right-panel composite) and the tool spinner frames.
- **Key symbols:**
  - Sizing consts: `railWidthCols`, `railMinW`/`railMaxW`, `railMinTerminalWidth` (80), `railPollEvery`/`railSpinEvery`; `toolSpinnerFrames`.
  - `railSessionLister` (interface) + `(*model) railLister` — the daemon-backed capability the rail needs; nil for local chats.
  - `(*model) siblingSessionCount` — other live daemon sessions (for the /rebuild warning).
  - `(*model) railVisible`/`railCols`/`railWidth`/`setRailW` — rail visibility + effective/clamped width.
  - `railTickMsg` + `(*model) railTick`/`refreshRail` — periodic session-list poll (faster while a sibling works); piggybacks the tasks-badge refresh.
  - `railRow` + `(*model) railGrouped`/`railRows` — the shared row model (project headers + session rows) honoring collapsed state.
  - `clampScrollOffset` + `(*model) clampRailScroll`/`scrollRail`/`visibleRailRows` — scroll math for the session list.
  - `(*model) railGlyph`/`railEntryLabel`/`railEntryRow`/`railProjectOpen`/`railHeaderLabel` + `statusRank` — row rendering (active session pops on Overlay tint via the Focus pointer).
  - `(*model) railLines` — renders the rail as exactly h lines.
  - `railPad`/`railPadOn`/`railContentW` — surface-tinted row padding with a separator gutter (shared with the sidebar).
  - `(*model) transcriptBand` — composes rail | transcript | right-panel into vp.Height rows (used by `View`).
  - `(*model) railRowAt`/`toggleRailProject`/`anyRailCollapsed`/`toggleRailProjects`/`hopToSession` — click/collapse handlers + the hop action.
- **Depends on:** `internal/chat`, `internal/theme`; bubbletea/x/ansi; calls into changes.go (`changesLines`/`rightPanelWidth`), sidebar.go (`sidebarLines`), brand.go (`workingLambda`), panels.go (`panelTitleLine`).
- **Used by / entrypoint:** `transcriptBand`/`railLines` from `View`; rail click/hop/scroll from `Update`'s mouse handler; `railVisible`/`railWidth`/`setRailW` from layout/header/palette/action.

### internal/tui/sidebar.go
- **Role:** Tier 11.5 headerless left command sidebar — THE design on wide terminals: a left column that subsumes the header, top plan panel, and bottom status bar (title, nav actions, status setters, todo plan, embedded rail), with one shared row model (`sidebarRows`) for render + hit-test.
- **Key symbols:**
  - `styleFaint` + `sectionLabel` — faint section dividers ("navigate ──────").
  - `(*model) sessionsCollapseGlyph`/`sessionsHeaderLine` — the embedded sessions header with collapse-all toggle.
  - `sidebarRowKind` (iota: brand/title/cwd/blank/section/nav/status/todoHeader/todo/sessionsHeader/rail) + `sidebarRow`.
  - `(*model) sidebarVisible` — active when `width >= railMinTerminalWidth`.
  - `(*model) sidebarRows` — builds the full row model (brand → title → nav → status setters → todo plan → embedded rail).
  - `(*model) sidebarSessionsHeaderIndex`/`sidebarSessionViewportHeight`/`sidebarSessionAreaAt`/`sidebarVisibleRows` — scroll windowing for the embedded rail section (chrome above stays fixed).
  - `(*model) tasksBadge` — the background-tasks row label (running/failed/done counts; "" = no tasks).
  - `(*model) sidebarStatusRows`/`sidebarStatusLabel` — convert status-bar segments into rows reusing their styles.
  - `(*model) sidebarRowAt` — map a sidebar-local y → visible row.
  - `(*model) sidebarLines` — render the sidebar as exactly h lines (mirrors railLines padding).
- **Depends on:** `internal/theme`; lipgloss; reuses rail.go (`railRows`/`railEntryRow`/`railPad`/`railContentW`/`railGlyph`/`railHeaderLabel`/`clampRailScroll`), plan.go (`statusBarParts`/`todoGlyphStyled`/`maxTodoRows`), brand.go (`brandMark`), panels.go (`panelTitleLine`).
- **Used by / entrypoint:** `sidebarLines` from `transcriptBand`; `sidebarVisible` gates header/plan/statusbar in `View` + `headerHeight`; `sidebarRowAt`/`sidebarSessionAreaAt` from `Update`'s mouse handler + `hitTest`.

### internal/tui/panels.go
- **Role:** Shared side-panel chrome (Tier 11) — a titled panel header line with a visible clickable `[x]` close affordance, used by both side panels.
- **Key symbols:**
  - `panelCloseGlyph` (`[x]`).
  - `panelTitleLine(title, width, close)` — one-line padded panel header with optional right-aligned close.
  - `panelCloseAt(localX, localY, width)` — whether a local click hit the `[x]`.
- **Depends on:** x/ansi.
- **Used by / entrypoint:** `panelTitleLine` from rail.go/sidebar.go/changes.go; `panelCloseAt` from `hitTest` (layout.go).

### internal/tui/overlay.go
- **Role:** A small reusable bottom-line overlay (Tier 9 Wave 0/1) for actions that must not fire silently — a y/n confirm or a single-line text entry (rename). Captures keys while active.
- **Key symbols:**
  - `promptKind` (`promptConfirm`/`promptText`); `overlay` struct (active/kind/message/value/onAccept).
  - `(*model) openConfirm`/`openText` — open a confirm or text prompt.
  - `(*model) overlayKey` — key handler while active (returns handled).
  - `(*model) overlayView` — renders the one-line prompt.
  - `(*model) openPermPicker`/`openCompactPrompt`/`openRename` — the concrete confirm/text flows (perm toggle, compact, rename).
- **Depends on:** `internal/agent` (PermAuto/PermGated); bubbletea.
- **Used by / entrypoint:** `overlayKey` from `Update`; `overlayView` from `View`; openers from action.go (perm/compact), commands.go + header (rename), goal.go (text), taskspanel.go (confirm).

### internal/tui/palette.go
- **Role:** The fuzzy command palette (Tier 9 Wave 5, ctrl+k) — one launcher over every registry action + chrome toggle + common slash command, so everything is keyboard-reachable without memorizing bindings.
- **Key symbols:**
  - `paletteCmd` — one launchable entry (label/hint/action-id/slash/prefill); `palette` state struct.
  - `(*model) paletteCatalog` — the full ordered command set, incl. per-plugin-role task prefills (`agent.PluginRoleNames`).
  - `(*model) openPalette`/`refilterPalette` — open + fuzzy-rank the matches.
  - `(*model) paletteKey` — key handler (nav/run/filter); routes to `dispatch` (action), prefill, or `command` (slash).
  - `(*model) paletteView` — renders the query line + ranked matches (dims disabled actions).
  - `clampInt` — clamp an index into [0,n).
- **Depends on:** `internal/agent`, `internal/fuzzy`; bubbletea; consults `actionRegistry` + `dispatch`/`command`.
- **Used by / entrypoint:** `openPalette`/`paletteKey` from `Update` (ctrl+k); `paletteView` from `View`.

### internal/tui/table.go
- **Role:** Markdown table rendering — turns GFM `| a | b |` pipe tables into aligned, bordered tables on the Surface tint so they read as real document elements.
- **Key symbols:**
  - `lineAt` — safe `lines[i]` access.
  - `isTableSep` — detect a GFM separator row (`|---|:--:|`).
  - `splitRow` — split a row into trimmed cells.
  - `renderMarkdownTable(lines, maxW)` — render a table from `lines[0]` (header) + `lines[1]` (sep); returns rendered lines + consumed count (0 = not a table). Proportionally shrinks columns to fit `maxW`.
- **Depends on:** `internal/theme`; x/ansi; uses surface.go (`fillBG`/`surfaceHex`) + `itoa` (codetint.go).
- **Used by / entrypoint:** `renderMarkdownTable` from blocks.go (assistant prose rendering).

## Cross-links

- **internal/chat** — `chat.Backend` (the session interface: Send/Resend/Messages/Compact/Answer/Wire/Perm/Effort/SearchMode/Title/Goal/Running), plus capability interfaces `chat.Detacher`, `chat.Interrupter`, `chat.SessionLister`, and `chat.SessionEntry`. The TUI is a *view* over a backend that may be local or daemon-hosted.
- **internal/agent** — the `agent.Event`/`agent.EventSink` stream consumed by `renderEvent`; `agent.Perm*` constants; `agent.PluginRoleNames` for palette prefills.
- **internal/llm** — `llm.Message`/`llm.Image`/`llm.Provider`/`llm.ModelInfo`/`llm.Compactor`, `llm.New`, `llm.Vision` (vision-capability catalog).
- **internal/theme** — single source of truth for all colors/styles (`S*` style vars, surface roles Base/Surface/Overlay, status glyphs).
- **internal/transcript** — `transcript.Save`/`SaveMeta`/`LoadMeta`, `SessionMeta` (autosave + config sidecar).
- **internal/memory** + **internal/dream** — idle-dream reflection pipeline (memory-v2 Stage1/Consolidate/Summarize).
- **internal/session** — `session.Store`/`session.Meta` (the resume picker).
- **internal/skill** — `skill.Set` (the `/skills` browser).
- **internal/speech** + **internal/voice** + **internal/clipboard** — TTS/STT, dictation, and copy/paste, surfaced through the `speakerIface`/`clipIface` slices.
- **internal/hook** — session-lifecycle hooks (`OnSessionStart`/`OnSessionStop`).
- **internal/fuzzy** — `fuzzy.Score` for the switcher + palette ranking.
- **Sibling files in `internal/tui` (not in this slice)** — this slice calls into many same-package files: `action.go` (`actionRegistry`/`dispatch`/action ids), `commands.go` (`command`, slash handlers), `plan.go` (`planView`/`statusBarParts`/`statusBarView`/`statusActionAt`/`todoItem`), `blocks.go` (`block`/`renderWrapped`/block kinds), `changes.go` (right panel + `minTranscriptCols`), `voice.go`/`speechqueue.go` (voice state + streamed speech), `ping.go` (attention signals), `brand.go` (`loaderView`/`workingLambda`/`brandMark`/`titleWorking`/`titleReady`/`setTermTitle`), `completion.go` (`@file`/slash autocomplete), `configpanel.go`, `goal.go`, `grow.go`, `resize.go`, `tray.go`, `taskspanel.go`/`shellspanel.go`/`termpanel.go`/`notepad.go`/`gitpanel.go`/`rightpanel.go`, `uiprefs.go`, `switches.go`, `codetint.go`/`highlight.go`.
- **External callers of `tui.Run`** — `main.go`, `daemon.go`, `remote_session.go`, `smoke_hooks_smoke.go` (read the returned `Result` to drive rebuild/switch/open-app navigation).
