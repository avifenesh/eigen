# LLM provider backends & transport

> This area is the set of concrete model backends behind eigen's `llm.Provider`
> contract, plus the shared HTTP/auth plumbing they ride on. Each provider
> translates eigen's neutral transcript (`Request`/`Message`/`ToolSpec` from
> `llm.go`) into one vendor's wire format, sends it over the shared retrying HTTP
> transport (`http.go`, signing via `sigv4.go` for AWS), and maps the reply back
> into a neutral `Response`. `llm.New` (in `provider.go`) is the factory that picks
> the right backend from a `(provider, model)` pair or a `tag:model` ref, so the
> agent loop, daemon, router, and GUI never see provider-specific shapes. Every
> built-in backend (and all three custom adapters) implements the optional
> `Streamer` capability: `Stream(ctx, req, sink)` forwards text/reasoning deltas
> to a `StreamSink` while still returning the assembled `Response`, which is what
> keeps the GUI/TUI live mid-turn; `Complete` is the non-streaming fallback.
> Wire dialects cluster into three families: Anthropic Messages (native +
> Bedrock Converse), OpenAI Responses (Bedrock mantle + Codex/ChatGPT), and
> OpenAI chat-completions (llama/grok/glm + custom), with `vendor.go` providing
> the cross-vendor classification used for review/judging. Reasoning continuity
> across tool-using turns is carried in the neutral transcript
> (`Message.Reasoning`/`ReasoningID`/`ReasoningEncrypted`).

## Files

### internal/llm/llm.go
- **Role:** The provider contract itself — the neutral transcript types every backend maps to/from, plus the optional capability interfaces. No wire code; this is the package's public type surface.
- **Key symbols:**
  - `Provider` (interface: `Name`/`ModelID`/`Complete`) — the minimal backend surface.
  - `Streamer` (interface: `Stream(ctx, req, sink) (*Response, error)`) — optional streaming capability; the agent uses it whenever a chunk sink is set.
  - `EffortSetter` (`SetEffort`/`Effort`), `Searcher` (`SetSearch`/`SearchMode`), `FastModer` (`SetFast`/`FastMode`) — optional runtime knobs.
  - `Role`/`RoleSystem`/`RoleUser`/`RoleAssistant`/`RoleTool`, `Message` (with `Reasoning`/`ReasoningID`/`ReasoningEncrypted`/`Images`/`ToolCalls`/`ToolError`), `ToolSpec`, `ToolCall`, `Image`, `Request`.
  - `Response` (`Text`/`Reasoning`/`ReasoningID`/`ReasoningEncrypted`/`ToolCalls`/`Usage`), `Usage` (`InputTokens`/`OutputTokens`/`CacheReadTokens`/`CacheWriteTokens`) + `Usage.CacheHitRate()`.
  - `ChunkKind` (`ChunkText`/`ChunkReasoning`), `StreamChunk`, `StreamSink`.
  - `EffortLevels` (global fallback set), `ValidEffort`, `ModelEffortLevels(modelID)`, `effortSupported`.
- **Depends on:** `catalog.go` (`Lookup`) via `ModelEffortLevels`.
- **Used by / entrypoint:** every file in this slice plus `internal/agent`, `internal/chat`, `internal/gui`, `router.go`, `review.go`, `council.go`, `compact_summary.go`.

### internal/llm/provider.go
- **Role:** The provider factory: resolves a `(provider, model)` pair (or a `tag:model` ref) to a concrete backend constructor. This is the single entrypoint into the whole slice.
- **Key symbols:**
  - `New(provider, model string) (Provider, error)` — strips any stray `Name()` suffix, parses an explicit `tag:` ref (which force-selects the backend), else `ResolveProvider`s from the catalog, canonicalizes aliases, then dispatches to `NewMantle`/`NewLlama`/`NewConverse`/`NewAnthropic`/`NewCodex`/`NewGrok`/`NewGLM` or `newCustomProvider`.
- **Depends on:** `ParseRef`, `ResolveProvider`, `canonicalProvider` (sibling files `ref.go`/`provider`-resolution in package `llm`).
- **Used by / entrypoint:** **entrypoint** for the whole area. Called from `build.go`, `router.go`, `daemon.go`, `main.go`, `main_gui_wails.go`, `plugincmd.go`.

### internal/llm/http.go
- **Role:** Shared HTTP transport: JSON POST and SSE-stream POST with jittered exponential backoff, Retry-After handling, and a response-size cap. Every provider in this slice sends through here.
- **Key symbols:**
  - `Version` (const `"0.1.0"`) — eigen's base semantic version, sent as the User-Agent (`eigen/<Version>`) and used as the headline everywhere; `FullVersion()` (version.go) annotates it with the git rev.
  - `maxAttempts` (5) / `maxResponseBytes` (16 MiB) — retry bound + body-size cap (also reused by the stream assemblers' scanner buffers and the converse event-stream frame cap).
  - `httpJSON(ctx, client, url, headers, body, sign)` — POSTs JSON, retries network/429/5xx, returns `(body, status, err)`; reads one byte past the cap to detect oversize; `sign` is re-invoked per attempt (SigV4 freshness).
  - `httpStream(ctx, client, url, headers, body, sign)` — POSTs and returns the open `*http.Response` for streaming on a 2xx; retries only the initial connect; non-2xx returned as an error string.
  - `sleepBackoff(ctx, attempt, retryAfter)` — exponential delay (honoring Retry-After) + jitter, cancellable.
  - `parseRetryAfter(h)` — parses Retry-After as delta-seconds or HTTP-date.
- **Depends on:** stdlib only.
- **Used by / entrypoint:** `httpJSON` — anthropic, converse, mantle, custom (responses/anthropic), `chatClient`, imagegen. `httpStream` — anthropic, converse, codex, mantle, custom (responses/anthropic), `chatClient`. `sleepBackoff` reused for retry pacing in codex/mantle stream loops.

### internal/llm/fallback.go
- **Role:** A `Provider` decorator that tries a PRIMARY model, then a FALLBACK — and FREEZES the primary for the rest of the local day when it fails on quota/billing, so a drained account isn't re-hit on every call. Built for the proactive-feed suggester (glm-5.2 primary → small-model fallback) where a GLM "insufficient balance" 429 would otherwise kill ideas every scan.
- **Key symbols:**
  - `IsQuotaError(err)` — true for HTTP 429 / "insufficient balance" / "no resource package" / z.ai code 1113 / quota / billing / out-of-credit wording (the raw provider body rides in the error string from http.go's `HTTP <code>: <body>`). Distinct from a transient 5xx/network blip.
  - `fallbackProvider` (type) — `primary`/`fallback` providers + a mutex-guarded `frozenUntil`; `frozen()`/`freezeForToday()` (parks the primary until the next local midnight). `Complete` tries the primary unless frozen; on `IsQuotaError` it freezes for the day, and ANY primary error routes to the fallback (except a dead ctx, which short-circuits). `Name`/`ModelID` report the PRIMARY (the headline model; the fallback is invisible).
  - `NewFallback(primary, fallback)` — wraps the two; a nil side collapses to the other, nil/nil → nil.
- **Depends on:** stdlib only.
- **Used by / entrypoint:** `main_gui_wails.go:guiSuggestProvider` and `internal/app/data.go:suggestProvider` wrap glm-5.2 over the small model. Tested in `fallback_test.go` (quota detection, healthy-primary, route+freeze, non-quota-no-freeze).

### internal/llm/version.go
- **Role:** Build-stamped version string, computed once from `debug.BuildInfo` so daemon, CLI, GUI, and TUI all report the same identity for a given binary.
- **Key symbols:**
  - `FullVersion()` — base `Version` annotated with the short git revision and a `-dirty` marker (e.g. `0.1.0+7c6737f-dirty`); falls back to bare `Version` when no VCS stamp is embedded. Cached behind a `sync.Once`.
- **Depends on:** `http.go` (`Version`), stdlib.
- **Used by / entrypoint:** `main.go` (`--version`), `internal/gui/bridge.go` (`Version()` bridge method), `internal/daemon/host.go` (build identity in the host info DTO).

### internal/llm/sigv4.go
- **Role:** AWS Signature Version 4 signing + AWS credential loading. Used to authenticate the Bedrock Converse path when no Bedrock bearer token is set.
- **Key symbols:**
  - `awsCreds` (struct) — access key / secret / session token.
  - `loadAWSCreds(profile)` — resolves creds from env first, else the named profile in `~/.aws/credentials`.
  - `parseAWSProfile(path, profile)` — minimal INI parser for one credentials section.
  - `signV4(req, body, creds, service, region, now)` — signs the request in place (canonical request → string-to-sign → HMAC chain → Authorization header).
  - `canonicalURI(escapedPath)` / `awsURIEncode(s)` — AWS double-encoding of non-S3 canonical paths.
  - `hmacSHA256` / `sha256hex` — signing primitives.
- **Depends on:** stdlib only.
- **Used by / entrypoint:** `converse.go` (`signV4` as the per-attempt `sign` callback; `loadAWSCreds` at construction and per-request re-resolution). Service string `"bedrock"`.

### internal/llm/anthropic.go
- **Role:** Native Anthropic Messages API provider (`api.anthropic.com/v1/messages`), `Complete` + `Stream`. Its distinguishing feature: it can reuse **Claude Code OAuth** credentials from `~/.claude/.credentials.json`, spoofing Claude Code's headers/system block so a Pro/Max subscription drives eigen.
- **Key symbols:**
  - `Anthropic` (struct) + `NewAnthropic(model)` — resolves `ANTHROPIC_API_KEY`/`EIGEN_ANTHROPIC_API_KEY` (x-api-key) else the Claude Code OAuth token; pulls cache/1M/adaptive/thinking-budget caps from the catalog.
  - `buildBody(req, stream)` — shared request builder for both paths; adaptive models send `thinking.type=adaptive` + `output_config.effort`, budget models `thinking.type=enabled`+`budget_tokens`.
  - `Complete(ctx, req)` — non-streaming POST; refuses `max_tokens` truncation; maps text/thinking/tool_use blocks back, capturing the thinking `signature` into `Response.ReasoningEncrypted` for interleaved-thinking carry-back.
  - `Stream(ctx, req, sink)` — streamed Messages SSE (`stream:true`); accumulates per-content-block deltas (text/thinking/signature/`input_json` tool args), folds `anthropicStreamUsage` from `message_start`/`message_delta`, refuses `max_tokens` truncation. This is the live default Claude-API path.
  - `Name`/`ModelID`/`SetEffort`/`Effort`/`snapshotThinking` — `EffortSetter` surface + thread-safe snapshot.
  - `systemBlocks(system)` — first block is the mandatory `claudeCodeSpoof`, then the caller's prompt (cached when caching on).
  - `headers()` — mirrors Claude Code (`anthropic-version`, the `anthropicBetas` set incl. mandatory `oauth-2025-04-20`, Bearer for OAuth / x-api-key otherwise).
  - `anthropicMessages(req)` — neutral→native transcript; re-emits the prior signed thinking block (from `ReasoningEncrypted`) before tool_use, groups tool results into one user turn, drops empty assistant turns.
  - `anthropicTools(tools, cache)` / `isAnthropicImageType(mt)` — tool mapping with a tool-prefix cache breakpoint; image media-type gate.
  - `anthropicStreamUsage` (+ `applyTo`) — folds non-zero input/output/cache counts from streamed usage events into the running `Usage`.
  - `claudeCredentialsPath()` / `claudeOAuthToken(path)` — locate & read an unexpired Claude Code OAuth token (no refresh).
  - wire types: `anthropicRequest` (with `Thinking`/`OutputConfig`/`Stream`)/`anthropicMessage`/`anthropicContent` (text/image/thinking/tool_use/tool_result)/`anthropicTool`/`anthropicReply`/`anthropicTextBlock`/`anthropicCacheCtl`/`anthropicImageSrc`.
- **Depends on:** `http.go` (`httpJSON`, `httpStream`, `maxResponseBytes`), `converse.go` (`firstNonEmpty`, `normalizeArgsRaw`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`, `effortBudget`, `budgetToEffort`).
- **Used by / entrypoint:** `provider.go` `New` → `NewAnthropic` (provider names `anthropic`/`claude-code`/`claude-api`). Wire types + `anthropicStreamUsage` also reused by `customAnthropic`/`parseAnthropicSSE` in `custom.go`.

### internal/llm/converse.go
- **Role:** Bedrock Runtime **Converse API** provider for Anthropic Claude (and other Converse-capable models), `Complete` + `Stream`. Auths via SigV4 from an AWS profile **or** a Bedrock bearer token (`AWS_BEARER_TOKEN_BEDROCK`, which makes a remote daemon work with no `~/.aws` file). Also the home of several shared helpers and the AWS event-stream frame decoder used by `Stream`.
- **Key symbols:**
  - `Converse` (struct, incl. test-only `baseURL`) + `NewConverse(model)` — region/profile/creds/bearer resolution; cache/1M/thinking-budget/adaptive from catalog with `EIGEN_CONVERSE_*` / `EIGEN_THINKING_BUDGET` overrides.
  - `buildPayload(req)` — shared request body for both endpoints: content-block messages, system + tool cachePoints, `additionalModelRequestFields` for 1M-beta + thinking.
  - `auth()` — returns headers + the per-request `sign` callback (bearer header, else SigV4 with freshly re-resolved profile creds for the long-lived daemon).
  - `endpointURL(action)` — builds the regional Bedrock model endpoint for `"converse"` / `"converse-stream"` (honors `baseURL` in tests).
  - `Complete(ctx, req)` — POSTs `/converse`; refuses `max_tokens` truncation; maps text/toolUse blocks + cache usage.
  - `Stream(ctx, req, sink)` — POSTs `/converse-stream` (AWS event-stream binary framing, not SSE); decodes contentBlockStart/Delta (text / reasoningContent / partial toolUse input), messageStop, metadata (usage), and the Bedrock exception events; refuses `max_tokens` truncation. The live default Claude-on-Bedrock path.
  - `Name`/`ModelID`/`SetEffort`/`Effort`/`snapshotSettings` — `EffortSetter` surface.
  - `additionalConverseFields(...)` (pkg fn) — builds the extra-fields JSON (adaptive vs budget thinking, 1M beta); `(*Converse).additionalFields()` is a thin wrapper over it.
  - `converseMessages(req)` — neutral→Converse blocks, grouping tool results into one user turn (tool-result images ride as image blocks); **drops empty assistant turns** (reasoning-only) that Converse would 400 on.
  - `converseTools` / `converseImageFormat` — tool & image mapping with cachePoint.
  - **AWS event-stream decoder:** `eventStreamReader` + `newEventStreamReader` / `(*eventStreamReader).next` / `parseEventType` — minimal `vnd.amazon.eventstream` frame reader (prelude + headers + payload + CRC) that extracts the `:event-type` header and JSON payload; no AWS SDK vendored.
  - **Shared helpers (used package-wide):** `effortBudget` (map), `budgetToEffort`, `normalizeArgsRaw`, `firstNonEmpty`, `envBool`, `envInt`, `urlPathEscape`.
  - wire types: `converseRequest`/`converseMessage`/`converseContent`/`converseToolUse`/`converseToolResult`/`converseCachePoint`/`converseImage`/`converseImageSource`/`converseInference`/`converseReply` etc.
- **Depends on:** `http.go` (`httpJSON`, `httpStream`, `maxResponseBytes`), `sigv4.go` (`signV4`, `loadAWSCreds`, `awsCreds`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`).
- **Used by / entrypoint:** `provider.go` `New` → `NewConverse` (provider names `converse`/`bedrock-converse`/`claude`). `firstNonEmpty`/`envBool`/`envInt`/`normalizeArgsRaw`/`urlPathEscape`/`effortBudget`/`budgetToEffort` consumed by anthropic, codex, grok, glm, mantle, custom, imagegen.

### internal/llm/mantle.go
- **Role:** Bedrock "mantle" provider for OpenAI-family models (GPT-5.5) via the **OpenAI Responses API**, authed with a Bedrock long-term API key (Bearer). Owns the shared Responses-API request/reply types, the `buildInput` transcript serializer (incl. vision + reasoning carry), and the empty-response/transient-failure retry handling.
- **Key symbols:**
  - `Mantle` (struct) + `NewMantle(model)` — requires `AWS_BEARER_TOKEN_BEDROCK`; region `EIGEN_MANTLE_REGION` (default us-east-2); effort from catalog with model-validated `EIGEN_REASONING_EFFORT` override.
  - `Complete(ctx, req)` — Responses POST; retries the known empty-completed-response quirk up to `maxEmptyRetries`; refuses `incomplete` truncation.
  - `Stream(ctx, req, sink)` — SSE; retries pre-output `response.failed` (`maxStreamFailRetries`) with backoff; recovers a full reply that was spuriously flagged failed.
  - `streamOnce` / `outputFromFailed` / `streamFailReason` — SSE attempt (returns `(final, emitted, failErr, err)`), failed-event output recovery, error extraction.
  - `Name`/`ModelID`/`SetEffort`/`Effort`/`snapshot` — `EffortSetter` surface.
  - `post(ctx, body)` — non-stream POST via `httpJSON` (honors `EIGEN_DEBUG_REQUEST`).
  - **Shared Responses helpers (used by codex + custom):** `parseReply(raw)` (also fills `Reasoning`/`ReasoningID`), `buildInput(req)` (emits `developer` system, `function_call_output`, vision via `inputParts`, and reasoning items only when an `encrypted_content` blob is present), `inputParts`, `toResponsesTools`, `argString`, `normalizeArgs`, `outputText`, `jsonStr`, `mantleUsage` (splits cached tokens out of input).
  - consts `reasoningEffort`/`reasoningSummary`, `maxEmptyRetries`, `maxStreamFailRetries`.
  - wire types: `responsesRequest` (incl. `Instructions`/`Reasoning`/`Stream`/`ServiceTier`/`Store`/`Include`)/`responsesInputItem` (with `Summary`/`Encrypted` for reasoning carry)/`responsesReply` (with `incomplete_details`/`reasoning` summary/usage cached split)/`responsesTool`/`reasoningConfig`/`summaryPart`.
- **Depends on:** `http.go` (`httpJSON`, `httpStream`, `sleepBackoff`, `maxResponseBytes`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`, `effortSupported`).
- **Used by / entrypoint:** `provider.go` `New` → `NewMantle` (default/empty provider, names `mantle`/`bedrock-mantle`). Responses wire types + helpers reused by `codex.go` and `custom.go` (`customOpenAIResponses`).

### internal/llm/codex.go
- **Role:** OpenAI model via the **Codex** backend — the same path the `codex` CLI uses: Responses API over `chatgpt.com/backend-api/codex` with a ChatGPT-account OAuth token (not `api.openai.com`). Reuses mantle's Responses shapes + SSE assembler; adds OAuth refresh and the `service_tier` "fast mode" knob. Owns the shared `parseResponsesSSE` assembler.
- **Key symbols:**
  - `Codex` (struct) + `NewCodex(model)` — reads `~/.codex/auth.json` (ChatGPT login required); rejects API-key-only auth; effort + service tier from catalog/env.
  - `Complete` — delegates to `Stream(ctx, req, nil)` since the backend is stream-only.
  - `Stream(ctx, req, sink)` — SSE via `parseResponsesSSE`; retries transient `response.failed` only if nothing was emitted.
  - `openStream` / `refreshToken` / `persist` / `readCodexAuth` / `codexAuthPath` — token lifecycle: on 401, refresh via `auth.openai.com/oauth/token` (refresh-grant) and rewrite auth.json.
  - `buildPayload(req, stream)` — pulls `System` into the top-level `instructions`, sets `store:false` + `include:[reasoning.encrypted_content]`, the service tier, and the stream flag.
  - `openStream` — POSTs the stream; on a 401 refreshes the token once and retries.
  - `headers` — Bearer + `ChatGPT-Account-Id` + `OpenAI-Beta`/`originator`.
  - `SetEffort`/`Effort`/`FastMode`/`SetFast`/`snapshot` — `EffortSetter` + `FastModer` surfaces; `normalizeTier`.
  - `isTransientCodexStreamFailure` / `isUnauthorized` — error classifiers.
  - **`parseResponsesSSE(resp, sink)` (pkg fn) — the single SSE assembler for BOTH codex and mantle wire shapes**; collects authoritative output from `response.output_item.done` (Codex's empty `response.completed` channel), falls back to the completed reply (mantle) and accumulated deltas, recovers a spuriously-failed reply. `applyOutputItem` folds one item (function_call/message/reasoning) into the response, pairing `ReasoningID` with its own `encrypted_content` blob.
- **Depends on:** `http.go` (`httpStream`, `sleepBackoff`, `maxResponseBytes`), `mantle.go` (`responsesRequest`, `parseReply`, `buildInput`, `toResponsesTools`, `reasoningConfig`, `reasoningSummary`, `normalizeArgs`, `outputFromFailed`, `streamFailReason`, `maxStreamFailRetries`), `converse.go` (`firstNonEmpty`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`, `effortSupported`, `reasoningEffort`).
- **Used by / entrypoint:** `provider.go` `New` → `NewCodex` (names `codex`/`openai-codex`/`chatgpt`). `parseResponsesSSE` is also invoked by `custom.go` (`customOpenAIResponses.Stream`).

### internal/llm/openaichat.go
- **Role:** `chatClient` — the reusable OpenAI-compatible `/v1/chat/completions` backend shared by every chat-dialect provider (llama, grok, glm, custom-openai-chat). Handles transcript→chat-message mapping, function-tool wrapping, vision parts, non-streaming + SSE-delta parsing, and pluggable per-provider hooks.
- **Key symbols:**
  - `chatClient` (struct) + `newChatClient(baseURL, model, apiKey, label)` — with hook fields `extra` (extra body fields), `extraTools` (extra tool entries), `extraHeaders`.
  - `complete(ctx, req)` / `stream(ctx, req, sink)` — non-streaming and SSE chat completion; stream forwards content + `reasoning_content` deltas to sink and assembles fragmented tool-call deltas keyed by index.
  - `body(req, stream)` — merges hooks (`extra`, `extraTools`) into the JSON payload.
  - `headers()` — Bearer + static extra headers.
  - `chatMessagesFrom(req)` / `flushToolImgsChat(...)` / `chatToolsFrom(specs)` — neutral→chat mapping; vision parts inline, tool-result images buffered into one synthetic user message.
  - `chatUsage.usage()` / `chatUsage.CachedTokens()` — OpenAI usage → neutral `Usage` with cache split.
  - wire types: `chatRequest`/`chatMessage`/`chatPart`/`chatTool`/`chatToolCall`/`chatFunction`/`chatReply` (text + `reasoning_content`)/`chatUsage`/`chatImageURL`.
- **Depends on:** `http.go` (`httpJSON`, `httpStream`, `maxResponseBytes`), `mantle.go` (`argString`, `normalizeArgs`).
- **Used by / entrypoint:** instantiated by `llama.go`, `grok.go`, `glm.go`, and `custom.go` (`customOpenAIChat`). Reached via those providers' `Complete`/`Stream`.

### internal/llm/llama.go
- **Role:** Thin provider for any OpenAI-compatible `/v1/chat/completions` server (primarily a local llama.cpp `llama-server`). Pure delegation to `chatClient`.
- **Key symbols:**
  - `Llama` (struct) + `NewLlama(model)` — base URL defaults to `http://localhost:8080/v1` (`EIGEN_LLAMA_BASE_URL`/`EIGEN_LLAMA_API_KEY`).
  - `Name`/`ModelID`/`Complete`/`Stream` — `Complete`/`Stream` forward to the embedded `chatClient`'s `complete`/`stream`.
- **Depends on:** `openaichat.go` (`chatClient`, `newChatClient`).
- **Used by / entrypoint:** `provider.go` `New` → `NewLlama` (names `llama`/`local`).

### internal/llm/grok.go
- **Role:** xAI **Grok** provider over the OpenAI-compatible chat API, adding xAI **Live Search** (`search_parameters`). Falls back from `XAI_API_KEY` to a grok-cli OIDC session token against the cli-chat-proxy.
- **Key symbols:**
  - `Grok` (struct) + `NewGrok(model)` — key/base resolution, grok-cli proxy headers, search default from catalog, `EIGEN_GROK_*` overrides; wires `g.searchParams` into `chatClient.extra`.
  - `Complete`/`Stream` — clone the chatClient with a snapshot-bound `extra` + a search-hint system prompt.
  - `SetSearch`/`SearchMode`/`snapshot` — `Searcher` surface.
  - `searchParams()` / `grokSearchParams(...)` — build the `search_parameters` field (nil when off).
  - `grokPrepare(req, search, sources)` (pkg fn) — appends the "prefer built-in search over fetch" hint.
  - `splitCSV`, `grokCLIClientVersion`, `grokCLIToken` — env list parsing + grok-cli auth-store reading (freshest unexpired bearer).
- **Depends on:** `openaichat.go` (`chatClient`), `converse.go` (`firstNonEmpty`), catalog (`Lookup`).
- **Used by / entrypoint:** `provider.go` `New` → `NewGrok` (names `grok`/`xai`). Compile-time `Searcher` check is implicit via interface use in the TUI/agent.

### internal/llm/glm.go
- **Role:** Zhipu **GLM** provider over the OpenAI-compatible "coding plan" chat API, adding the server-side `web_search` built-in tool and GLM's two-mode (`enabled`/`disabled`) thinking with "Preserved Thinking".
- **Key symbols:**
  - `GLM` (struct) + `NewGLM(model)` — key (`GLM_API_KEY`/`ZHIPUAI_API_KEY`/`EIGEN_GLM_API_KEY`), thinking mode from catalog, `EIGEN_GLM_*` overrides; wires `g.webSearchTool` into `extraTools` and `g.bodyExtra` into `extra`.
  - `Complete`/`Stream` — clone the chatClient with snapshot-bound hooks + search-hint prompt.
  - `SetSearch`/`SearchMode` (`Searcher`) and `SetEffort`/`Effort` (`EffortSetter`, mapping eigen vocab → enabled/disabled); `snapshot`.
  - `bodyExtra()` / `glmBodyExtra(...)` — `thinking.type` + `clear_thinking:false` preservation.
  - `webSearchTool()` / `glmWebSearchTool(search)` — the GLM `web_search` tool entry (nil when off).
  - `glmPrepare(req, search)` (pkg fn) — prefer-web_search-over-fetch hint.
  - compile-time asserts `_ Searcher = (*GLM)(nil)`, `_ EffortSetter = (*GLM)(nil)`.
- **Depends on:** `openaichat.go` (`chatClient`), `converse.go` (`firstNonEmpty`), catalog (`Lookup`).
- **Used by / entrypoint:** `provider.go` `New` → `NewGLM` (names `glm`/`zhipu`/`z.ai`). `llm.New("glm", "glm-5.2")` is also called as the proactive-feed suggester's credentialed-default probe in `main_gui_wails.go` and `internal/app/data.go` (glm-5.2 = 1M-ctx flagship with web_search "auto" so ideas can be web-grounded).

### internal/llm/custom.go
- **Role:** User-defined providers stored in `~/.eigen/providers.json` — a committable/exportable catalog (API keys referenced by env var, or inlined for private local configs). Validates and persists the catalog, and at dispatch time builds the right streaming adapter (`customOpenAIChat`, `customOpenAIResponses`, or `customAnthropic`) reusing the wire code from the built-in providers. All three implement `Streamer` + `EffortSetter`.
- **Key symbols:**
  - `CustomProvider` (incl. `API`/`APIKeyEnv`/`APIKey`/`NoAuth`/`Version` fields) / `CustomModel` (incl. `EffortLevels`/`Vision`/`Search`/`Social`) — the on-disk catalog shape.
  - `CustomProvidersPath`, `LoadCustomProviders`, `SaveCustomProviders` (atomic temp-file write, 0600), `UpsertCustomProvider`, `ValidateCustomProvider`, `validateCustomCatalog`, `mergeCustomProvider`, `normalizeCustom*` — catalog CRUD/validation (rejects reserved/built-in names, model-name collisions with the built-in catalog, non-loopback http with creds, etc.).
  - `customModels()` — exposes custom models as `ModelInfo` for the catalog.
  - `customKind`, `isBuiltinProvider`, `customProviderByName`, `customModelByName`, `customProviderAvailable`, `customAPIKey`, `defaultCustomWindow`, `lookupBuiltinModel`, `isLoopbackHost`, `validateCustomBaseURL`, `normalizeOpenAIBase`, `normalizeAnthropicBase`.
  - `customEffort` (embedded struct) + `newCustomEffort`/`customEffortLevels` — the shared runtime effort knob (`SetEffort`/`Effort`/`snapshot`) all three adapters embed to satisfy `EffortSetter`.
  - `newCustomProvider(providerName, modelName)` — the dispatch invoked by `provider.New`'s default case; routes to `newCustomOpenAIChat`/`newCustomOpenAIResponses`/`newCustomAnthropic`.
  - adapters `customOpenAIChat` (wraps `chatClient`, injects `reasoning_effort` via its `extra` hook), `customOpenAIResponses` (Responses via `buildInput`/`toResponsesTools`/`parseReply`; `Stream` uses `parseResponsesSSE`; retries the empty-completed quirk), `customAnthropic` (Anthropic Messages via `anthropicMessages`/`anthropicTools`/`anthropicReply`; `Stream` uses `parseAnthropicSSE`).
  - `parseAnthropicSSE(resp, label, sink)` (pkg fn) — native Messages SSE assembler for custom Anthropic providers (reuses `anthropicStreamUsage`); the label prefixes errors with the provider name.
- **Depends on:** `openaichat.go` (`chatClient`), `mantle.go` (`responsesRequest`, `buildInput`, `toResponsesTools`, `parseReply`, `reasoningConfig`, `reasoningSummary`, `maxEmptyRetries`), `codex.go` (`parseResponsesSSE`), `anthropic.go` (`anthropicRequest`, `anthropicMessages`, `anthropicTools`, `anthropicReply`, `anthropicStreamUsage`, `anthropicVersion`, `anthropicMaxTok`), `http.go` (`httpJSON`, `httpStream`, `maxResponseBytes`), `converse.go` (`firstNonEmpty`/`normalizeArgsRaw` via shared helpers), catalog (`Catalog`, `ModelInfo`, `canonicalProvider`).
- **Used by / entrypoint:** `provider.go` `New` default case → `newCustomProvider`. Catalog CRUD (`Load/Save/Upsert/Validate/CustomProvidersPath`) used by `internal/app/{pages,data}.go`.

### internal/llm/vendor.go
- **Role:** Cross-vendor model classification used by review/judging and the planning council: same-family models share blind spots, so review always crosses vendors.
- **Key symbols:**
  - `Vendor` (enum: Unknown/Anthropic/OpenAI/XAI/Zhipu) + `VendorOf(model)` — classify a model id by substring.
  - `CrossReviewer(author, candidates)` — pick the canonical opposite-vendor reviewer (GPT↔Claude pairing; GPT for grok/glm/unknown), strongest by tier+rank.
  - `CrossVendorAdversaries(author, candidates)` — ordered list of non-author-vendor adversaries (primary cross-vendor first), used by the planning council's fallback.
  - `sortByRankDesc` — helper insertion sort by rank.
- **Depends on:** `scoreFor` (sibling `routerscores.go`).
- **Used by / entrypoint:** `VendorOf`, `CrossReviewer` — `internal/llm/{review.go,council.go}`. `CrossVendorAdversaries` — `router.go` (repo root) at the council adversary-fallback step.

### internal/llm/review.go
- **Role:** Cross-vendor artifact review: ask one model to critique work produced by another. Independence is structural — the caller picks the reviewer from the other vendor (`vendor.go`).
- **Key symbols:**
  - `ReviewArtifact(ctx, reviewer, reviewerID, authorID, artifact, focus)` — runs one `Provider.Complete` with the rigorous-critic system prompt, framing the author by vendor; returns the review text.
  - `reviewPrompt` (template), `authorVendorLabel(authorID)` — describe the author for the reviewer.
- **Depends on:** `llm.go` (`Provider`, `Request`, `Message`), `vendor.go` (`VendorOf`).
- **Used by / entrypoint:** `router.go` (repo root) review step, `council.go` (sibling).

### internal/llm/compact_summary.go
- **Role:** Model-backed context compaction: when the transcript exceeds budget, shed old tool-result payloads first, then replace older history with a single model-generated summary merged into the first retained user turn.
- **Key symbols:**
  - `Compactor` (interface: `Summarize`), `NewCompactor(p)` / `providerCompactor`, `CompactorChain(...)` / `chainCompactor` — adapt a `Provider` into a summarizer, with a fallback chain (cheap model first, main provider as backup).
  - `CompactWith(ctx, c, msgs, maxTokens)` — the entry point: microcompaction (`ShedToolResults`/`ShedOldToolResults`) → model summary at user boundaries → `compactFit` last-resort tail.
  - `compactFit`, `userStarts`, `firstUserText` — helpers.
  - `summaryPrompt` / `summaryReinjectPrefix` (Codex-style handoff framing), consts `shedKeepRounds`/`shedKeepToolResults`.
- **Depends on:** `llm.go` (`Message`, `Request`), `compact.go` (`EstimateTokens`, `Compact`), `shed.go` (`ShedToolResults`, `ShedOldToolResults`).
- **Used by / entrypoint:** `internal/agent`/`internal/chat` compaction paths; `Compactor` is constructed wherever a summarizing model is wired (e.g. via `NewCompactor`/`CompactorChain`).

### internal/llm/imagegen.go
- **Role:** Generative-image backend — turns a prompt into PNG image(s) via Amazon Bedrock InvokeModel, reusing the Converse provider's SigV4 + AWS-profile auth. Supports both the Stability text-to-image dialect (default) and the Nova Canvas / Titan Image dialect, chosen by model-id prefix.
- **Key symbols:**
  - `ImageGenerator` (interface: `Generate`/`ModelID`), `ImageOpts` (width/height/count/seed).
  - `bedrockImager` + `NewImageGenerator()` — config from `EIGEN_IMAGE_MODEL`/`EIGEN_IMAGE_REGION`/`EIGEN_IMAGE_PROFILE` (defaults stability core, us-west-2, aviary).
  - `Generate` / `invoke` — build the dialect payload, sign + POST `/invoke`, decode `{images:[base64]}`; loops Stability (one image per call) to honor count.
  - `aspectRatio(w, h)` — pixel dims → Stability aspect-ratio enum; wire types `novaImageRequest`/`novaImageResponse`.
- **Depends on:** `sigv4.go` (`signV4`, `loadAWSCreds`), `http.go` (`httpJSON`), `converse.go` (`urlPathEscape`), `embed.go` (`firstNonEmptyEnv`).
- **Used by / entrypoint:** the image-generation tool / agent surface (the only generative-image model kind in the catalog).

## Cross-links

- **internal/llm (siblings, not in this slice):** `catalog.go` (`Lookup`, `ModelInfo`, `Catalog`, `ModelEffortLevels` data, `customModels`), `ref.go` (`ParseRef`), provider resolution (`ResolveProvider`, `canonicalProvider`), `routerscores.go` (`scoreFor`, `RouterScore`), `router.go`/`council.go`/`candidates.go` (consume `Provider`, `VendorOf`, `CrossReviewer`, `CrossVendorAdversaries`, `ReviewArtifact`), `compact.go`/`shed.go` (`EstimateTokens`/`Compact`/`Shed*` consumed by `compact_summary.go`), `embed.go` (`firstNonEmptyEnv`)/`classify.go` (other `Provider` consumers).
- **internal/agent (`agent.go`):** drives `Provider`/`Streamer` — uses `Stream(ctx, req, sink)` when `OnEvent` is set (else `Complete`), and the optional `EffortSetter`/`Searcher`/`FastModer` capabilities (subtask effort + fast-mode fallback).
- **internal/tui:** runtime toggles via the optional interfaces (`switches.go`, `commands.go`, `plan.go` — effort/search/fast).
- **internal/chat (`local.go`):** wraps a `Provider` and re-exposes `EffortSetter`/`Searcher`/`FastModer` to the GUI chat path.
- **internal/gui (`bridge.go`):** Wails-bound `SetFast` etc. ride through the daemon client down to these providers; `Version()` bridge method returns `llm.FullVersion()`. `internal/daemon/host.go` reports `FullVersion()` as the build identity.
- **internal/app (`pages.go`, `data.go`):** the custom-provider catalog UI (`LoadCustomProviders`/`SaveCustomProviders`/`UpsertCustomProvider`/`ValidateCustomProvider`); `data.go` also calls `llm.New`.
- **Repo-root command layer:** `build.go`, `main.go`, `daemon.go`, `router.go`, `plugincmd.go`, `main_gui_wails.go` all call `llm.New` (`main.go` also prints `llm.FullVersion()`).
- **External services:** api.anthropic.com, AWS Bedrock (Converse + mantle + image invoke), chatgpt.com Codex backend + auth.openai.com, api.x.ai / grok cli-chat-proxy, api.z.ai (Zhipu), and arbitrary OpenAI-compatible/Anthropic-compatible endpoints (llama/custom).
