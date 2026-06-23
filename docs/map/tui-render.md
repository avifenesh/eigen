# TUI rendering, input affordances & misc

> This slice is the presentation-and-interaction layer of eigen's Bubble Tea
> terminal UI (package `internal/tui`). It owns how transcript content is drawn
> (markdown prose, fenced code, syntax/JSON highlighting, line diffs, the
> right-hand "changes" panel), the brand identity (the breathing λ loader,
> wordmark, welcome screen), and the input affordances that let a user act:
> the unified **action registry** (one validated dispatch path shared by keys,
> clicks, and the command palette), the slash-command dispatcher, `@file`/`/`
> autocomplete, drag-to-select + drag-to-resize, drag-and-drop file paths, image
> attachment, voice/dictation/read-aloud, streaming speech, and attention pings
> (terminal bell + notify hook). It does **not** own the main event loop
> (`tui.go`), layout (`view.go`/`rail.go`), or the agent backend — it provides
> the rendering primitives and handlers those wire together. Most functions are
> methods on the central `*model` (defined elsewhere in the package) or pure
> render helpers callable without it.

## Files

### internal/tui/action.go
- **Role:** The action layer — a single validated dispatch table so a key press, a click on chrome, and the command palette all run the SAME id through the SAME enablement gate.
- **Key symbols:**
  - `actionID` (type) + the `act*` const block — ~34 dispatchable action ids (model picker, perm, effort, voice, rail/panel toggles, tabs, tray, background turn, etc.).
  - `action` (struct) — one registry entry: `id`, `label`, `enabled func(*model) bool`, `run func(*model) tea.Cmd`.
  - `actionRegistry` (map) — the single source of truth mapping id → action.
  - `always` / `idleOnly` / `hasBackend` — enablement predicates (`idleOnly` blocks mutating actions mid-turn).
  - `(*model).dispatch(id)` — runs an action through the gate; disabled/unknown ids are a no-op with a hint.
  - `disabledHint` — appends a short "why it's blocked" reason (e.g. "press esc to interrupt").
- **Depends on:** bubbletea only (model state + methods live across the package).
- **Used by / entrypoint:** `dispatch` is called from key handling (`tui.go`), clickable chrome (`tui.go` mouse path), and the palette (`palette.go`). The `act*` ids are referenced by `composer.go`, `keys`/`palette.go`, etc.

### internal/tui/art.go
- **Role:** Empty-transcript welcome screen — the eigen wordmark, a time-of-day greeting, and starter hints.
- **Key symbols:**
  - `eigenArt` (var) — the ASCII wordmark lines.
  - `greeting()` — time-of-day salutation string.
  - `(*model).welcomeView(width, height)` — renders the centered wordmark + greeting + hints, with a tiny-terminal one-line fallback; sweeps `theme.Spectrum` top→bottom.
- **Depends on:** `internal/theme`, lipgloss, `charmbracelet/x/ansi` (true display-width centering).
- **Used by / entrypoint:** `tui.go` (`welcomeView`) when the transcript is empty.

### internal/tui/attach.go
- **Role:** Image attachment — pulls image files referenced in the prompt (or pasted from the clipboard) into the turn as vision inputs when the model supports vision.
- **Key symbols:**
  - `maxImageBytes` (const, 8MB) — per-image cap.
  - `imageMediaType(path)` — extension → IANA media type ("" if not an image).
  - `extractImages(prompt)` — loads readable image files named in the prompt; returns `[]llm.Image` + per-file skip notes (prompt text unchanged).
  - `referencesImage(prompt)` — cheap "does this name an image file" check, biases routing toward a vision model.
  - `promptTokens(s)` — whitespace tokenizer that keeps quoted spans together.
  - `unwrapToken` / `expandHome` — strip `@`/quotes; resolve `~/`.
  - `(*model).pasteImage()` — clipboard image → `pendingImages`, fails open on unknown-vision models.
- **Depends on:** `internal/clipboard`, `internal/llm` (`llm.Image`, `llm.Vision`).
- **Used by / entrypoint:** `tui.go` submit path (`extractImages`, `referencesImage`), paste key (`pasteImage`); `promptTokens`/`imageMediaType` covered by `attach_test.go`.

### internal/tui/blocks.go
- **Role:** The transcript block model and its rendering — the core of how each conversation unit (user/assistant text, thinking, tool calls, notes) is drawn, including markdown prose, fenced code blocks, and per-tool headers/diffs/JSON/code framing.
- **Key symbols:**
  - `blockKind` / `toolState` (types + consts) — text/thinking/tool/note; running/done/failed.
  - `block` (struct) — one selectable/collapsible unit, with a render cache (`rcache`/`rkey`/`wrapW`) valid because content is append-only.
  - `(*block).collapsible()` / `renderWrapped(selected, width)` / `render(selected)` — cache-keyed width-wrapped render; `render` is the big switch over kinds.
  - `(*block).statusGlyph()` — live ✓/✗/spinner for tool blocks (uses package `animFrame`).
  - `animFrame` (package var) — shared transcript spinner frame, advanced by the spinner tick.
  - `(*block).header()` / `toolSummary(name, args)` — compact per-tool header text (e.g. `read src/main.go`).
  - `(*block).editPath()` / `codeResult(full)` / `toolDetail()` — language-aware edit path; framed source-code result; +/- diff text for edit/multiedit/apply_patch/write.
  - `renderCodeBlock(code, lang, codeW)` — framed, syntax-tinted code surface (chroma, falling back to the heuristic tinter).
  - `normalizePatch` — unified diff → the block diff dialect.
  - `renderProse(s, width)` — lightweight markdown renderer (tables, fenced code, headings, quotes, lists, inline spans).
  - `headingLevel` / `bulletItem` / `orderedItem` / `renderInline` / `previewLine` / `gutterRule` / `toolIcon` / `isCodePath` / `langForPath` — prose/format helpers.
- **Depends on:** `internal/theme`, lipgloss; calls into `diffview.go`, `highlight.go`, `codetint.go`, `jsonview.go`, `table.go`.
- **Used by / entrypoint:** `tui.go`/`view.go`/`rail.go` render the transcript via `renderWrapped`; `changes.go` reuses `toolDetail`/`editPath`/`langForPath`.

### internal/tui/brand.go
- **Role:** The eigen identity — the breathing λ loader and brand mark (the mark is "alive" exactly when the agent is working).
- **Key symbols:**
  - `brandGlyph` (const "λ"), `breathRamp` / `workingRamp` (theme-owned brightness cycles).
  - `breathDot(frame)` — synced pulse after the caret.
  - `breathingLambda(frame)` / `workingLambda(frame)` — λ rendered at the frame's brightness (teal vs. orange "working" ramp).
  - `loaderView(frame)` — the full working loader (breathing λ + caret + dot).
  - `(*model).brandMark()` — static λ when idle, breathing when `stRunning`.
- **Depends on:** `internal/theme`, lipgloss.
- **Used by / entrypoint:** `view.go` (`loaderView`), `rail.go` (`workingLambda`), `sidebar.go` (`brandMark`), `ping.go` (`brandGlyph` in tab titles).

### internal/tui/changes.go
- **Role:** The right "changes" panel — a per-run index of edited files (+adds/−dels) plus an inline-diff view, derived entirely from the transcript's edit-family tool blocks (no separate data feed). Also defines the right-panel width geometry and is the tab-router into the other right-panel tabs.
- **Key symbols:**
  - `rightPanelWidthCols` / `rightMinW` / `rightMaxW` / `minTranscriptCols` (consts) — panel geometry.
  - `fileChange` (struct) — one touched file with stats + jump-to block index.
  - `(*model).changesVisible()` / `rightCols()` / `rightPanelWidth()` / `setRightW(w)` — visibility + width clamp + resize→reflow→reshape-terminal.
  - `(*model).lastRunChanges()` / `changesSig()` / `computeLastRunChanges()` / `collectChanges(start,end)` — cached scan of the latest edit-producing run, segmented at user messages.
  - `editsInBlock(b, idx)` / `filesInPatch(patch, idx)` — extract file changes from a tool block / unified patch.
  - `changesView` (struct), `(*model).buildChangesView()` — memoized flat line list + per-line file index for click-jump.
  - `expandTabs(s)` — tab→spaces (shared helper; panel width math counts `\t` as 1 col).
  - `(*model).diffForChange(fc)` / `patchSection(detail, path)` — colored diff for one file, filtered for multi-file patches.
  - `(*model).changesLines(h)` — the tab router: dispatches to `gitLines`/`termLines`/`tasksLines`/`observeLines`/`goalLines`/`shellsLines`/`notepadLines`, else renders the changes diff windowed by scroll.
  - `changesPad` / `changesRowAt(localY)` / `(*model).jumpToChange(i)` — panel padding on the Surface tint; click row → file index → select+scroll the source block.
  - `itoa(n)` — tiny int→string (avoids strconv).
- **Depends on:** `internal/theme`, bubbletea, `charmbracelet/x/ansi`; calls `toolDetail`/`langForPath` (blocks.go) and `renderDiffLang` (diffview.go).
- **Used by / entrypoint:** `rail.go` calls `changesLines`; `tui.go` mouse path calls `changesRowAt`/`jumpToChange`/`resizeEdgeAt`→`setRightW`; toggles/tabs come through the action registry.

### internal/tui/codetint.go
- **Role:** Heuristic (non-parser) syntax tinting for code lines — the fallback used when chroma (`highlight.go`) finds no lexer.
- **Key symbols:**
  - `tintComment` / `tintString` / `tintKeyword` / `tintNumber` (styles), `codeKeywords` (cross-language set), `reCodeNumber`.
  - `tintCodeLine(line, base)` — tints one tab-expanded line (comment split, then strings/keywords/numbers).
  - `splitComment(s)` / `tintCodeSegment(b, s, base)` / `isWordByte(c)` — supporting passes.
- **Depends on:** `internal/theme`, lipgloss, stdlib `regexp`.
- **Used by / entrypoint:** `blocks.go` `renderCodeBlock` (fallback branch).

### internal/tui/commands.go
- **Role:** Slash-command dispatch — the big `command()` switch (`/help`, `/model`, `/compact`, `/resume`, `/config`, `/goal`, `/route`, `/ban`, `/voice`, custom commands, etc.) plus session load/save/export helpers.
- **Key symbols:**
  - `(*model).command(line)` — parses and dispatches every slash command; returns an optional `tea.Cmd`.
  - `safeWhileRunning(name)` — whether a command may run mid-turn (read-only/settings vs. session-mutating).
  - `(*model).loadSession`/`loadSessionByID`/`applyResumed` — resume from store id or transcript file.
  - `(*model).openAppPageCmd(page)` — quit-to-app-shell for `/home`, `/plugins`, `/hooks`.
  - `(*model).isCustomCommand` / `runCustomCommand(name, arg)` — `~/.eigen/commands` Claude-format custom commands (honors `model:`/`allowed-tools` frontmatter).
  - `defaultSessionPath` / `defaultExportPath` / `sessionMarkdown` — file targets + markdown export.
  - `valueOrUnset` / `configOptionsHint` / `splitBan` — `/config` and `/ban` parsing helpers.
- **Depends on:** `internal/agent`, `internal/command`, `internal/config`, `internal/hook`, `internal/llm`, `internal/transcript`, `internal/voice`, bubbletea.
- **Used by / entrypoint:** `command()` is invoked from the input submit path (`tui.go`/`input.go`) and from action-registry `run` funcs (`m.command("/model")` etc.); the palette and several `act*` actions route here.

### internal/tui/completion.go
- **Role:** Autocomplete popup for the input box — `/command` and `@file` mention completion, including the cached project file index.
- **Key symbols:**
  - `compKind` (consts none/slash/mention), `compItem`, `completion` (popup state) + `active()`/`rows()`.
  - `maxCompRows` (const 8).
  - `slashCmd` + `slashCommands` (var) — the full ordered menu of built-in slash commands with descriptions.
  - `(*model).refreshCompletion()` — recompute popup from current input (matches built-ins + custom commands, or `@file` matches).
  - `(*model).applyCompletion()` — replace the active token with the highlighted item.
  - `(*model).compMenuView()` — render the popup rows above the input.
  - `(*model).fileMatches(token)` / `indexFiles(root)` — ranked `@file` matches over a briefly-cached project walk; `skipDir`, `maxIndexedFiles`, `fileIdxTTL`, `fileIdxBudget`, `fileIndexWalkCount`.
- **Depends on:** `internal/command`, stdlib `io/fs`/`path/filepath`/`sort`/`sync/atomic`.
- **Used by / entrypoint:** `input.go`/`tui.go` (`refreshCompletion`, `applyCompletion`), `view.go` (`compMenuView`).

### internal/tui/diffview.go
- **Role:** Diff computation and rendering — line-level LCS diffs with collapsed context, +N −M stats, and ANSI/syntax-aware styling for edit/patch tool blocks and the changes panel.
- **Key symbols:**
  - `maxDiffLines` (200) / `diffContextLines` (2).
  - `diffText(old, new)` — plain +/- diff text (cache/preview-safe).
  - `collapseContext` / `lcsDiff` — context folding + LCS core.
  - `diffStats(detail)` / `statsSuffix(detail)` — counts + " (+N −M)" header suffix.
  - `renderDiff(s)` — thin wrapper = `renderDiffLang(s, "")` (test seam; see dead-code note).
  - `renderDiffLang(s, lang)` — per-line colored diff with syntax-highlighted code, uniform tint bands, ± markers; falls back to plain marker coloring.
- **Depends on:** `internal/theme`, lipgloss, `charmbracelet/x/ansi`; calls `highlightCode` (highlight.go).
- **Used by / entrypoint:** `blocks.go` (`renderDiffLang` in tool-block render), `changes.go` (`diffForChange`); `diffText`/`diffStats` used by blocks.go/changes.go; `renderDiff` only by `blocks_test.go`.

### internal/tui/drop.go
- **Role:** Drag-and-drop file support — normalizes a bracketed-paste file path payload (file:// URIs, quoted/escaped/percent-encoded, multi-file) into clean path tokens the model reads like `@file` mentions.
- **Key symbols:**
  - `looksLikeDrop(s)` — is this payload one-or-more dropped paths (vs. prose)?
  - `normalizeDropped(s)` — payload → space-joined clean path tokens (spaces quoted).
  - `splitDropTokens` / `decodePath` / `quoteIfSpaced` / `nonEmptyFields` — tokenize, decode file:// + unescape, requote.
- **Depends on:** stdlib `net/url`, `strings`.
- **Used by / entrypoint:** `tui.go` paste handler (`normalizeDropped` when a paste looks like a drop).

### internal/tui/grow.go
- **Role:** Terminal/pane "growing" — when a side panel is toggled on but the terminal is too narrow, asks the surrounding multiplexer (zellij/tmux) or the terminal window to widen the pane.
- **Key symbols:**
  - `growDoneMsg` — outcome message (want/got/ok/unsupported/triedWindow).
  - `termWidth()` — real tty width (m.width lags the WindowSizeMsg).
  - `growToWidth(target)` — async `tea.Cmd`: try zellij, then tmux, then XTWINOPS escape; verify and report honestly.
  - `growWindow(targetCols)` — XTWINOPS `CSI 8;rows;cols t` resize via /dev/tty.
  - `growZellij(target)` — stepwise directional zellij resize with progress polling.
  - `(*model).railNeededWidth()` / `rightNeededWidth()` — minimum terminal widths for the panels.
- **Depends on:** bubbletea, `charmbracelet/x/term`, stdlib os/exec.
- **Used by / entrypoint:** `panel_toggles.go` calls `growToWidth`/`railNeededWidth`/`rightNeededWidth` when a panel won't fit.

### internal/tui/highlight.go
- **Role:** Real per-language syntax highlighting via chroma lexers, mapped onto eigen's theme palette (not a stock chroma theme). Primary highlighter; `codetint.go` is the fallback.
- **Key symbols:**
  - `highlightCode(code, lang, bg)` — tokenize with chroma and render each token in a palette role style; `(string, bool)` (false → caller uses the heuristic).
  - `lexerFor(lang, code)` — resolve lexer by name then content analysis.
  - `codeStyles` (struct) + `codeTokenStyles(bg)` — the role styles (keyword/str/num/comment/fn/typ/text/punct) on a background.
  - `(codeStyles).style(t)` — collapse chroma's fine token categories onto the small role set.
- **Depends on:** `alecthomas/chroma/v2` (+ lexers), `internal/theme`, lipgloss.
- **Used by / entrypoint:** `blocks.go` (`renderCodeBlock`), `diffview.go` (`renderDiffLang`).

### internal/tui/jsonview.go
- **Role:** JSON pretty-printing + tinting — tool results and ```json blocks that are raw JSON get 2-space indent and role-colored keys/values/punctuation.
- **Key symbols:**
  - `looksLikeJSON(s)` — cheap prefix + `json.Valid` check (objects/arrays only).
  - `jsonStyles` (struct) + `jsonStylesOn(bg)` — fg styles (key/str/num/lit/punct) with optional background.
  - `renderJSON(s, bg)` — indent + tint line-by-line.
  - `tintJSONLine(ln, st)` — token scanner (string-key vs. value, numbers, literals, punctuation).
- **Depends on:** `internal/theme`, lipgloss, stdlib `encoding/json`/`bytes`.
- **Used by / entrypoint:** `blocks.go` render (`looksLikeJSON`/`renderJSON` for JSON tool results).

### internal/tui/ping.go
- **Role:** Attention signals — terminal bell + optional notify command on long-turn completion / approvals, terminal tab-title updates, and the idle goal-nag schedule.
- **Key symbols:**
  - `pingMinTurn` (30s) / `goalNagInterval` (5m).
  - `bell()` / `setTermTitle(s)` / `(*model).setTitleThrottled(s)` — BEL, OSC-2 title, change-only title writes.
  - `titleWorking(secs)` / `titleReady()` — tab title strings (use `brandGlyph`).
  - `(*model).notifyCmdline()` / `ping(msg)` — resolve + fire the external notifier (fire-and-forget) plus bell.
  - `(*model).pingOnTurnDone(err)` — ping + in-app flash after long turns only.
  - `goalNagMsg` + `(*model).scheduleGoalNag()` / `handleGoalNag(msg)` — idle goal reminders, generation-guarded against stale timers.
- **Depends on:** bubbletea, stdlib os/exec.
- **Used by / entrypoint:** `tui.go` event loop (turn-start/turn-done title flips, `ping`, `pingOnTurnDone`, goal-nag handling), `commands.go` (`scheduleGoalNag`).

### internal/tui/plugin_commands.go
- **Role:** TUI wrappers for the plugin + marketplace registry operations behind `/plugin` and `/marketplace`. User-typed only (not an agent tool) and intentionally not `safeWhileRunning`.
- **Key symbols:**
  - `marketplaceTimeout` (90s) / `pluginTimeout` (120s).
  - `(*model).pluginCommand(arg)` / `pluginList` / `pluginInstall` / `pluginScanner()` — list/install/remove/enable/disable; scanner uses the live provider.
  - `formatPluginInstallResult` / `formatMarketplaceAdded` — human-readable result text.
  - `(*model).marketplaceCommand(arg)` / `marketplaceList` / `marketplaceUpdate` — marketplace add/update/remove/enable/disable.
  - `(*model).commandError(s)` — push an error note block.
  - `pluginInstallArgs` + `parsePluginInstallArgs` / `splitPluginMarket` — install flag/`name@marketplace` parsing.
- **Depends on:** `internal/llm`, `internal/plugin`, `internal/skill`, bubbletea, stdlib context.
- **Used by / entrypoint:** `commands.go` routes `/plugin`/`/marketplace` → `pluginCommand`/`marketplaceCommand`.

### internal/tui/resize.go
- **Role:** Drag-to-resize for the side panels — the rail's separator column and the right panel's gutter column are grabbable edges.
- **Key symbols:**
  - `panelResizeStep` (const 4) — keyboard/palette resize delta.
  - `(*model).resizeEdgeAt(x, y)` — is (x,y) on a grabbable edge, and which `region`?
  - `(*model).applyResizeDrag(x)` — live-resize the dragged panel so its edge lands on column x (delegates to `setRailW`/`setRightW`).
- **Depends on:** none beyond package types (`region`, layout from `computeLayout`).
- **Used by / entrypoint:** `tui.go` mouse handler (press → `resizeEdgeAt`, motion → `applyResizeDrag`); `panelResizeStep` used by the rail/panel widen/narrow actions in `action.go`.

### internal/tui/selection.go
- **Role:** Drag-to-select + copy — maps screen coordinates to transcript content points and renders/extracts the highlighted selection.
- **Key symbols:**
  - `point` (struct) + `(point).beforeOrEq(q)` — content position + reading-order compare.
  - `(*model).screenToContent(x, y)` — screen row/col → content `point` (rebases by `topHeight()`, viewport scroll, rail width).
  - `(*model).selectedText()` — plain text under the anchor→cursor selection (newline-joined, trailing space trimmed).
  - `clamp(v, max)` — bound helper.
  - `styleSelect` (reverse-video) + `(*model).showSelection()` — re-render the viewport with the selection highlighted during a drag (sync() restores after).
- **Depends on:** lipgloss, stdlib strings.
- **Used by / entrypoint:** `tui.go` mouse handlers (`screenToContent`, `showSelection`, `selectedText` for copy); `plan.go`/`rail.go` comment on the rebasing.

### internal/tui/speechqueue.go
- **Role:** Streaming speech — speak the first sentence of a reply as soon as it lands; a single speaker loop coalesces accumulated text into one TTS process per batch.
- **Key symbols:**
  - `speechQueue` (struct) + `newSpeechQueue(parent, tts)` — serial TTS queue with kick/done channels and a context cancel.
  - `(*speechQueue).Push` / `Close` / `Stop` / `wake` / `take` / `run(tts)` — append / end-of-input / cancel / coalesce / speaker loop.
  - `(*model).speechStreaming()` — should streamed deltas feed speech now (voice or read-aloud + available TTS)?
  - `(*model).speechFeed(delta)` — buffer deltas, push complete sentences.
  - `(*model).flushSpeech()` — push the tail + close the queue at turn end (returns the queue to await drain).
  - `(*model).dropSpeech()` — kill in-flight streamed speech.
  - `lastSentenceEnd(s)` — index past the last complete sentence (`.!?:` + whitespace, or newline).
- **Depends on:** `internal/voice` (`voice.TTS`), stdlib context/sync.
- **Used by / entrypoint:** `view.go` (`speechFeed` on assistant deltas), `voice.go` (`flushSpeech` in `voiceTurnDone`), `tui.go`/`nav.go`/`commands.go` (`dropSpeech`).

### internal/tui/voice.go
- **Role:** Voice features — three distinct affordances: one-shot dictation, one-shot read-aloud, and full hands-free conversation mode (record → spoken reply → relisten), with VAD-endpointed recording and interrupt-on-speech.
- **Key symbols:**
  - `voiceSpokenMsg` / `voiceSpeechDoneMsg` — transcript-back and speech-finished messages (generation-guarded).
  - `voiceTTS(spk)` — pick conversation-mode TTS (reuses the read-aloud speaker's stack; `EIGEN_VOICE_TTS_CMD` overrides).
  - `voiceState` (consts idle/listening/transcribing/speaking).
  - `(*model).dictateOnce()` — record one utterance, submit as a text turn.
  - `(*model).toggleVoice()` / `exitVoiceMode(note)` / `toggleMute()` — enter/leave conversation mode; mute mic (replies still speak).
  - `(*model).startListening(conv)` / `stopListening(note)` — start/stop one VAD recording off the UI loop.
  - `(*model).handleSpoken(msg)` — route a finished recording (drop stale gens, submit dictation, relisten on silence).
  - `(*model).voiceTurnDone(err)` — at turn end: speak the answer (or drain streamed speech), then relisten; interrupt-on-speech via `voice.InterruptMonitor`.
  - `(*model).speakLastAnswer()` / `stopSpeaking()` — one-shot read-aloud; cancel in-flight TTS.
  - `(*model).micGlyph()` — composer button state string.
- **Depends on:** `internal/speech` (`speech.Speaker`), `internal/voice` (`voice.TTS`, `InterruptMonitor`, `TTSFromArgv`, `DetectTTS`), bubbletea.
- **Used by / entrypoint:** `tui.go` (`handleSpoken`, `voiceTurnDone`, `voiceTTS` at model init), `composer.go` (`micGlyph`), and the `actVoice*`/`actDictate`/`actSpeakAnswer` actions + `/voice`/`/mute`/`/dictate`/`/speak` commands.

## Cross-links

- **internal/theme** — palette, spectrum, role styles, glyphs (`ToolIcon`, `BreathRamp`, syntax hues); pervasive across the rendering files.
- **internal/llm** — `llm.Image`/`llm.Vision`/`llm.Models`/`llm.ParseRef`/`llm.ResolveProvider` for image attachment, model switching, plugin scanner.
- **internal/voice** + **internal/speech** — TTS/STT, VAD recording, interrupt monitoring for the speech queue and voice features.
- **internal/agent** — `agent.Permission`, `agent.GoalStartInstruction` in slash-command dispatch.
- **internal/config** — `/config` field metadata + load/save.
- **internal/command** — custom slash-command discovery + expansion (`/`-completion and dispatch).
- **internal/plugin** + **internal/skill** — `/plugin` and `/marketplace` registry operations + skill scanning.
- **internal/transcript** — `/save`, `/export`, `/resume`, `/rebuild` session I/O.
- **internal/hook** — `OnSessionResume` fired on resume.
- **internal/clipboard** — clipboard image paste.
- **alecthomas/chroma/v2** — third-party lexers/highlighting (external, not internal).
- **Rest of `internal/tui`** (not in this slice, called into by it): `tui.go` (event loop / mouse / submit), `view.go` + `rail.go` + `sidebar.go` + `composer.go` (layout/render wiring), `palette.go` (command palette → `dispatch`), `panel_toggles.go` (panel grow), `table.go` (`renderMarkdownTable`/`isTableSep`/`lineAt`), `input.go`/`nav.go`/`switches.go`/`tray.go`/`plan.go`/`notepad.go`/`taskspanel.go` (shared helpers like `fillBG`/`surfaceHex`/`sb`/`compact`/`dim`, right-panel tab renderers).
