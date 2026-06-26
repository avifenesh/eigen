# telegram/, mcp/

> Two "external surface" packages of the Eigen Go agent. **`internal/mcp`** is a
> Model Context Protocol client over TWO transports — local **stdio** (spawned
> server subprocesses) and remote **Streamable HTTP** (the shape "connectors"
> like Google Workspace / Slack / Notion take). Both satisfy one `session`
> interface; the loader lists each server's tools and wraps them as native Eigen
> `tool.Definition`s (named `<server>_<tool>`) with progressive-disclosure
> grouping, allowlist filtering, schema slimming, lazy per-session connections,
> built-in auto-detection of Eigen's bundled workspace / computer-use /
> chrome-bridge helpers, and a typed `mcp.json` editor (config.go) the GUI uses.
> Remote-server auth is supplied by `internal/connector` via the
> `RemoteAuthProvider` hook (OAuth bearer that refreshes transparently).
> **`internal/telegram`** is a
> dependency-free Telegram Bot API client plus a `Bridge` that turns an authorized
> Telegram chat into a phone-side VIEW onto a live daemon session — long-polling
> updates, relaying messages as session input, streaming agent events back as an
> edited-in-place message, and rendering gated-tool approvals as inline ✅/❌
> buttons. MCP feeds the agent's tool layer (`build.go`, `main.go`); Telegram is a
> standalone foreground command (`eigen telegram`) that dials the daemon per chat.

## Files

### internal/mcp/session.go
- **Role:** The transport-agnostic `session` interface both MCP transports satisfy (stdio `*Client` + remote `*httpClient`), so the loader + `lazyClient` wire a local server and a remote connector identically.
- **Key symbols:** `session` — `Instructions()`/`ServerName()`/`ListTools`/`CallToolRich`/`alive()`/`Close()`.
- **Used by / entrypoint:** `lazyClient.dial` returns a `session`; `LoadTools` probes via it.

### internal/mcp/client.go
- **Role:** The low-level MCP **stdio** JSON-RPC client — process spawn, handshake, request/response plumbing, tool listing and invocation. Also holds the transport-shared decode/handshake helpers reused by the HTTP client.
- **Key symbols:**
  - `ToolSpec` — one MCP tool (name, description, raw `inputSchema`, optional annotations).
  - `ToolAnnotations` — optional MCP behavior hints; `ReadOnlyHint` lets safe tools auto-run in gated mode.
  - `Client` — a connected stdio MCP session: JSON encoder, pending-call map keyed by request id, `closeFn`, cached `instructions` / `serverName`. Satisfies `session`.
  - `Connect(ctx, command, env)` — spawns the server process in its OWN process group (`Setpgid`) so `Close` can SIGTERM/SIGKILL the whole tree (X server, browser, apps), then runs `initialize`.
  - `initializeParams()` / `initResult` — the shared initialize payload + response shape (used by BOTH transports).
  - `(*Client).initialize` — sends `initialize` with protocol version `2024-11-05`, caches server instructions + name, fires `notifications/initialized`.
  - `(*Client).readLoop` — scans newline-delimited responses (64 MiB buffer cap), routes by id to the waiting channel, fails in-flight calls + marks the connection dead on stream close / oversized line.
  - `(*Client).call` / `(*Client).notify` — send a request and await the id-matched response (ctx-cancellable) / fire-and-forget notification.
  - `(*Client).ListTools` — `tools/list` → `[]ToolSpec`.
  - `toolCallParams` / `rawToolResult` / `decodeToolResult(out, toolName, serverName)` — the shared tools/call request + wire result + decode (text + base64 image blocks, capped at `maxMCPImages`=4 / `maxMCPImageBytes`=4 MiB; `isError` → Go error). Used by BOTH transports.
  - `(*Client).CallToolRich` — `tools/call` then `decodeToolResult`.
  - `(*Client).CallTool` — thin text-only wrapper over `CallToolRich` (drops images).
  - `(*Client).Instructions` / `(*Client).ServerName` / `(*Client).alive` — `session` accessors.
  - `serverSuffix(name)` — " from server <name>" provenance for blank-message tool errors (free function, shared).
  - `ToolResult` — text + `[]llm.Image`, mirrors `tool.Result`.
- **Depends on:** `internal/llm` (for `llm.Image` in `ToolResult`).
- **Used by / entrypoint:** `load.go` (`Connect` via `lazyClient.dial`); `http.go` reuses `initializeParams`/`initResult`/`toolCallParams`/`rawToolResult`/`decodeToolResult`/`serverSuffix`.

### internal/mcp/http.go
- **Role:** The remote MCP transport — **Streamable HTTP** (POST JSON-RPC; response inline as `application/json` OR as a `text/event-stream` SSE). This is how connectors (remote MCP servers) connect.
- **Key symbols:**
  - `httpClient` — a remote session: target url, `http.Client`, an `authHeader func() string` re-read per request (so a refreshed token is picked up), static headers, server-assigned `Mcp-Session-Id`. Satisfies `session`.
  - `httpDialer` — dial inputs: `URL`, `AuthHeader` func, `HTTPHeaders`, optional `Client`.
  - `ConnectHTTP(ctx, dialer)` — opens the session + runs `initialize`.
  - `(*httpClient).rpc` / `.post` / `.notify` — JSON-RPC over POST with the dual `Accept`, session id, and auth headers; captures the session id on initialize.
  - `readJSONRPCResponse` / `readSSEResponse` / `matchResponse` — read the response whether inline JSON or SSE (skipping comments + non-matching-id frames; matches by request id).
  - `httpAuthError` (`WWWAuthenticate()`) — a 401 wrapped with the `WWW-Authenticate` challenge — the trigger + discovery hint for `internal/connector`'s OAuth flow.
- **Depends on:** shared helpers in client.go; stdlib `net/http`.
- **Used by / entrypoint:** `load.go` (`ConnectHTTP` via `newLazyHTTPClient`/`httpDialerFor`).

### internal/mcp/load.go
- **Role:** The config loader + tool adapter — reads `mcp.json`, probes servers for schemas, and turns each tool into a lazily-connected Eigen `tool.Definition`.
- **Key symbols:**
  - `serverConfig` — one `mcp.json` entry: `Name`, `Command`, `Env`, `Description`, `Tools` allowlist, `ExcludeTools`, `Disabled`, plus remote fields `URL`, `Type` (`http`/`sse`/`streamable-http`), `Headers` (`http_headers`), `BearerTokenEnv` (`bearer_token_env_var`, static-creds path).
  - `mcpConfig` — `{ servers: [...] }`.
  - `Handle` — interface (`Close() error`) for a per-session MCP resource returned to callers.
  - `LoadTools(ctx, path)` — reads `mcp.json` (missing file is fine), applies `withBuiltinServers`, probes each enabled server once for schemas (stdio OR remote — same probe→list→wrap path), derives a Level-0 group "gist", returns `[]tool.Definition` + `[]Handle` (lazy clients) + non-fatal `errs`.
  - `lazyClient` — owns a server started only on first tool invocation; the transport is hidden behind a `dial func(ctx) (session, error)`; `get` dials once under a mutex (with a fail-fast cooldown), `CallToolRich` proxies, `Close` idempotent, `started()` a test probe.
  - `newLazyClient` (stdio: spawns command/env) / `newLazyHTTPClient` (remote: opens the HTTP session).
  - `isRemoteServer(sc)` — a remote entry = a url/type and NO command (command wins when both present).
  - `RemoteAuthProvider` (var) — the hook `internal/connector` sets: given (name, url) it returns the OAuth bearer auth-header func; nil/absent → `httpDialerFor` falls back to the static `bearer_token_env_var`, else no auth.
  - `httpDialerFor(sc)` — builds the `httpDialer` for a remote entry (OAuth header → static bearer env → none; static headers always).
  - `wrapCaller(client, server, gist, sp)` — the core adapter: slims the schema, builds the `<server>_<tool>` name, honors `readOnlyHint`, flags screenshot/observe tools for path→image attachment, assigns capability + niche/group metadata, and sets `RunRich`.
  - `wrap` / `wrapLazy` — thin `wrapCaller` shims for a `*Client` / `*lazyClient` (only `wrapLazy` is used in production).
  - `toolCaller` — interface (`CallToolRich`) letting `wrapCaller` accept either client type.
  - `toolAllowed(sc, name)` — applies allowlist then excludelist with exact / `*`-prefix matching.
  - `toolCapability(server, tool, desc)` — maps known workspace/computer_use/chrome tool names to a capability bucket + description (for grouping in the UI/model view).
  - `slimSchema` / `stripSchemaNoise` — recursively drop `$schema` and `title` keys to cut per-request token cost.
  - `serverEnv` / `expandCommand` / `envLookup` — merge configured env onto `os.Environ`, and expand `${VAR}`/`$VAR` in the command (how plugin MCP servers resolve `EIGEN_PLUGIN_ROOT`).
  - `sanitize` — coerce names to a provider-safe `[A-Za-z0-9_-]` set.
  - `firstSentence` — first line/sentence of a server's instructions (truncated to 120) for the gist fallback.
- **Depends on:** `internal/tool` (`tool.Definition`, `tool.Result`); same-package `Connect`/`ConnectHTTP`/`session`/`withBuiltinServers`.
- **Used by / entrypoint:** `LoadTools` called from `build.go` and `main.go` (the agent's tool-layer assembly). Returned `Handle`s stored as `mcpClients` and closed at session end.

### internal/mcp/config.go
- **Role:** The typed, schema-aware `mcp.json` editor (stdio AND remote) the GUI/CLI use to add/edit/enable/remove servers without hand-editing JSON. The plugin layer manipulates the same file as raw maps for bundle wiring; this is the user-facing path.
- **Key symbols:**
  - `ServerEntry` — the public view of a server (with a `Remote` flag), `toEntry`/`toConfig` converters.
  - `UserConfigPath()` — `~/.eigen/mcp.json` (the GUI/connector edit target; project-local `.eigen/mcp.json` is a CLI-cwd concern, not editable from the desktop).
  - `readConfig`/`writeConfig` — load (missing → empty) / atomic write (0600, temp+rename).
  - `ListServers(path)` — every configured server (NOT auto-built-ins), name-sorted.
  - `validateEntry` — must have a name and EITHER a remote url OR a stdio command (not both/neither).
  - `SaveServer` (add/replace by name) / `RemoveServer` / `SetServerDisabled`.
- **Depends on:** stdlib only (same package's `serverConfig`/`isRemoteServer`).
- **Used by / entrypoint:** `internal/gui` (`connectors.go`, `wiring.go`).

### internal/mcp/builtin.go
- **Role:** Auto-detection + registration of Eigen's bundled MCP helpers (workspace sandbox, computer-use, Chrome bridge) so they appear as first-class tools without hand-editing `mcp.json`.
- **Key symbols:**
  - `workspaceTools` / `computerUseTools` / `chromeBridgeTools` — curated tool allowlists per server (token-cost control; e.g. 27 of the workspace server's ~82 tools).
  - `withBuiltinServers(user)` — appends auto-detected built-ins to the user's server list, skipping any name the user already configured (user wins); also backfills canonical descriptions for known builtin names.
  - `ChromeBridge()` — locates the bundled `mcp-server.js` and a node runtime; returns `("","")` if either is absent.
  - `chromeBridgeScript` — resolves the bridge script via `EIGEN_CHROME_BRIDGE` override or `~/.eigen/chrome-bridge/bin/mcp-server.js`.
  - `findNode` — resolves node via `EIGEN_NODE_BIN`, PATH, or common nvm/local locations (tolerates the daemon's minimal PATH).
  - `ComputerUseBinary()` / `WorkspaceBinary()` — locate the bundled helper binaries via env override or `~/.local/bin` (PATH binaries ignored unless opted in).
  - `isExecutable` / `isExecutableOrFile` — stat-based existence/permission checks.
- **Depends on:** stdlib only (`os`, `os/exec`, `path/filepath`, `strings`).
- **Used by / entrypoint:** `withBuiltinServers` called by `LoadTools` (load.go:76). `WorkspaceBinary`/`ComputerUseBinary`/`ChromeBridge` also called from `daemon.go:771`, `main.go` (helper/doctor reporting at ~1846, 1936, 1958, 1979).

### internal/mcp/screenshot.go
- **Role:** Upgrades a tool result that REFERENCES a screenshot file path into one that carries the decoded image, so the model can actually see sandbox/observe screenshots.
- **Key symbols:**
  - `attachScreenshotPath(res)` — no-op unless the text result is JSON with a screenshot path and no inline image; reads the file and appends it as an `llm.Image`.
  - `screenshotPathFromJSON(text)` — extracts a `path` from `{"screenshot":{"path":…}}` or top-level `{"path":…}`, only for known image extensions.
  - `isImagePath` — extension check (png/jpg/jpeg/webp/gif).
  - `readImageFile(path)` — reads an on-disk image (capped at `screenshotImageBytes`=8 MiB), returns bytes + media type.
- **Depends on:** `internal/llm` (`llm.Image`).
- **Used by / entrypoint:** `attachScreenshotPath` called from `wrapCaller`'s `RunRich` (load.go:280) when the tool name contains `screenshot`/`observe`.

## internal/connector — OAuth 2.1 for remote MCP connectors

> Implements OAuth 2.1 (auth-code + PKCE) for remote MCP servers ("connectors":
> Google Workspace, Slack, Notion, Linear, …). On a 401 it discovers the
> authorization server (RFC 9728 protected-resource → RFC 8414 / OpenID
> auth-server metadata), dynamically registers a public client (RFC 7591), runs
> the PKCE flow against a loopback redirect, and persists the token — in the OS
> keychain when available — so the MCP transport attaches a bearer that refreshes
> transparently. The OAuth mechanics lean on `golang.org/x/oauth2`; the
> MCP-specific discovery + registration are hand-rolled.

### internal/connector/manager.go
- **Role:** The centerpiece — owns the token store, runs the interactive `Connect` flow, and supplies the auth-header func wired into `mcp.RemoteAuthProvider`.
- **Key symbols:**
  - `Manager` — store + `http.Client` + browser opener + per-connector cache and live `oauth2.TokenSource`s.
  - `NewManager(path)` (keychain-or-file store) / `NewManagerWithStore` (explicit store, tests).
  - `AuthHeaderFunc(name, url) (func() string, ok)` — the `mcp.RemoteAuthProvider` impl: a func that yields the current `Bearer <token>`, refreshing via a `persistingSource` that writes rotated tokens back; `ok=false` → loader falls back to its static path.
  - `Connect(ctx, name, url, hint)` — discover → `clientFor` (reuse or RFC 7591 register) → open browser for PKCE authorize → exchange code → persist token. Validates `state` (CSRF), sends the RFC 8707 `resource` indicator.
  - `Disconnect` / `Statuses` / `IsConnected`; `chooseScopes`.
- **Depends on:** `golang.org/x/oauth2`; same-package discovery/register/store/callback/browser.
- **Used by / entrypoint:** `wire.go:Default`/`Install`; `internal/gui/connectors.go`.

### internal/connector/discovery.go
- **Role:** RFC 9728 + RFC 8414 / OpenID metadata discovery.
- **Key symbols:** `parseWWWAuthenticate` (pull `resource_metadata` from the 401 challenge), `splitAuthParams`, `discover` (resource → auth-server), `fetchProtectedResource`/`fetchAuthServer` (well-known paths, derived from origin when no hint), `authServerMeta`/`protectedResourceMeta`, `fetchJSON`, `originOf`.

### internal/connector/register.go
- **Role:** RFC 7591 dynamic client registration (public client: `token_endpoint_auth_method=none`, PKCE-secured). `registerClient`, `clientRegistration`, `joinScopes`.

### internal/connector/store.go
- **Role:** Persistence behind the `secretStore` interface. `record` (endpoints + client creds + `*oauth2.Token`), `Status`, `fileStore` (JSON, 0600, atomic), `sortedStatuses`. The file store is the default + the no-keyring fallback.

### internal/connector/keychain.go
- **Role:** `secretStore` impl that keeps the SECRET half (the OAuth token) in the OS keychain (`go-keyring`: macOS Keychain / libsecret / Windows Credential Manager) and the non-secret metadata in the JSON file. `newSecretStore(path)` returns this when `keychainAvailable()`, else the plain `fileStore`.
- **Key symbols:** `keychainStore`, `keychainAvailable` (probe round-trip), `newSecretStore`, `load`/`save` (token ↔ keychain, metadata ↔ file; deletes stale keychain entries).

### internal/connector/callback.go
- **Role:** One-shot localhost HTTP server catching the OAuth redirect. `loopbackServer` (ephemeral 127.0.0.1 port), `newLoopbackServer`/`redirectURI`/`wait`/`close`, `callbackResult`, `callbackHTML` (the "you can close this tab" page).

### internal/connector/browser.go
- **Role:** Cross-platform default-browser opener (`open`/`xdg-open`/`rundll32`). `openBrowser`.

### internal/connector/wire.go
- **Role:** Process-wide wiring. `DefaultPath()` (`~/.eigen/connectors.json`), `Default()` (shared Manager), `Install()` — sets `mcp.RemoteAuthProvider = Default().AuthHeaderFunc`; called once at startup (`main.go`) before `LoadTools`.
- **Depends on:** `internal/mcp` (the hook).

### internal/telegram/telegram.go
- **Role:** The dependency-free Telegram Bot API client — long-poll `getUpdates`, send/edit HTML messages, inline keyboards, message splitting/escaping. No inbound listener (outbound HTTPS only).
- **Key symbols:**
  - `Bot` — a bot bound to one token; 70 s HTTP timeout (> long-poll); `api` override for tests.
  - `New(token)` — constructs a `Bot`.
  - `Update` / `Message` / `User` / `Chat` / `CallbackQuery` — the subset of Telegram update types the bridge uses.
  - `(*Bot).call` — POST a method with form params, unwrap the `{ok, description, result}` envelope.
  - `(*Bot).SetCommands` — register the "/" autocomplete menu (`setMyCommands`).
  - `(*Bot).GetMe` — verify token, return bot username.
  - `(*Bot).GetUpdates(offset, timeout)` — long-poll, restricted to `message`/`callback_query`.
  - `(*Bot).Send` — send HTML message (auto plain-text retry on parse error); returns new message id.
  - `(*Bot).SendChatAction` — show the "typing…" indicator.
  - `(*Bot).Edit` — edit a message in place (swallows "not modified", retries plain on parse error) — the streaming-reply mechanism.
  - `(*Bot).AnswerCallback` — acknowledge an inline-button tap.
  - `(*Bot).SendLong` — split over-limit text across multiple messages (buttons on the final chunk only).
  - `InlineKeyboard` / `Button` / `Buttons(pairs...)` — inline keyboard model + single-row builder.
  - `clampMsg` / `truncate` / `splitForTelegram` — enforce the 4000-char clamp / split on blank-line→newline→hard boundaries.
  - `escapeHTML` / `stripHTML` — escape reserved HTML chars / strip tags for the plain-text fallback.
  - `isNotModified` / `isParseError` / `isConflict` — error-string classifiers (benign edit, bad entities, 409 second-poller).
- **Depends on:** stdlib only (`net/http`, `net/url`, `encoding/json`, …).
- **Used by / entrypoint:** `telegram.New` called from `main.go:2196` (the `eigen telegram` command). `Bot` methods called throughout `bridge*.go`.

### internal/telegram/bridge.go
- **Role:** The `Bridge` core — wiring, the long-poll loop, authorization (fail-closed), and inbound-message routing.
- **Key symbols:**
  - `Bridge` — holds the `Bot`, a `dial` func for fresh `daemon.Client`s, the chat-id `allow` set, and per-chat `chatState` map + poll `offset`.
  - `chatState` — per-chat attached session: `daemon.Client`, `sessionID`, in-place streaming message id + buffer + `lastEdit` throttle clock, `pendingApproval` id.
  - `NewBridge(bot, dial, allow)` — builds a bridge, materializing the allowlist into a set.
  - `(*Bridge).Run(ctx)` — verifies the bot, registers the command menu, then long-polls `GetUpdates` until ctx cancel; backs off 30 s on 409 conflict (another poller), recovers per-update panics.
  - `(*Bridge).handle` — dispatches an update to `onCallback` or `onMessage` (panic-recovered).
  - `(*Bridge).authorized` — chat-id allowlist check.
  - `(*Bridge).onMessage` — for an authorized chat: route `/`-commands to `onCommand`, else relay text to the attached session via `SteerInput` (steer-or-fresh-turn) after resetting the stream.
  - `(*Bridge).chat` / `(*Bridge).resetStream` — locked chatState lookup / prepare a fresh streaming message.
- **Depends on:** `internal/daemon` (`daemon.Client`).
- **Used by / entrypoint:** `NewBridge`/`Run` called from `main.go:2197-2203`.

### internal/telegram/bridge_commands.go
- **Role:** The `/slash`-command implementations that operate on the attached session (status, model, perm, goal, compact, clear, resend).
- **Key symbols:**
  - `(*Bridge).requireSession` — returns the attached session or sends a hint + false.
  - `(*Bridge).status` — render live session state (title, id, model, perm, running, context %) with refresh/stop/compact buttons.
  - `(*Bridge).modelCmd` / `(*Bridge).applyModel` — inline model picker from `llm.Models()` / apply a `SetModel`.
  - `(*Bridge).permCmd` — set/toggle permission posture (`gated`/`auto`) via `SetPerm`.
  - `(*Bridge).goalCmd` — set/clear/show the session goal via `SetGoal`/`State`.
  - `(*Bridge).compactCmd` / `(*Bridge).clearCmd` / `(*Bridge).resendCmd` — `Compact` / `Clear` / `Resend` on the session.
  - `errText` / `nz` — small render helpers (error string, non-zero default).
- **Depends on:** `internal/daemon` (via `chatState.client`), `internal/llm` (`llm.Models`).
- **Used by / entrypoint:** All methods dispatched from `onCommand` (bridge_handlers.go) or inline-button callbacks in `onCallback`.

### internal/telegram/bridge_handlers.go
- **Role:** Command dispatch, session lifecycle (list/attach/new/interrupt), live event rendering + streaming, and inline-button callback handling.
- **Key symbols:**
  - `(*Bridge).onCommand` — the `/`-command router; `helpText` and `commandMenu` (the registered "/" autocomplete) are kept in sync here.
  - `(*Bridge).connFor` — dial + cache a per-chat `daemon.Client` on first use.
  - `(*Bridge).listSessions` — list daemon sessions, active-first then by recency, with tap-to-attach buttons + glyphs.
  - `(*Bridge).attach` — follow a session: show headline + recent history via `State`, set `sessionID`, then spawn an `Attach` goroutine that skips replay and forwards live events to `onEvent`.
  - `(*Bridge).newSession` — `NewSession(dir)` then attach.
  - `(*Bridge).interrupt` — `Interrupt` the running turn.
  - `(*Bridge).onEvent` — render a live `daemon.WireEvent`: stream `text`, typing on `reasoning`, terse `tool_start`/`tool_result`, ✅/❌ buttons on `approval` (stores `pendingApproval`), final flush on `done`, italic `note`.
  - `(*Bridge).streamText` / `(*Bridge).flushStream` — accumulate + edit the streaming message (1200 ms throttle, tail-trim past one message) / write the authoritative final answer (split long ones via `SendLong`).
  - `(*Bridge).onCallback` — inline-button taps: approve/deny (`Approve`), model/perm pickers, `act:status|stop|compact`, `attach:`.
  - `sessionHeadline` / `recentHistory` / `compactArgs` / `trunc` — render helpers.
  - `sessionActive` / `statusGlyph` — map `daemon.Status` to sort priority / glyph.
  - `commandMenu` — `[][2]string` autocomplete list registered via `SetCommands`.
- **Depends on:** `internal/daemon` (`daemon.Client`, `WireEvent`, `SessionState`, `Status`, `StatusWorking`/`StatusApproval`/`StatusError`), `internal/llm` (`llm.Message`, roles).
- **Used by / entrypoint:** `onCommand`/`onCallback` reached from `Bridge.handle`; `onEvent` reached from the `Attach` goroutine in `attach`.

## Cross-links
- **internal/daemon** — the Telegram bridge is a pure client of the daemon: dials `daemon.Client`, calls `List/Attach/SteerInput/Interrupt/Approve/State/SetPerm/SetGoal/Compact/Clear/Resend/SetModel/NewSession`, and renders `WireEvent`/`SessionState`/`Status`. `mcp.WorkspaceBinary` is also called from `daemon.go`.
- **internal/tool** — `mcp.LoadTools` produces `tool.Definition`s (with `RunRich`→`tool.Result`); this is the seam by which MCP tools enter the agent's tool registry.
- **internal/llm** — `mcp.ToolResult` carries `llm.Image` (decoded screenshots); the bridge renders `llm.Message`/roles and lists `llm.Models()`.
- **internal/harness** — sibling to `mcp/builtin.go`: `harness/chrome.go` installs the Chrome bridge connector that `mcp.ChromeBridge` later detects (parallel ChromeBridge* helpers there).
- **main.go / build.go** — entrypoints: `build.go`/`main.go` call `mcp.LoadTools` to assemble the agent's tools; `main.go` (`eigen telegram` command, ~line 2150-2206) constructs and runs the `Bridge`, holding a global flock so only one poller wins, and reports helper availability via `WorkspaceBinary`/`ComputerUseBinary`/`ChromeBridge`.
