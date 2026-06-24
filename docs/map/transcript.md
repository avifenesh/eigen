# transcript/ — cross-tool transcript readers

> `internal/transcript` is eigen's import/export layer for conversation history. It reads session
> files written by other coding agents (Claude Code, Codex, Pi, Hermes, OpenCode) and by eigen
> itself, and normalizes every format into a single `[]llm.Message` so a foreign or prior
> conversation can be resumed and continued inside eigen. It also owns eigen's *native* durable
> transcript format: atomic JSONL writes with rotating backups and backup-based crash recovery
> (`Save`/`SaveForce`/`Load`/`ClearBackups`), a sidecar `*.meta.json` recording the live session
> config for faithful resume (atomic `SaveMeta`/`LoadMeta`), and a cheap parse-free `Peek` that
> extracts a session's working dir, title, and bounded-cost message count for the session/projects
> browser without reading the whole file. The package has one first-party dependency
> (`internal/llm` for the canonical message type) plus `modernc.org/sqlite` for the OpenCode DB
> reader; it is consumed by the CLI (`main.go`, `agent_sessions.go`, `task_status.go`, `daemon.go`),
> the TUI, the session store, the daemon, the GUI, and the app session-copy layer.

## Files

### internal/transcript/transcript.go
- **Role:** Package core — format dispatch, the eigen-native save/load format with atomic writes, rotating backups, backup-based crash recovery, and the shared JSONL scanner + arg-normalization helpers all parsers use.
- **Key symbols:**
  - `type Source string` + consts `SourceEigen`/`SourceClaude`/`SourceCodex`/`SourcePi`/`SourceHermes`/`SourceOpenCode` — string enum identifying a transcript format.
  - `Import(path) ([]llm.Message, error)` — reads a transcript, auto-detecting the source from the path.
  - `ImportFrom(src, path)` — parses with an explicit source; switch dispatches to `parseClaude`/`parseCodex`/`parsePi`/`parseHermes`/`ImportOpenCode`/`Load`.
  - `Detect(path) Source` — guesses source from path substrings (`/.claude/projects/`, `/.codex/sessions/`, `/.pi/agent/sessions/`, `/.hermes/sessions/`, opencode markers; defaults to eigen).
  - `Save(path, msgs) error` — writes eigen-native JSONL atomically. Guards against data loss: **refuses an empty save over a non-empty transcript** (an accidental short/empty autosave would otherwise rotate the good file into a backup then rename a zero-message file over it); returns an error pointing at `SaveForce`.
  - `SaveForce(path, msgs) error` — same atomic write as `Save` but without the empty-over-non-empty guard; the deliberate-clear path (e.g. `/clear`). Pair with `ClearBackups`.
  - `Load(path) ([]llm.Message, error)` — reads eigen-native JSONL (one marshaled `llm.Message` per line); when the primary is missing/empty/zero-message it falls back to the newest readable backup generation (`.bak`..`.bak.4`) so a truncated or vanished primary does not lose the conversation.
  - `ClearBackups(path)` — removes all rotated backup generations; call it alongside a deliberate `Save([])`/`SaveForce(nil)` so `Load`'s recovery cannot resurrect the cleared conversation.
  - `existsNonEmpty`/`saveAtomic` (unexported) — the stat-check behind `Save`'s guard, and the shared atomic-write core (temp file + `Write` + `Sync` + `rotateBackups` + `Rename` + best-effort `syncDir`; tolerates a concurrent racing rename when the destination now exists) used by both `Save` and `SaveForce`.
  - `loadJSONL`/`recoverFromBackup` (unexported) — decode one eigen JSONL file into messages; try `.bak`..`.bak.4` newest-first for the first generation yielding ≥1 message.
  - `scanJSONL(path, fn)` (unexported) — shared per-line JSONL reader (32MB line buffer, skips blank/malformed lines); the backbone of all text-file parsers and `loadJSONL`.
  - `rawArgs(json.RawMessage)` (unexported) — normalizes tool args to a valid JSON object, unwrapping a JSON-string-encoded JSON object.
  - `rotateBackups`/`backupPath`/`syncDir` (unexported) + const `transcriptBackupGenerations = 5` — the `.bak`..`.bak.4` rotation and best-effort directory fsync behind every atomic write.
- **Depends on:** `internal/llm` (the `llm.Message` target type).
- **Used by / entrypoint:** `Import` ← `internal/tui/commands.go`; `ImportFrom` ← `main.go` (`--resume`/`--from`), `internal/session/session.go`; `Detect` ← `main.go`; `Save` ← `main.go`, `task_status.go`, `internal/tui` (tui.go, commands.go), `internal/session`, `internal/daemon/host.go`, `internal/gui/sessions_extra.go`, `internal/app/sessions.go`; `SaveForce`/`ClearBackups` ← `internal/daemon/host.go` (deliberate session clear/removal); `Load` ← `main.go`, `daemon.go`, `task_status.go`, `internal/tui/header.go`, `internal/session`, `internal/daemon` (host.go, persist.go), `internal/gui/sessions_extra.go`, `internal/app/sessions.go`.

### internal/transcript/claude.go
- **Role:** Parser for Claude Code session JSONL (Anthropic Messages format wrapped per line).
- **Key symbols:**
  - `parseClaude(path)` (unexported) — folds assistant `text` + `tool_use` blocks into one assistant message (text blocks joined with `\n` so they stay separated); `tool_result` blocks become tool messages; plain-string content and user-side `text` blocks handled too.
  - `claudeResultText(json.RawMessage)` (unexported) — flattens a `tool_result` content (string or text/image block list) to plain text, joining multiple text blocks with `\n` and skipping non-text (e.g. image) blocks.
  - `role(string) llm.Role` (unexported) — maps a role string to `llm.Role`; handles `user`/`assistant`/`tool`/`toolResult`, passthrough for others.
- **Depends on:** `internal/llm`.
- **Used by / entrypoint:** `parseClaude` ← `ImportFrom` (transcript.go) when `src == SourceClaude`.

### internal/transcript/codex.go
- **Role:** Parser for Codex rollout JSONL (raw OpenAI Responses-API items in `response_item` lines), including encrypted reasoning needed for `store:false` resume.
- **Key symbols:**
  - `parseCodex(path)` (unexported) — groups an assistant turn's `message` text, `reasoning`, and following `function_call`/`custom_tool_call` items into one assistant message; `*_output` items become tool messages; `developer`/`system` messages and event/meta lines flush/are ignored. Uses a local `flush()` accumulator (builds `out` directly, ignoring `scanJSONL`'s return slice). **Reasoning handling:** folds a turn's reasoning into `asst.ReasoningEncrypted` + `asst.ReasoningID` (last item carrying a blob, id+blob taken together) and joins all summary texts into `asst.Reasoning` — mirrors `applyOutputItem` in `internal/llm/codex.go` so a resumed turn carries the encrypted blob back paired with its item id (server 404s otherwise).
  - `codexOutputText(json.RawMessage)` (unexported) — flattens a `*_output`'s `output` (JSON string or Responses-API content-block list) to plain text, concatenating text blocks and skipping non-text (e.g. `input_image`); mirrors `claudeResultText`.
  - `rawArgsString(string) json.RawMessage` (unexported) — turns a tool-arg string into a JSON object: used as-is if valid JSON, else wrapped as `{"input": <string>}`.
- **Depends on:** `internal/llm`.
- **Used by / entrypoint:** `parseCodex` ← `ImportFrom` when `src == SourceCodex`; `rawArgsString` also used by `parseHermes` (hermes.go).

### internal/transcript/hermes.go
- **Role:** Parser for Hermes session JSONL (flat OpenAI chat-completions messages, one per line).
- **Key symbols:**
  - `parseHermes(path)` (unexported) — per-line role switch (user/assistant/tool); assistant `tool_calls` fold into the assistant message via `rawArgsString`.
- **Depends on:** `internal/llm`.
- **Used by / entrypoint:** `parseHermes` ← `ImportFrom` when `src == SourceHermes`.

### internal/transcript/pi.go
- **Role:** Parser for Pi agent session JSONL (`message` lines with a nested role + typed content blocks).
- **Key symbols:**
  - `parsePi(path)` (unexported) — role switch user/assistant/toolResult; assistant `text` + `toolCall` blocks fold into one assistant message; toolResult text blocks become a tool message carrying `ToolName`/`ToolError`.
- **Depends on:** `internal/llm`.
- **Used by / entrypoint:** `parsePi` ← `ImportFrom` when `src == SourcePi`.

### internal/transcript/opencode.go
- **Role:** Reader for OpenCode conversations stored in a SQLite DB (read-only access, safe while OpenCode runs).
- **Key symbols:**
  - `type OpenCodeSession struct{ID, Title string; Updated int64}` — lightweight session metadata.
  - `ListOpenCodeSessions(path) ([]OpenCodeSession, error)` — lists sessions (id/title/time_updated) from the `session` table without parsing messages — cheap discovery.
  - `ImportOpenCode(path, sessionID) ([]llm.Message, error)` — loads one session (most-recent if id empty); joins `message` + `part` rows, folding text/tool parts into assistant messages with tool-result messages following.
  - `openCodeDBPath(path)` (unexported) — resolves the DB path; `""`/`"opencode"` → `~/.local/share/opencode/opencode.db`.
  - `type ocPart struct` (unexported) — JSON shape of a `part` row (type/text/tool/callID + nested state input/output/error/status).
- **Depends on:** `internal/llm`; `modernc.org/sqlite` (blank-imported pure-Go SQLite driver).
- **Used by / entrypoint:** `ImportOpenCode` ← `main.go` (`--resume` opencode), `internal/session/session.go`; `ListOpenCodeSessions` ← `internal/session/session.go` (session indexing); `ImportFrom` (transcript.go) also routes `SourceOpenCode` here with an empty session id.

### internal/transcript/meta.go
- **Role:** Sidecar `*.meta.json` for eigen-native transcripts — records the live session config so resume restores provider/model/perm/effort/search/goal/loop rather than launch defaults.
- **Key symbols:**
  - `type SessionMeta struct` — Dir, Title, Provider, Model, Perm, Effort, Search, Goal, LoopPrompt, LoopEvery (all `omitempty`, optional).
  - `SaveMeta(sessionPath, m) error` — writes `<session>.meta.json` as indented JSON **atomically** (temp file + `Sync` + `Rename` + best-effort `syncDir`); a mid-write crash (happens every turn) never leaves a half-written sidecar that `LoadMeta` would reject and silently reset config. Best-effort to callers (error returned but non-fatal).
  - `LoadMeta(sessionPath) (SessionMeta, bool)` — reads the sidecar; `false` if absent/unreadable/invalid.
  - `metaPath(sessionPath)` (unexported) — `sessionPath + ".meta.json"`.
- **Depends on:** none beyond stdlib (`encoding/json`, `os`, `path/filepath`).
- **Used by / entrypoint:** `SaveMeta` ← `main.go`, `task_status.go`, `internal/tui/tui.go`; `LoadMeta` ← `main.go` (resume restore), `internal/tui/header.go`, and `peekEigen` in this package; `SessionMeta` fields written/read in `main.go`, `internal/tui/tui.go`, `task_status.go`, and mirrored by `internal/daemon` (host.go/persist.go) via their own meta type.

### internal/transcript/peek.go
- **Role:** Parse-free preview extraction — gets a session's working dir, derived title, and turn count without reading the whole transcript; powers the session/projects browser.
- **Key symbols:**
  - `type Preview struct{Cwd, Title string; Messages int}` — the cheap metadata.
  - `Peek(src Source, origin string) Preview` — dispatches to a per-source peeker; OpenCode (session-id origin) has no cheap peek and returns an empty `Preview`.
  - `peekClaude`/`peekCodex`/`peekPi`/`peekHermes`/`peekEigen` (unexported) — per-format head-scan that pulls cwd + first-user-message title; `peekEigen` prefers the meta sidecar's Dir/Title; `peekHermes` has no cwd (chat-platform origin).
  - `scanPeek(path, countTurn) (lines []string, turns int)` (unexported) — single pass with **two bounded jobs**: keeps head lines up to `peekMaxBytes` (256KB) for title/cwd, and counts conversational turns via `countTurn` up to `peekCountMaxBytes` (8MB). Files under the count cap are counted exactly; past it scanning stops and the count is **extrapolated** by observed turn density (turns/byte × total size), so a peek never per-line unmarshals an entire 100MB+ transcript.
  - `titleFrom(string)` (unexported) — derives a concise title, rejecting injected context (valid-JSON blobs, complete XML/instruction tags via `isMarkupTag`, AGENTS.md, `<user_instructions>`/`<environment`, interrupt/caveat notices) while still titling real asks that merely open with `{`/`[`/`<`; collapses whitespace, truncates to 72 runes.
  - `isMarkupTag(string)` (unexported) — narrow test for whether a string opens with a complete `<name …>`/`<name/>`/`</name>` tag on one line (so prose like `< 10ms` or `<3` still yields a title).
  - `claudeText`/`codexText` (unexported) — extract first user text from content blocks (string or block list).
  - `claudeDirFromPath`/`resolveClaudeDir`/`resolveSegs`/`dirExists` (unexported) + var `claudeSep` — recover the cwd from Claude's lossy `-`-encoded project-folder name by **DFS over the real filesystem**: every `/`, `.`, `_`, and literal `-` was flattened to one `-`, so each `-` is ambiguous (separator vs. in-name char). `resolveSegs` descends, trying the shortest child first and fusing segments with `claudeSep` only when that directory actually exists; returns `""` when no reconstruction reaches an existing dir (an empty Cwd beats a phantom project). The caller still prefers the real cwd recorded in the transcript body.
  - `claudeTurn`/`codexTurn`/`piTurn`/`hermesTurn`/`eigenTurn` (unexported) — mechanical per-source line classifiers: "is this one user/assistant turn?" (passed as `countTurn` into `scanPeek`).
  - consts `peekMaxBytes = 256 << 10` (head window) and `peekCountMaxBytes = 8 << 20` (turn-count budget).
- **Depends on:** `internal/llm` (eigen line is a marshaled `llm.Message`).
- **Used by / entrypoint:** `Peek` ← `internal/session/session.go` (session indexing); `claudeText` is shared by `peekClaude` and `peekPi`; `LoadMeta` (meta.go) is called from `peekEigen`.

## Cross-links
- **`internal/llm`** — the canonical `llm.Message`/`llm.ToolCall`/`llm.Role` target type every parser produces; the only first-party dependency of this slice.
- **`internal/session`** — the heaviest consumer: builds its session index using `Source`, `sourceGlobs`, `Peek`, `ListOpenCodeSessions`, `ImportFrom`/`ImportOpenCode`, and `Load`/`Save`; also has its own first-user-text extractor (`internal/session/title.go`) that switches on `transcript.Source` per format.
- **`internal/tui`** — saves transcripts/meta on every turn (`Save`/`SaveMeta` in tui.go, commands.go), reads cwd from meta in the header (`LoadMeta`), and imports foreign transcripts via `Import` in slash-command handling.
- **`internal/daemon`** (host.go, persist.go) — persists/restores daemon-hosted session transcripts with `Save`/`Load`; uses `SaveForce` + `ClearBackups` to clear a session on removal; keeps its own parallel meta sidecar struct but mirrors the same `SessionMeta` fields (Dir/Model/Title/Goal/Perm).
- **`internal/gui`** (sessions_extra.go) — copies a session by `Load`+`Save` of its transcript.
- **`internal/app`** (sessions.go) — copies/loads persisted transcripts via `Load`/`Save`.
- **`main.go` (CLI root)** — `--resume`/`--continue`/`--from` flow: `Detect` → `Load`/`ImportFrom`/`ImportOpenCode`, then `LoadMeta` to restore provider/model/perm/effort/search/goal/loop config; saves with `Save`/`SaveMeta`.
- **`agent_sessions.go`** (repo root) — cross-agent session discovery: glob-maps `transcript.Source` to on-disk layouts and lists OpenCode sessions via `ListOpenCodeSessions`.
- **`task_status.go` / `daemon.go`** (repo root) — background-task and daemon transcript save/load (`Save`/`SaveMeta`/`Load`).
- **`modernc.org/sqlite`** — external pure-Go SQLite driver, blank-imported only for the OpenCode reader.

## Dead-code review
No high-confidence dead code found. All exported symbols (`Import`, `ImportFrom`, `Detect`, `Save`, `SaveForce`, `Load`, `ClearBackups`, `Peek`, `Preview`, `Source` + all six `Source*` consts, `SessionMeta` + all its fields, `SaveMeta`, `LoadMeta`, `ImportOpenCode`, `ListOpenCodeSessions`, `OpenCodeSession`) have verified callers outside the package (grepped repo-wide; `SaveForce`/`ClearBackups` are used by `internal/daemon/host.go`). All unexported helpers, turn classifiers, and the consts (`peekMaxBytes`, `peekCountMaxBytes`, `transcriptBackupGenerations`) are referenced within the package.

One low-confidence note: in `role()` (claude.go), the `case "tool", "toolResult"` and the `default` passthrough branches are effectively unreachable from its single callsite — `role` is only ever called with `rec.Message.Role` on a record already gated to `type == "user" || "assistant"`. The branches are harmless defensive mapping, not removable dead code in the strict sense (a complete role-mapper), so they are not flagged as dead.
