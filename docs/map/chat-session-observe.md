# chat/, session/, observe/

> This slice is the connective tissue between the chat UI and everything that backs it. **`internal/chat`** defines the `Backend` seam — one interface the same rich TUI drives whether the conversation runs **locally** (in-process `agent.Agent`) or **remotely** (a session hosted in the eigen daemon, attached over the socket). **`internal/session`** is the on-disk session store: it discovers conversations across all transcript sources (Claude/Codex/Pi/Hermes/OpenCode/Eigen), indexes them by stable id, ingests each once into eigen-native JSONL, and titles untitled ones with a small async model. **`internal/observe`** is a metadata-only, append-only JSONL activity log: a thin `EventSink` wrapper that records tool calls, errors, routing decisions, hooks, token usage, and runtime samples for learning/observability, plus a tail-reading summarizer. Together they let the TUI (`internal/tui`), daemon (`daemon.go`, `internal/daemon`), and Wails GUI (`internal/app`, `internal/gui`) share one conversation contract, one session catalog, and one observability stream.

## Files

### internal/chat/chat.go

- **Role:** Defines the `Backend` interface — the audited coupling surface between the chat TUI and whatever runs the conversation (local agent or remote daemon session).
- **Key symbols:**
  - `Backend` (interface) — the full contract: `Send`/`Resend` (run/retry a turn), `Messages`/`Tokens`/`Running`/`Compact` (history + context state), model/perm/goal getters+setters, `Title`/`SetTitle`, `Effort`/`SearchMode`/`FastMode` capability state (carried over the socket for remote), `Tools`/`SetTurnTools`, `Shells`/`KillShell`/`DetachBash` (bash shell panel), `AddDir`/`Roots` (sandbox grants), `Steer` (mid-turn injection), `Provider` (live handle, nil for remote), `Reset` (resume/clear), `Wire` (connect event sink + persist callback before first Send), `Answer` (resolve a gated-tool approval by id).
  - `ToolInfo` (struct) — one registered tool for display (`Name`, `ReadOnly`).
  - `ShellInfo` (struct) — one backgrounded bash shell for the shells panel (`ID`, `Command`, `Status`, `ExitCode`, `LastLine`).
- **Depends on:** `internal/agent` (`Permission`, `EventSink`), `internal/llm` (`Message`, `Image`, `Provider`, `Compactor`).
- **Used by / entrypoint:** Implemented by `chat.Local` and `chat.Remote`; consumed by `internal/tui` (`tui.Run(backend chat.Backend, …)` at `internal/tui/tui.go:2042`, field `model.backend`). The whole TUI talks to this interface, never the concrete types (except tests).

### internal/chat/local.go

- **Role:** `Local` — the in-process backend; a thin adapter wrapping `*agent.Agent` + `*agent.Session` so every `Backend` method delegates to the agent the TUI used to touch directly.
- **Key symbols:**
  - `Local` (struct) — holds the agent/session, current `modelID`, user `title`, a `running` flag (so `Steer` knows whether to inject vs start a new turn), and a `map[string]chan bool` of pending gated-tool approvals.
  - `NewLocal(a, history, modelID)` — wraps an agent (resuming history if any) into a `*Local`.
  - `Send` / `Steer` / `Resend` — run a turn (sets/clears `running`); `Steer` injects mid-turn only when a turn is in flight.
  - `Running()` — always returns `false` for local (the TUI itself drives Send, so nothing is ever in flight before the UI starts).
  - `SetModel` — live provider switch (`a.SetLive`), tracks `modelID` for the status bar.
  - `Wire(sink, persist)` — installs `a.OnEvent`/`a.Persist`, and an `a.Approve` callback that surfaces gated tool calls as `EventApproval` events answered via `Answer` (same approval path as remote).
  - `Answer(id, allow)` — resolves a pending approval by id over its channel.
  - `Effort`/`SetEffort`/`SearchMode`/`SetSearch`/`FastSupported`/`FastMode`/`SetFast` — type-assert the live provider to `llm.EffortSetter`/`llm.Searcher`/`llm.FastModer`; return "" / false when unsupported.
  - `Agent() *agent.Agent` — escape hatch exposing the underlying agent for main-side wiring; the TUI must not use it (used by tests only — see dead-code note).
- **Depends on:** `internal/agent` (`Agent`, `Session`, `Event`, `EventSink`, `Permission`), `internal/llm` (`Provider`, `Compactor`, `EffortSetter`, `Searcher`, `FastModer`, `Message`, `Image`).
- **Used by / entrypoint:** Constructed in `main.go` (standalone chat, `chat.NewLocal(a, nil, *model)`) and `smoke_hooks_smoke.go`. `Agent()` reached only from `internal/tui/*_test.go` and `chrome_test.go`.

### internal/chat/remote.go

- **Role:** `Remote` — the daemon-hosted backend; forwards input/commands over the socket via `daemon.Client` and feeds the daemon's event stream into the TUI sink, so the same TUI drives daemon sessions.
- **Key symbols:**
  - `Remote` (struct) — holds `*daemon.Client`, daemon session `id`, a last-synced `*daemon.SessionState` snapshot, the event `sink`, turn-completion signalling (`turnDone` chan + `lastText`/`lastErr`), staged per-turn `turnTools`, and a `detached` flag.
  - `NewRemote(c, sessionID)` — fetches initial state and returns a `*Remote` (unusable until `Wire`).
  - `Wire(sink, _)` — `c.Attach`es to the daemon event stream; ignores `persist` (daemon owns it); drops replayed events; maps the wire shape via `wireToEvent`; closes `turnDone` on a `done` event or terminal `note`, capturing any daemon-side turn error.
  - `wireToEvent(e)` (unexported) — maps `daemon.WireEvent` kinds back to `agent.EventKind`/`agent.Event`.
  - `isTerminalNote(text)` (unexported) — reports whether a note ends a turn (`"interrupted"` or `"error: …"`).
  - `Send` / `Resend` — write input/resend over the socket, block on `turnDone`; on a live view's ctx cancel they `Interrupt` the daemon turn, but a *detached* view's cancel does not (the daemon keeps running).
  - `refresh()` / `Refresh()` — re-sync the cached snapshot; `Refresh` is the exported variant for callers that must re-derive capability bits before reading (e.g. `/fast` after a `/model` switch).
  - `snap()` (unexported) and the snapshot-backed getters: `Messages`/`Tokens`/`Running`/`ModelID`/`ProviderName`/`MaxContextTokens`/`Perm`/`Goal`/`Title`/`Tools`/`Shells`/`Roots`/`Effort`/`SearchMode`/`FastSupported`/`FastMode`.
  - Setters that round-trip the socket then `refresh()`: `SetModel` (sends `p.ModelID()`, NOT `Name()`, so the daemon can rebuild the provider), `SetPerm`, `SetGoal`, `SetTitle`, `SetEffort`, `SetSearch`, `SetFast`, `Compact`, `AddDir`, `KillShell`, `DetachBash`, `Reset` (empty=clear, non-empty=resetTo), `Answer`.
  - `SetTurnTools`/`takeTurnTools` — stage/consume the per-turn allowed-tools allowlist that rides along with the next `Input`.
  - `Steer` — fire-and-forget mid-turn injection (`c.SteerInput`); never blocks.
  - `Provider()` — always nil (the provider can't cross the socket).
  - `SessionID()` / `Sessions()` — implement `SessionLister`: the daemon session id and the list of sibling sessions for the in-window switcher.
  - `Detach()` — implements `Detacher`: releases the view without touching the running turn (unblocks Send, drops the sink).
  - `Interrupt()` — implements `Interrupter`: cancels the daemon's in-flight turn from a view that did NOT start it.
- **Depends on:** `internal/daemon` (`Client`, `SessionState`, `WireEvent`), `internal/agent` (`Event`, `EventKind`, `EventSink`, `Permission`), `internal/llm` (`Message`, `Image`, `Provider`).
- **Used by / entrypoint:** Constructed by `remote_session.go`, `daemon.go`, and `main.go` (`chat.NewRemote(...)`). The extra methods (`Sessions`/`SessionID`/`Detach`/`Interrupt`/`Refresh`) are reached via interface assertions in `internal/tui` (`view.go`, `tray.go`, `nav.go`, `tui.go`, `switches.go`).

### internal/chat/sessions.go

- **Role:** Capability interfaces and the row struct for the in-chat daemon-session switcher (alt+s) — the part of the seam only remote backends implement.
- **Key symbols:**
  - `SessionEntry` (struct) — one daemon-hosted session row: `ID`, `Title`, `Dir`, `Model`, `Status` (idle/working/approval/error), `Turns`, `Views`, `Updated` (unix nano).
  - `SessionLister` (interface) — `Sessions() []SessionEntry` + `SessionID() string`; implemented only by daemon backends (local chats have no siblings).
  - `Detacher` (interface) — `Detach()`; backends whose session outlives the view.
  - `Interrupter` (interface) — `Interrupt() error`; backends that can cancel a turn this view did not start.
- **Depends on:** none (intra-package).
- **Used by / entrypoint:** `internal/tui` asserts the live `Backend` to these interfaces — `view.go`, `tray.go`, `nav.go`, `rail.go`, `tui.go` (`SessionLister`); `nav.go`/`tui.go` (`Detacher`); `tui.go` (`Interrupter`). `chat.Remote` satisfies all three.

### internal/session/session.go

- **Role:** `Store` — the on-disk session index + ingested JSONL copies under `~/.eigen`. Discovers, dedupes, ingests, loads, deletes, and exports conversations across all transcript sources.
- **Key symbols:**
  - `Meta` (struct) — indexed metadata for one session (no bodies): `ID`, `Source`, `Origin`, `OriginMod` (mtime), `Title`, `Cwd`, `Messages`, `Updated`, `Ingested`, `PeekVer`, `Fingerprint`.
  - `Store` (struct) — `dir` (~/.eigen), a `mu`-guarded `map[string]*Meta`.
  - `Open()` — loads/creates the store and its `store/` dir, reading `sessions.json`.
  - `Save()` / `Get(id)` / `List()` — persist the index; fetch one meta; list newest-first deduped by `Fingerprint`.
  - `Discover()` — cheaply scans every source (stat for files via `sourceGlobs`, one DB query for OpenCode), upserts metas, then runs a bounded cheap-preview pass (`transcript.Peek`) to fill cwd/title/turn-count for un-peeked file sessions.
  - `Load(mid)` — returns the full conversation, ingesting the source into eigen-native JSONL on first use (so sources are never re-parsed); sets `Ingested`/`Messages`/`Fingerprint`.
  - `Delete(mid)` — forgets a session and deletes our ingested copy (and, only for eigen-native sessions, the original file + `.meta.json` sidecar); never deletes a foreign source file.
  - `Export(mid, destPath)` — writes a session's full transcript to `destPath` as eigen JSONL (loading/ingesting first).
  - `id(src, origin)` (unexported) — stable sha256-derived `eig_…` id.
  - `fingerprint(msgs)` (unexported) — dedupe hash of first+last user text.
  - `upsert(...)` (unexported) — create/update a meta; a changed mtime clears `Ingested` + `PeekVer`.
  - `sourceGlobs` / `peekVersion` (=2) / `peekBudget` (=400) — source→glob map; preview-logic version bump trigger; per-discover preview cap.
- **Depends on:** `internal/llm` (`Message`, `RoleUser`), `internal/transcript` (`Source`, `Peek`, `Load`, `Save`, `ImportFrom`, `ImportOpenCode`, `ListOpenCodeSessions`, source constants).
- **Used by / entrypoint:** `session.Open()` called from `main.go`, `daemon.go`, `main_gui_wails.go`. `Store` methods reached via `internal/app/data.go` (`d.Store`), `internal/gui/sessions_extra.go`, `internal/app/sessions.go`, and `internal/tui/tui.go`.

### internal/session/title.go

- **Role:** Async titling of untitled sessions using a cheap model, plus per-source extraction of the first user message head.
- **Key symbols:**
  - `Titler` (interface) — `Title(ctx, head) (string, error)`.
  - `ProviderTitler` (struct) — titles via an `llm.Provider`; trims/cleans the response (strips quotes, first line only, ≤80 chars) using `titlePrompt`.
  - `Store.TitleUntitled(ctx, t, limit)` — backgrounds titling of up to `limit` recent untitled sessions, reading only a cheap transcript head per session, with bounded concurrency (sem of 3); titles fill in and persist as they land.
  - `firstUserText(src, origin)` (unexported) — cheaply reads a bounded prefix (≤300 lines, 4 MiB buffer) to get the first user message; returns "" for OpenCode (DB-titled).
  - `userTextFromLine(src, line)` (unexported) — per-source JSONL line decoder for Hermes/Claude/Pi/Codex/Eigen user text.
  - `titlePrompt` (unexported const) — the small-model titling instruction.
- **Depends on:** `internal/llm` (`Provider`, `Request`, `Message`, `RoleUser`), `internal/transcript` (`Source` + source constants).
- **Used by / entrypoint:** `ProviderTitler` constructed in `daemon.go` and `main.go` (`session.ProviderTitler{P: titleProvider(...)}`); `TitleUntitled` called from `main.go:899` and `internal/app/app.go:208`.

### internal/observe/observe.go

- **Role:** `Logger` — appends one JSON `Record` per agent event to an append-only JSONL log; a thin `EventSink` wrapper that is zero-overhead when disabled (nil logger).
- **Key symbols:**
  - `Record` (struct) — flattened durable view of an `agent.Event`: time, session, kind, provider/model, step, tool, skill, error classification (`ErrorKind`/`ErrorHash`, content-free), note/route classification, hook fields, byte lengths (not content), token counts (in/out/cache), and milestone-only runtime samples (mem/heap/goroutines/GC).
  - `Logger` (struct) — `mu`-guarded file + `json.Encoder`, per-turn `toolStart`/`skillStart` timers, `turnStart`, `lastRuntimeSample`.
  - `Open(path, session)` — creates/opens the log (nil logger when `path==""`).
  - `DefaultPath()` — `~/.eigen/observe/events.jsonl`.
  - `Wrap(next)` — returns an `agent.EventSink` that records then forwards to `next` (nil logger returns `next` unchanged).
  - `record(e)` (unexported) — builds a `Record`, computes tool/turn durations, classifies errors/notes, samples runtime on milestone/error events, encodes the line (best-effort; never breaks a turn).
  - `HookObserver()` / `recordHook(o)` — returns a `hook.Observer` that logs hook start/done metadata (with error classification).
  - `Close()` — flushes and closes the file.
  - Unexported classifiers/helpers: `toolKey`, `skillNameFromArgs`, `shouldSampleRuntime`, `classifyError`, `classifyNote`, `applyRouteNote` (parses `routed → model (kind/difficulty; assessor)` notes), `classifyRouteSkip`, `firstNonEmpty`, `hashText` (sha256 prefix), `kindName`.
- **Depends on:** `internal/agent` (`Event`, `EventKind`, `EventSink` + kind constants), `internal/hook` (`Observer`, `Observation`).
- **Used by / entrypoint:** `observe.Open(observe.DefaultPath(), "")` in `build.go` and `main.go`; `Wrap` composed into the agent's `EventWrap` chain (`build.go:301`, `main.go:617`); `HookObserver` passed to `hookRunner.SetObserver`.

### internal/observe/summary.go

- **Role:** Tail-reads `events.jsonl` and aggregates it into a compact, content-free `Summary` (counts + resource maxima), with a human-readable formatter.
- **Key symbols:**
  - `Summary` + sub-structs: `ToolSummary`, `HookSummary`, `SkillSummary`, `ModelSummary`, `RouteSummary`, `SubagentSummary` (with `Total()`), `RuntimeSummary`.
  - `ReadSummary(path, limit)` — tail-reads a bounded window near the end of the file (512 B/record heuristic, 64 MiB cap), skips the partial first line after a seek, parses up to `limit` records, returns `summarize(...)`.
  - `summarize(records)` (unexported) — folds records into the `Summary` (by-kind, tools, errors, notes, hooks, skills, models, routes, subagents, runtime maxima).
  - `FormatSummary(s)` — renders the summary as a multi-section text report.
  - Unexported accumulators/formatters: `accumulateRoute`, `accumulateSkill`, `accumulateSubagentTool`, `writeInlineCounts`, `writeTop`, `sortedKeys` (generic), `safeDiv64`, `bytesHuman`.
- **Depends on:** none beyond stdlib (operates on the `Record` type from observe.go).
- **Used by / entrypoint:** `ReadSummary`/`FormatSummary` called from `internal/tui/observepanel.go`, `main.go` (`eigen observe` CLI path), `internal/gui/observe.go`, and `internal/app/data.go`. `Summary`/`SubagentSummary.Total` consumed by `internal/tui/observepanel.go` and `internal/app/observe.go`.

## Cross-links

- **internal/agent** — `chat` adapts `Agent`/`Session` and routes `Event`/`EventSink`/`Permission`; `observe` wraps the agent `EventSink` and reads `Event` fields (provider/model/tokens/cache).
- **internal/llm** — shared `Message`/`Image`/`Provider`/`Compactor` plus capability interfaces (`EffortSetter`, `Searcher`, `FastModer`) used by `chat.Local`; `session.ProviderTitler` calls `Provider.Complete`.
- **internal/daemon** — `chat.Remote` is a pure client of `daemon.Client`/`SessionState`/`WireEvent`; the daemon hosts the real agent loop and persistence.
- **internal/transcript** — `session.Store` is built on transcript `Peek`/`Load`/`Save`/`ImportFrom`/`ImportOpenCode`/`ListOpenCodeSessions` and `Source` constants; `title.go` decodes per-source JSONL.
- **internal/hook** — `observe.Logger.HookObserver` produces a `hook.Observer`; hook start/done metadata flows into the same log.
- **internal/tui** — the primary consumer: drives `chat.Backend`, asserts `SessionLister`/`Detacher`/`Interrupter`, reads the session `Store`, and renders observe summaries (`observepanel.go`).
- **internal/app + internal/gui** — the Wails GUI side reads the session `Store` (export/list) and renders observe summaries via the same `ReadSummary`/`FormatSummary`.
- **build.go / main.go / daemon.go / remote_session.go** — top-level wiring that constructs `chat.Local`/`chat.Remote`, opens the session `Store`, and installs the observe logger into the agent's `EventWrap` chain.
