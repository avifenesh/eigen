# lsp/ — language server integration

> A minimal Language Server Protocol (LSP) client that lets eigen surface a real language server's
> navigation features as agent tools. It speaks JSON-RPC 2.0 over Content-Length-framed stdio (the
> LSP wire format, distinct from MCP's newline-delimited transport), performs the `initialize`
> handshake, opens documents (`didOpen`), and issues `definition`, `references`, `hover`, and
> `documentSymbol` requests, plus it captures `publishDiagnostics` the server pushes. A `Manager`
> maps each file to its configured language server by extension, starts servers lazily on first use,
> and caches the live client for the session. The package's only external entrypoint is
> `LoadTools`, which reads an `lsp.json` config and returns five read-only tools
> (`lsp_definition`, `lsp_references`, `lsp_hover`, `lsp_symbols`, `lsp_diagnostics`) wired to that
> manager; the CLI (`main.go`) and the GUI/build path (`build.go`) both call it and register the
> tools (marked `Niche`, disclosed via `search_tools`).

## Files

### internal/lsp/client.go
- **Role:** The JSON-RPC 2.0 transport and connection state — frame read/write loop, request/response correlation, and diagnostics cache.
- **Key symbols:**
  - `Client` (type) — a connected LSP session: holds the framed writer (with a write mutex), pending-request channels keyed by id, and the latest diagnostics per URI.
  - `rpcMessage` / `rpcError` (types) — the JSON-RPC envelope and error; `rpcError.Error()` formats `lsp error <code>: <msg>`.
  - `newClient(w, r, closeFn)` — constructs a `Client` and launches the background `readLoop` over the reader.
  - `readLoop(r)` — reads framed messages, routes responses (id + result/error, no method) to waiters, and feeds `textDocument/publishDiagnostics` notifications to `handleDiagnostics`; on stream close it fails all in-flight calls.
  - `readFrame(br)` — reads one LSP message (headers to blank line, then a Content-Length body); errors on missing/zero length.
  - `writeFrame(v)` — marshals and writes a value with the `Content-Length` header, serialized by `wm`.
  - `call(ctx, method, params, result)` — sends a request, allocates an id + response channel, and blocks until the response, ctx cancel, or connection close.
  - `notify(method, params)` — fire-and-forget notification.
  - `marshalParams(params)` — nil-safe JSON marshal of params.
  - `handleDiagnostics(raw)` — decodes `PublishDiagnosticsParams` and stores diagnostics under their URI.
  - `Diagnostics(uri)` — returns a defensive copy of the latest diagnostics for a URI.
  - `Close()` — invokes the stored `closeFn` (which kills the server process).
- **Depends on:** stdlib only (`bufio`, `encoding/json`, `io`, `sync`, `strconv`, `strings`).
- **Used by / entrypoint:** `newClient` is called from `protocol.go`'s `Connect`; `Diagnostics`/`Close`/`call`/`notify` are used by `manager.go`, `protocol.go`, and `tools.go`. Reached transitively from `LoadTools`.

### internal/lsp/protocol.go
- **Role:** The LSP protocol type definitions (the subset eigen uses) plus the typed request methods on `*Client` (`Connect`, `initialize`, `DidOpen`, `Definition`, `References`, `Hover`, `DocumentSymbols`).
- **Key symbols:**
  - `Position`, `Range`, `Location`, `Diagnostic`, `PublishDiagnosticsParams`, `DocumentSymbol` (types) — the wire-format structs; `DocumentSymbol` accepts both hierarchical `DocumentSymbol[]` and flat `SymbolInformation[]` shapes.
  - `hoverResult` (type) — variant hover response wrapper whose `Contents` field uses the custom `hoverContents` unmarshaler.
  - `Connect(ctx, command, env, rootDir)` — spawns the language-server process, wires stdin/stdout into a `Client`, and runs the `initialize` handshake (closing on failure).
  - `initialize(ctx, rootDir)` — sends the `initialize` request (declaring definition/references/hover/documentSymbol/publishDiagnostics capabilities) then the `initialized` notification.
  - `DidOpen(uri, languageID, text)` — `textDocument/didOpen` so position requests resolve.
  - `Definition` / `References` — position requests, delegating to `locations`.
  - `locations(ctx, method, uri, pos, extra)` — issues a position request and normalizes the result via `decodeLocations`.
  - `Hover(ctx, uri, pos)` — returns flattened hover text ("" when none).
  - `DocumentSymbols(ctx, uri)` — returns the document's symbols via `decodeSymbols`.
- **Depends on:** stdlib only (`context`, `encoding/json`, `os/exec`, `path/filepath`).
- **Used by / entrypoint:** `Connect` is called by `manager.go`'s `clientFor`; the `*Client` request methods are called by the tool closures in `tools.go`.

### internal/lsp/decode.go
- **Role:** URI/path conversion plus tolerant decoders that flatten the several JSON shapes LSP servers may return into eigen's normalized types, and human-readable name mappers.
- **Key symbols:**
  - `PathToURI(path)` — absolute path to `file://` URI (Windows drive-letter aware).
  - `URIToPath(uri)` — `file://` URI back to a filesystem path; non-file URIs pass through unchanged.
  - `hoverContents` (type) + `UnmarshalJSON` — custom unmarshaler that flattens any hover-contents shape on decode.
  - `flattenMarkup(b)` — recursively turns `string | MarkedString | MarkupContent | array` hover JSON into joined plain text.
  - `decodeLocations(raw)` — normalizes `Location | []Location | LocationLink[] | null` into `[]Location`.
  - `decodeSymbols(raw)` — flattens hierarchical `DocumentSymbol[]` (and `SymbolInformation[]` via a `Location.Range` fallback) into a flat `[]DocumentSymbol`.
  - `SymbolKindName(kind)` — maps an LSP `SymbolKind` number to a readable name (e.g. 12 → "function").
  - `SeverityName(sev)` — maps a diagnostic severity to "error"/"warning"/"info"/"hint".
- **Depends on:** stdlib only (`encoding/json`, `net/url`, `path/filepath`, `runtime`, `strings`).
- **Used by / entrypoint:** `PathToURI`/`URIToPath` used across `manager.go`, `protocol.go`, `tools.go`; `decodeLocations`/`decodeSymbols`/`hoverContents`/`flattenMarkup` are internal decode helpers; `SymbolKindName`/`SeverityName` are used in `tools.go` output formatting.

### internal/lsp/manager.go
- **Role:** Maps files to language servers by extension, starts and caches servers lazily per session, tracks which documents have been opened, and remembers servers that failed to start.
- **Key symbols:**
  - `ServerConfig` (type) — one server entry: `name`, `command`, `extensions`, `env`, optional `language_id`, `disabled` (the JSON shape of `lsp.json` server entries).
  - `connectTimeout` (const, 20s) — bounds the initialize handshake / each request.
  - `Manager` (type) — owns `root`, the configs, and (under a mutex) `clients`/`opened`/`failed` maps.
  - `NewManager(root, configs)` — constructor.
  - `configFor(path)` — picks the `ServerConfig` whose extensions match the file.
  - `clientFor(ctx, path)` — returns a live initialized client + opened document URI, starting the server on first use and short-circuiting servers already marked failed.
  - `ensureOpen(client, cfg, path)` — sends `didOpen` once per URI per session (reading the file from disk), returning the URI.
  - `Close()` — shuts down every started server.
  - `serverEnv(extra)` — merges configured env vars onto `os.Environ()`.
  - `waitDiagnostics(ctx, c, uri)` — polls (75ms tick, 2s deadline) for asynchronously-published diagnostics after a `didOpen`.
- **Depends on:** stdlib only (`context`, `os`, `path/filepath`, `strings`, `sync`, `time`); calls `Connect`/`PathToURI`/`DidOpen`/`Diagnostics` within the package.
- **Used by / entrypoint:** `NewManager` is called by `LoadTools` (`tools.go`); `clientFor`/`waitDiagnostics` are used by the tool closures; `*Manager` is held as `deps.lspMgr` in `build.go` and as a deferred `Close()` in `main.go`.

### internal/lsp/tools.go
- **Role:** The public seam — loads `lsp.json` and exposes the manager's navigation features as eigen `tool.Definition`s (all `ReadOnly`, so they auto-run even in gated mode).
- **Key symbols:**
  - `maxResultBytes` (const, 16 KiB) — caps each tool's textual output so a huge references list can't blow the context budget.
  - `LoadTools(root, path)` — reads `lsp.json`, validates/filters server entries (skips `disabled`, rejects empty name/command/extensions), builds a `Manager`, and returns `[]tool.Definition` + the manager + non-fatal errors; a missing config yields no tools and no error.
  - `Tools(mgr)` — returns the five tool definitions backed by a manager.
  - `posArgs` (type) + `position()` — the `{path,line,character}` argument shape; converts 1-based human coords to LSP 0-based, clamping negatives to 0.
  - `posSchema` (const) — shared JSON schema for the position tools.
  - `definitionTool` / `referencesTool` / `hoverTool` / `symbolsTool` / `diagnosticsTool` — build the `lsp_definition`/`lsp_references`/`lsp_hover`/`lsp_symbols`/`lsp_diagnostics` definitions; each resolves a client via `mgr.clientFor`, issues the request, and formats output.
  - `parsePos(args)` — parses + validates position args (path required, line >= 1).
  - `formatLocations(locs)` — renders locations as compact project-relative `file:line:col` lines (truncated to `maxResultBytes`).
  - `displayPath(path)` — shortens an absolute path to cwd-relative for readable output.
- **Depends on:** internal `github.com/avifenesh/eigen/internal/tool` (`tool.Definition`, `tool.TruncateUTF8`); stdlib (`context`, `encoding/json`, `os`, `path/filepath`, `sort`, `strings`).
- **Used by / entrypoint:** `LoadTools` is the single external entrypoint — called from `main.go` (CLI tool registration) and `build.go` (`deps.lspMgr` wiring). Tools are registered as `Niche` and surfaced via `search_tools`.

## Cross-links
- **internal/tool** — every LSP feature is exported as a `tool.Definition`; output is capped with `tool.TruncateUTF8`. This is the only internal package dependency.
- **main.go (CLI root)** — calls `lsp.LoadTools(cwd, lspConfigPath())`, registers the tools, and defers `lspMgr.Close()` on exit. `lspConfigPath()` resolves project `.eigen/lsp.json` then per-user `~/.eigen/lsp.json`.
- **build.go (GUI / app build path)** — calls `lsp.LoadTools(p.Dir, ...)` and stores the `*lsp.Manager` as `deps.lspMgr`.
- **internal/app (extensions UI)** — `plugins.go`'s `loadLSPRows` parses `lsp.json` independently for the extensions panel; `toggle.go` references `lsp.json` as a toggleable extension config. Note: that inline parser reads a `languages` field, whereas `lsp.ServerConfig` uses `extensions` — a config-shape divergence between the two readers (outside this slice to reconcile).
- **Spawned language-server processes** — `Connect` launches the configured `command` as a child process over stdio; not a Go package, but the real external dependency at runtime.

## Dead-code notes (verified)
No high-confidence dead code found in this slice. Things that look unused but are not:
- `SymbolKindName` / `SeverityName` — exported but only consumed inside the package (in `tools.go`). Kept exported as a small public mapping API; not dead.
- `hoverResult.Range`, `DocumentSymbol.SelectionRange`, `Diagnostic.Code`, `LocationLink.TargetSelectionRange` — JSON-deserialization fields populated by the unmarshaler from the wire; part of the protocol contract even though Go code does not read them. Not dead.
- Doc nit (not code): the comment on `hoverResult` (protocol.go) references a `decodeHoverContents` function that does not exist — the actual flattening is done by `hoverContents.UnmarshalJSON` → `flattenMarkup`. Stale comment, no dead symbol.
