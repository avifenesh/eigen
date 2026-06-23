# LLM provider backends & transport

> This area is the set of concrete model backends behind eigen's `llm.Provider`
> contract, plus the shared HTTP/auth plumbing they ride on. Each provider
> translates eigen's neutral transcript (`Request`/`Message`/`ToolSpec` from
> `llm.go`) into one vendor's wire format, sends it over the shared retrying HTTP
> transport (`http.go`, signing via `sigv4.go` for AWS), and maps the reply back
> into a neutral `Response`. `llm.New` (in `provider.go`) is the factory that picks
> the right backend from a `(provider, model)` pair or a `tag:model` ref, so the
> agent loop, daemon, router, and GUI never see provider-specific shapes. Wire
> dialects cluster into three families: Anthropic Messages (native + Bedrock
> Converse), OpenAI Responses (Bedrock mantle + Codex/ChatGPT), and OpenAI
> chat-completions (llama/grok/glm + custom), with `vendor.go` providing the
> cross-vendor classification used for review/judging.

## Files

### internal/llm/provider.go
- **Role:** The provider factory: resolves a `(provider, model)` pair (or a `tag:model` ref) to a concrete backend constructor. This is the single entrypoint into the whole slice.
- **Key symbols:**
  - `New(provider, model string) (Provider, error)` — strips any stray `Name()` suffix, parses an explicit `tag:` ref (which force-selects the backend), else `ResolveProvider`s from the catalog, canonicalizes aliases, then dispatches to `NewMantle`/`NewLlama`/`NewConverse`/`NewAnthropic`/`NewCodex`/`NewGrok`/`NewGLM` or `newCustomProvider`.
- **Depends on:** `ParseRef`, `ResolveProvider`, `canonicalProvider` (sibling files `ref.go`/`provider`-resolution in package `llm`).
- **Used by / entrypoint:** **entrypoint** for the whole area. Called from `build.go`, `router.go`, `daemon.go`, `main.go`, `main_gui_wails.go`, `plugincmd.go`.

### internal/llm/http.go
- **Role:** Shared HTTP transport: JSON POST and SSE-stream POST with jittered exponential backoff, Retry-After handling, and a response-size cap. Every provider in this slice sends through here.
- **Key symbols:**
  - `Version` (const `"0.1.0"`) — eigen version, sent as the User-Agent.
  - `httpJSON(ctx, client, url, headers, body, sign)` — POSTs JSON, retries network/429/5xx, returns `(body, status, err)`; `sign` is re-invoked per attempt (SigV4 freshness).
  - `httpStream(ctx, client, url, headers, body, sign)` — POSTs and returns the open `*http.Response` for SSE on a 2xx; retries only the initial connect; non-2xx returned as an error string.
  - `sleepBackoff(ctx, attempt, retryAfter)` — exponential delay (honoring Retry-After) + jitter, cancellable.
  - `parseRetryAfter(h)` — parses Retry-After as delta-seconds or HTTP-date.
- **Depends on:** stdlib only.
- **Used by / entrypoint:** `httpJSON` — anthropic, converse, mantle, custom (responses/anthropic), `chatClient`. `httpStream` — codex, mantle, `chatClient`. `sleepBackoff` reused for retry pacing in codex/mantle stream loops.

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
- **Role:** Native Anthropic Messages API provider (`api.anthropic.com/v1/messages`). Its distinguishing feature: it can reuse **Claude Code OAuth** credentials from `~/.claude/.credentials.json`, spoofing Claude Code's headers/system block so a Pro/Max subscription drives eigen.
- **Key symbols:**
  - `Anthropic` (struct) + `NewAnthropic(model)` — resolves `ANTHROPIC_API_KEY` (x-api-key) else the Claude Code OAuth token; pulls cache/1M/adaptive/thinking-budget caps from the catalog.
  - `Complete(ctx, req)` — builds the Messages request (adaptive `thinking.type=adaptive`+`output_config.effort` vs budget `thinking.type=enabled`), refuses `max_tokens` truncation, maps content/thinking/tool_use blocks back.
  - `Name`/`ModelID`/`SetEffort`/`Effort`/`snapshotThinking` — `EffortSetter` surface + thread-safe snapshot.
  - `systemBlocks(system)` — first block is the mandatory `claudeCodeSpoof`, then the caller's prompt (cached when caching on).
  - `headers()` — mirrors Claude Code (`anthropic-version`, `anthropic-beta` set, Bearer for OAuth / x-api-key otherwise).
  - `anthropicMessages(req)` / `anthropicTools(tools, cache)` / `isAnthropicImageType(mt)` — neutral→native transcript & tool mapping, with a tool-prefix cache breakpoint.
  - `claudeCredentialsPath()` / `claudeOAuthToken(path)` — locate & read an unexpired Claude Code OAuth token (no refresh).
  - wire types: `anthropicRequest`/`anthropicMessage`/`anthropicContent`/`anthropicTool`/`anthropicReply`/`anthropicTextBlock`/`anthropicCacheCtl`/`anthropicImageSrc`.
- **Depends on:** `http.go` (`httpJSON`), `converse.go` (`firstNonEmpty`, `normalizeArgsRaw`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`, `effortBudget`, `budgetToEffort`).
- **Used by / entrypoint:** `provider.go` `New` → `NewAnthropic` (provider names `anthropic`/`claude-code`/`claude-api`). Wire types also reused by `customAnthropic` in `custom.go`.

### internal/llm/converse.go
- **Role:** Bedrock Runtime **Converse API** provider for Anthropic Claude (and other Converse-capable models). Auths via SigV4 from an AWS profile **or** a Bedrock bearer token (`AWS_BEARER_TOKEN_BEDROCK`, which makes a remote daemon work with no `~/.aws` file). Also the home of several shared helpers used across the slice.
- **Key symbols:**
  - `Converse` (struct) + `NewConverse(model)` — region/profile/creds/bearer resolution; cache/1M/thinking-budget/adaptive from catalog with `EIGEN_CONVERSE_*` / `EIGEN_THINKING_BUDGET` overrides.
  - `Complete(ctx, req)` — builds the Converse content-block request, system + tool cachePoints, `additionalModelRequestFields` for 1M-beta + thinking; re-resolves AWS creds per request (long-lived daemon); refuses `max_tokens` truncation.
  - `Name`/`ModelID`/`SetEffort`/`Effort`/`snapshotSettings` — `EffortSetter` surface.
  - `additionalConverseFields(...)` (pkg fn) — builds the extra-fields JSON (adaptive vs budget thinking, 1M beta); `(*Converse).additionalFields()` is a thin wrapper (see dead-code note).
  - `converseMessages(req)` — neutral→Converse blocks, grouping tool results into one user turn; **drops empty assistant turns** (reasoning-only) that Converse would 400 on.
  - `converseTools` / `converseImageFormat` — tool & image mapping with cachePoint.
  - **Shared helpers (used package-wide):** `effortBudget` (map), `budgetToEffort`, `normalizeArgsRaw`, `firstNonEmpty`, `envBool`, `envInt`, `urlPathEscape`.
  - wire types: `converseRequest`/`converseMessage`/`converseContent`/`converseToolUse`/`converseToolResult`/`converseCachePoint`/`converseImage`/`converseReply` etc.
- **Depends on:** `http.go` (`httpJSON`), `sigv4.go` (`signV4`, `loadAWSCreds`, `awsCreds`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`).
- **Used by / entrypoint:** `provider.go` `New` → `NewConverse` (provider names `converse`/`bedrock-converse`/`claude`). `firstNonEmpty`/`envBool`/`envInt`/`normalizeArgsRaw`/`urlPathEscape` consumed by anthropic, codex, grok, glm, mantle, custom, imagegen.

### internal/llm/mantle.go
- **Role:** Bedrock "mantle" provider for OpenAI-family models (GPT-5.5) via the **OpenAI Responses API**, authed with a Bedrock long-term API key (Bearer). Owns the shared Responses-API request/reply types, the `buildInput` transcript serializer, and the empty-response/transient-failure retry handling.
- **Key symbols:**
  - `Mantle` (struct) + `NewMantle(model)` — requires `AWS_BEARER_TOKEN_BEDROCK`; effort from catalog with model-validated `EIGEN_REASONING_EFFORT` override.
  - `Complete(ctx, req)` — Responses POST; retries the known empty-completed-response quirk up to `maxEmptyRetries`; refuses `incomplete` truncation.
  - `Stream(ctx, req, sink)` — SSE; retries pre-output `response.failed` (`maxStreamFailRetries`) with backoff; recovers a full reply that was spuriously flagged failed.
  - `streamOnce` / `outputFromFailed` / `streamFailReason` — SSE attempt, failed-event recovery, error extraction.
  - `Name`/`ModelID`/`SetEffort`/`Effort`/`snapshot` — `EffortSetter` surface.
  - `post(ctx, body)` — non-stream POST via `httpJSON`.
  - **Shared Responses helpers (used by codex + custom):** `parseReply(raw)`, `buildInput(req)`, `inputParts`, `toResponsesTools`, `argString`, `normalizeArgs`, `outputText`, `jsonStr`, `mantleUsage`.
  - consts `reasoningEffort`/`reasoningSummary`, `maxEmptyRetries`, `maxStreamFailRetries`.
  - wire types: `responsesRequest`/`responsesInputItem`/`responsesReply`/`responsesTool`/`reasoningConfig`/`summaryPart`.
- **Depends on:** `http.go` (`httpJSON`, `httpStream`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`, `effortSupported`).
- **Used by / entrypoint:** `provider.go` `New` → `NewMantle` (default/empty provider, names `mantle`/`bedrock-mantle`). Responses wire types + helpers reused by `codex.go` and `custom.go` (`customOpenAIResponses`).

### internal/llm/codex.go
- **Role:** OpenAI model via the **Codex** backend — the same path the `codex` CLI uses: Responses API over `chatgpt.com/backend-api/codex` with a ChatGPT-account OAuth token (not `api.openai.com`). Reuses mantle's Responses shapes + SSE assembler; adds OAuth refresh and the `service_tier` "fast mode" knob. Owns the shared `parseResponsesSSE` assembler.
- **Key symbols:**
  - `Codex` (struct) + `NewCodex(model)` — reads `~/.codex/auth.json` (ChatGPT login required); rejects API-key-only auth; effort + service tier from catalog/env.
  - `Complete` — delegates to `Stream(ctx, req, nil)` since the backend is stream-only.
  - `Stream(ctx, req, sink)` — SSE via `parseResponsesSSE`; retries transient `response.failed` only if nothing was emitted.
  - `openStream` / `refreshToken` / `persist` / `readCodexAuth` / `codexAuthPath` — token lifecycle: on 401, refresh via `auth.openai.com/oauth/token` (refresh-grant) and rewrite auth.json.
  - `buildPayload` — pulls `System` into the top-level `instructions`, sets `store:false` + `include:[reasoning.encrypted_content]`.
  - `headers` — Bearer + `ChatGPT-Account-Id` + `OpenAI-Beta`/`originator`.
  - `SetEffort`/`Effort`/`FastMode`/`SetFast`/`snapshot` — `EffortSetter` + `FastModer` surfaces; `normalizeTier`.
  - `isTransientCodexStreamFailure` / `isUnauthorized` — error classifiers.
  - **`parseResponsesSSE(resp, sink)` (pkg fn) — the single SSE assembler for BOTH codex and mantle wire shapes**; `applyOutputItem` folds one `output_item.done` into the response.
- **Depends on:** `http.go` (`httpStream`, `sleepBackoff`), `mantle.go` (`responsesRequest`, `parseReply`, `buildInput`, `toResponsesTools`, `reasoningConfig`, `reasoningSummary`, `normalizeArgs`, `outputFromFailed`, `streamFailReason`, `maxStreamFailRetries`, `maxResponseBytes`), `converse.go` (`firstNonEmpty`), catalog (`Lookup`, `ModelEffortLevels`, `EffortLevels`, `effortSupported`, `reasoningEffort`).
- **Used by / entrypoint:** `provider.go` `New` → `NewCodex` (names `codex`/`openai-codex`/`chatgpt`). `parseResponsesSSE` is also the assembler invoked indirectly through codex's own `Stream`.

### internal/llm/openaichat.go
- **Role:** `chatClient` — the reusable OpenAI-compatible `/v1/chat/completions` backend shared by every chat-dialect provider (llama, grok, glm, custom-openai-chat). Handles transcript→chat-message mapping, function-tool wrapping, vision parts, non-streaming + SSE-delta parsing, and pluggable per-provider hooks.
- **Key symbols:**
  - `chatClient` (struct) + `newChatClient(baseURL, model, apiKey, label)` — with hook fields `extra` (extra body fields), `extraTools` (extra tool entries), `extraHeaders`.
  - `complete(ctx, req)` / `stream(ctx, req, sink)` — non-streaming and SSE chat completion; stream assembles fragmented tool-call deltas keyed by index.
  - `body(req, stream)` — merges hooks into the JSON payload.
  - `headers()` — Bearer + static extra headers.
  - `chatMessagesFrom(req)` / `flushToolImgsChat(...)` / `chatToolsFrom(specs)` — neutral→chat mapping; tool-result images buffered into one synthetic user message.
  - `chatUsage.usage()` / `chatUsage.CachedTokens()` — OpenAI usage → neutral `Usage` with cache split.
  - wire types: `chatRequest`/`chatMessage`/`chatPart`/`chatTool`/`chatToolCall`/`chatFunction`/`chatReply`/`chatUsage`/`chatImageURL`.
- **Depends on:** `http.go` (`httpJSON`, `httpStream`), `mantle.go` (`argString`, `normalizeArgs`).
- **Used by / entrypoint:** instantiated by `llama.go`, `grok.go`, `glm.go`, and `custom.go` (`customOpenAIChat`). Reached via those providers' `Complete`/`Stream`.

### internal/llm/llama.go
- **Role:** Thin provider for any OpenAI-compatible `/v1/chat/completions` server (primarily a local llama.cpp `llama-server`). Pure delegation to `chatClient`.
- **Key symbols:**
  - `Llama` (struct) + `NewLlama(model)` — base URL defaults to `http://localhost:8080/v1` (`EIGEN_LLAMA_BASE_URL`/`EIGEN_LLAMA_API_KEY`).
  - `Name`/`ModelID`/`Complete`/`Stream` — forward to the embedded `chatClient`.
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
- **Used by / entrypoint:** `provider.go` `New` → `NewGLM` (names `glm`/`zhipu`/`z.ai`). Also constructed directly in `main_gui_wails.go` as a credentialed-default probe.

### internal/llm/custom.go
- **Role:** User-defined providers stored in `~/.eigen/providers.json` — a committable/exportable catalog (API keys referenced by env var). Validates and persists the catalog, and at dispatch time builds the right adapter (`customOpenAIChat`, `customOpenAIResponses`, or `customAnthropic`) reusing the wire code from the built-in providers.
- **Key symbols:**
  - `CustomProvider` / `CustomModel` (structs) — the on-disk catalog shape.
  - `CustomProvidersPath`, `LoadCustomProviders`, `SaveCustomProviders` (atomic temp-file write, 0600), `UpsertCustomProvider`, `ValidateCustomProvider`, `validateCustomCatalog`, `mergeCustomProvider`, `normalizeCustom*` — catalog CRUD/validation (rejects reserved/built-in names, non-loopback http with creds, etc.).
  - `customModels()` — exposes custom models as `ModelInfo` for the catalog.
  - `customKind`, `isBuiltinProvider`, `customProviderByName`, `customModelByName`, `customProviderAvailable`, `customAPIKey`, `defaultCustomWindow`, `lookupBuiltinModel`, `isLoopbackHost`, `validateCustomBaseURL`, `normalizeOpenAIBase`, `normalizeAnthropicBase`.
  - `newCustomProvider(providerName, modelName)` — the dispatch invoked by `provider.New`'s default case.
  - adapters `customOpenAIChat` (wraps `chatClient`), `customOpenAIResponses` (Responses via `buildInput`/`toResponsesTools`/`parseReply`), `customAnthropic` (Anthropic Messages via `anthropicMessages`/`anthropicTools`/`anthropicReply`).
- **Depends on:** `openaichat.go` (`chatClient`), `mantle.go` (`responsesRequest`, `buildInput`, `toResponsesTools`, `parseReply`), `anthropic.go` (`anthropicRequest`, `anthropicMessages`, `anthropicTools`, `anthropicReply`, `anthropicVersion`, `anthropicMaxTok`), `http.go` (`httpJSON`), `converse.go` (`firstNonEmpty`/`normalizeArgsRaw` via shared helpers), catalog (`Catalog`, `ModelInfo`, `canonicalProvider`).
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

## Cross-links

- **internal/llm (siblings, not in this slice):** `catalog.go` (`Lookup`, `ModelInfo`, `Catalog`, `ModelEffortLevels` data), `ref.go` (`ParseRef`), provider resolution (`ResolveProvider`, `canonicalProvider`), `routerscores.go` (`scoreFor`), `router.go`/`review.go`/`council.go`/`candidates.go` (consume `Provider`, `VendorOf`, `CrossReviewer`, `CrossVendorAdversaries`), `imagegen.go` (reuses `urlPathEscape` + the SigV4 transport for Bedrock image invoke), `embed.go`/`classify.go`/`compact.go` (other `Provider` consumers).
- **internal/agent:** drives `Provider`/`Streamer` and the optional `EffortSetter`/`Searcher`/`FastModer` capabilities (`agent.go` fast-mode fallback).
- **internal/tui:** runtime toggles via the optional interfaces (`switches.go`, `commands.go`, `plan.go` — effort/search/fast).
- **internal/chat (`local.go`):** wraps a `Provider` and re-exposes `FastModer` to the GUI chat path.
- **internal/gui (`bridge.go`):** Wails-bound `SetFast` etc. ride through the daemon client down to these providers.
- **internal/app (`pages.go`, `data.go`):** the custom-provider catalog UI (`LoadCustomProviders`/`SaveCustomProviders`/`UpsertCustomProvider`/`ValidateCustomProvider`).
- **Repo-root command layer:** `build.go`, `main.go`, `daemon.go`, `router.go`, `plugincmd.go`, `main_gui_wails.go` all call `llm.New` — the area's only public entrypoint.
- **External services:** api.anthropic.com, AWS Bedrock (Converse + mantle), chatgpt.com Codex backend + auth.openai.com, api.x.ai / grok cli-chat-proxy, api.z.ai (Zhipu), and arbitrary OpenAI-compatible/Anthropic-compatible endpoints (llama/custom).
