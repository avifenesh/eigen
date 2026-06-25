# LLM routing, catalog, compaction & council

> This slice is the **provider-neutral brain of `internal/llm`**: the normalized
> chat contract every backend implements (`Provider`/`Request`/`Response`), the
> model **catalog** (capabilities, context windows, provider canonicalization),
> the quality-tier **auto-router** that picks a model per task, **candidate**
> resolution (which models the user actually has credentials for), the **token
> budget + compaction/shedding** machinery that keeps conversations inside a
> model's context window, and the **multi-model collaboration** primitives —
> cross-vendor **review** and the adversarial planning **council**. It also holds
> two non-chat model kinds (text **embeddings** for retrieval, **image
> generation**) and model **auto-discovery**. The concrete wire backends
> (mantle, converse, codex, anthropic, grok, glm, llama, custom) live in sibling
> files of the same package and are *not* owned by this slice; they consume the
> contract and catalog defined here. The primary consumer is the root-package
> `autoRouter` (`/router.go`) and the agent loop (`internal/agent`).

## Files

### internal/llm/llm.go
- **Role:** The core provider contract — the normalized message/request/response
  shapes and the optional capability interfaces every backend implements.
- **Key symbols:**
  - `Role` + `RoleSystem/User/Assistant/Tool` — message author enum.
  - `ToolSpec`, `ToolCall` — provider-neutral tool description / invocation.
  - `Message` — one conversation turn (text, images, reasoning blobs, tool calls/results, `ToolError`).
  - `Image` — raw bytes + IANA media type for vision inputs.
  - `Request` / `Response` — normalized completion in/out; `Usage` accounting.
  - `Usage` + `CacheHitRate()` — token accounting incl. prompt-cache read/write; hit-rate ratio.
  - `Provider` interface — `Name()`, `ModelID()`, `Complete()`; the minimal backend surface.
  - `ChunkKind`/`ChunkText`/`ChunkReasoning`, `StreamChunk`, `StreamSink`, `Streamer` — optional streaming capability.
  - `EffortLevels` (var) — global fallback reasoning-effort ladder.
  - `EffortSetter`, `Searcher`, `FastModer` — optional runtime-toggle capabilities (effort / live-search / fast tier).
  - `ValidEffort(level)` — membership check against global `EffortLevels` (test-only caller — see dead-code note).
  - `ModelEffortLevels(modelID)` — per-model closed effort set, else global, else nil.
  - `effortSupported(level, levels)` (unexported) — membership in a model's accepted set; used by mantle/codex to ignore unhonorable global effort.
- **Depends on:** stdlib only (`context`, `encoding/json`); calls `Lookup` (catalog.go) inside `ModelEffortLevels`.
- **Used by / entrypoint:** Foundational — every other file in the package and every backend (`mantle.go`, `converse.go`, `codex.go`, `anthropic.go`, `grok.go`, `glm.go`, `llama.go`, `custom.go`) plus `internal/agent` build on these types. Capability interfaces are type-asserted in `internal/agent/agent.go` and `internal/chat/local.go`.

### internal/llm/catalog.go
- **Role:** The static model catalog — capabilities, context windows, provider
  defaults, and provider-alias canonicalization.
- **Key symbols:**
  - `ModelInfo` (type) — per-model record: `ID`, `Provider`, `ContextWindow`, `Cache`, `Context1M`/`ContextWindow1M`, `Reasoning`/`Effort`/`EffortLevels`/`ThinkingBudget`, `Search`/`Vision`/`Social`, `ServiceTier`.
  - `Catalog` (var) — the curated built-in model list, spanning all chat backends: mantle GPT (`openai.gpt-5.5/5.4/5`), codex GPT (`gpt-5.5/5.4`), converse Claude (`us.anthropic.claude-opus-4-8` = default, `sonnet-4-6`, `3-5-sonnet`, `haiku-4-5`), native-anthropic Claude (`claude-opus-4-1`, `sonnet-4-5` = native default, `3-5-haiku`), local llama, grok (`grok-build` = default, `composer-2.5-fast`, `grok-4`, `grok-code-fast-1`), and glm (`glm-5.2` = 1M native flagship + default, `glm-5.1`, `glm-5/5-turbo/4.7/4.6/4.5/4.5-air`).
  - `defaultModelByProvider` (var) + `DefaultModel(provider)` — resolve a provider's default model id (falls back to first custom-provider model).
  - `ResolveProvider(provider, model)` — reconcile a (provider,model) pair so a known model goes to its catalog backend (prevents 404s).
  - `CanonicalProvider(p)` / `canonicalProvider(p)` — collapse provider aliases ("claude"→"converse", "xai"→"grok") to a canonical backend.
  - `Models()` — catalog + user custom-provider models, stable order.
  - `Lookup(model)` — catalog entry by exact then prefix match; tag-blind.
  - `HasVision(model)` / `Vision(model)` — image-input support (the latter also reports whether the catalog *knows*: fail-open attach, fail-closed routing).
  - `EffectiveContextWindow(model)` — 1M window when the model+beta allow, else standard window (env `EIGEN_CONVERSE_1M`).
- **Depends on:** `ParseRef` (ref.go); `customProviderByName`/`customModels`/`normalizeCustomModel` + `envBool` (sibling `custom.go`/helpers, not in this slice).
- **Used by / entrypoint:** Heavily reused — `router.go`/`candidates.go`/`budget.go`/`ref.go`/`llm.go` here, every backend's constructor, plus `internal/config`, `internal/tui`, `internal/gui/routing.go`, `internal/app/data.go`, `internal/telegram`, and root `main.go`/`router.go`.

### internal/llm/routerscores.go
- **Role:** The auto-router's scoring table — the user's quality-TIER ladder (not a price search).
- **Key symbols:**
  - `Tier` + `TierSimple/SimpleMed/Med/Frontier` — quality classes 1–4; `TierFrontier` (4) is reserved as the unknown-model fallback target (no catalog model is tier-4 today).
  - `RouterScore` (type) — per-model `Tier`, within-tier `Rank`, `Speed`, `Strict`/`Design` affinity flags (Strict wins general work, Design wins frontend).
  - `routerScores` (var) — model-id → `RouterScore` by user TRUST not benchmarks: most grok/glm = tier-1 "simple only"; sonnet + `glm-5.2` = tier-2 simple-med (glm-5.2 outranks sonnet there); opus + gpt-5.x = tier-3 med (gpt-5.5 `Strict`/highest-rank, opus `Design`).
  - `scoreFor(id)` (unexported) — score for an id, defaulting unknown models to frontier so the router never silently downgrades.
- **Depends on:** none beyond package types.
- **Used by / entrypoint:** `scoreFor` is the scoring primitive for `router.go` (`Route`, `tierOrder`, `affinity`) and `vendor.go` (`CrossReviewer`, `CrossVendorAdversaries`).

### internal/llm/router.go
- **Role:** Pure routing policy — pick the model for a task by the quality-tier ladder + capability + Bedrock-avoidance.
- **Key symbols:**
  - `TaskKind` + `TaskGeneral/Search/Vision/Social` — what capability a task requires.
  - `Difficulty` + `DiffTrivial/Easy/Medium/Hard` — the user's scoping ladder.
  - `targetTier(d)` (unexported) — difficulty → minimum quality tier (hard is *not* tier-mapped; keeps default).
  - `RouteRequest` (type) — task kind, difficulty, min context, `Frontend` flag, `Candidates`.
  - `Route(req)` — the policy: filter capable candidates, pick lowest tier ≥ target (else highest present), order by affinity→rank→non-Bedrock→speed; returns chosen id + ok.
  - `tierOrder`/`affinity` (unexported) — within-tier ordering and task-flavor fit.
  - `isCapable(id, req)` (unexported) — required search/vision/social flags + context window check.
  - `isBedrock(id)` (unexported) — true for mantle/converse (employer-paid) so the router prefers other accounts in a tie.
- **Depends on:** `Lookup`/`EffectiveContextWindow` (catalog.go), `scoreFor` (routerscores.go), `canonicalProvider` (catalog.go); stdlib `sort`.
- **Used by / entrypoint:** `llm.Route` is called from the root-package `autoRouter` (`/router.go`: subtask routing + assessor-model pick); `RouteRequest`/`TaskKind`/`Difficulty` constants are used across `/router.go` and its tests.

### internal/llm/candidates.go
- **Role:** Credential-aware candidate sets — which catalog models the user can actually construct.
- **Key symbols:**
  - `ProviderAvailable(provider)` — cheap (no-network) credential probe per backend (codex auth.json, Bedrock bearer, converse SigV4, anthropic key/OAuth, grok key/CLI, glm key, llama URL, custom).
  - `converseAvailable()` (unexported) — Bedrock bearer or resolvable SigV4 creds for the converse profile.
  - `RouteCandidates(currentProvider, allowed)` — catalog ids whose provider is available AND allowlisted (current always included; cross-provider routing is opt-in).
  - `AllCredentialedModels()` — every credentialed model ignoring the route allowlist (for cross-vendor review/council where capability beats policy).
- **Depends on:** `canonicalProvider`/`Models` (catalog.go); credential helpers from sibling backends (`codexAuthPath`/`readCodexAuth`, `loadAWSCreds`, `claudeOAuthToken`/`claudeCredentialsPath`, `grokCLIToken`, `customProviderByName`/`customProviderAvailable`, `firstNonEmpty`); stdlib `os`.
- **Used by / entrypoint:** `/router.go` (candidate sets for routing/review/council), root `main.go`, `build.go`, `internal/gui/routing.go`, `internal/app/data.go`, `main_gui_wails.go`.

### internal/llm/budget.go
- **Role:** Compute the conversation token budget for a model.
- **Key symbols:**
  - `budgetHeadroomPct` (const, 85) — fraction of the window usable as conversation budget.
  - `ContextBudget(userMax, model, providerDefault)` — min(userMax, window−headroom); the model window always caps a too-large user setting.
- **Depends on:** `EffectiveContextWindow` (catalog.go).
- **Used by / entrypoint:** root `main.go` (`contextBudget`), `internal/tui/switches.go`.

### internal/llm/classify.go
- **Role:** Legacy deterministic prompt classifier (kind + difficulty) — retained for tests/cheap defaults; production routing assesses unstated subtasks with a small model instead.
- **Key symbols:**
  - `Classify(prompt, hasImage)` — kind + difficulty via wording heuristics (TEST-ONLY caller — see dead-code note).
  - `IsFrontend(prompt)` — frontend/design cue match (TEST-ONLY caller — see dead-code note).
  - `frontendCues`/`socialCues`/`searchCues`/`hardCues`/`trivialCues` (vars) — cue word lists.
  - `classifyKind`/`needsSocial`/`needsSearch`/`classifyDifficulty` (unexported) — the heuristics.
  - `ParseTaskKind(s)` / `ParseDifficulty(s)` — map orchestrator-supplied strings to enums (with "was-explicit" bool), safe fallback on bad/empty input.
- **Depends on:** stdlib `strings`; the `TaskKind`/`Difficulty` enums (router.go).
- **Used by / entrypoint:** `ParseTaskKind`/`ParseDifficulty` are called from `/router.go` (task-tool arg parsing). `Classify`/`IsFrontend` are only referenced by `classify_test.go`.

### internal/llm/compact.go
- **Role:** Token estimation + the deterministic recency-window compactor (last-resort, model-free).
- **Key symbols:**
  - `EstimateTokens(msgs)` — rough ~chars/4 + per-image flat estimate token count.
  - `Compact(msgs, maxTokens)` — keep the largest suffix of whole user-led rounds that fits; note dropped history on the first retained user message.
- **Depends on:** stdlib `fmt`.
- **Used by / entrypoint:** `EstimateTokens` is used widely (`internal/agent/agent.go`, `internal/agent/background.go`, here). `Compact` is the terminal fallback inside `compactFit` (compact_summary.go).

### internal/llm/compact_summary.go
- **Role:** Model-assisted compaction — microcompaction (shed) first, then a single LLM summary that replaces older history.
- **Key symbols:**
  - `Compactor` interface — `Summarize(ctx, msgs)`; testable without a live model.
  - `summaryPrompt`/`summaryReinjectPrefix` (consts) — the structured-summary instruction + Codex-style handoff framing.
  - `shedKeepRounds`/`shedKeepToolResults` (consts) — microcompaction retention knobs.
  - `CompactWith(ctx, c, msgs, maxTokens)` — the orchestrator: shed tool results, then count-shed, then summarize older + keep recent verbatim; degrades to `compactFit` on any failure.
  - `compactFit` (unexported) — progressive count-shed then deterministic `Compact` fallback.
  - `userStarts`/`firstUserText` (unexported) — round boundaries / original-task extraction.
  - `providerCompactor` + `NewCompactor(p)` — adapt a `Provider` into a `Compactor`.
  - `CompactorChain(cs...)` / `chainCompactor` — try compactors in order (cheap small model first, main provider fallback).
- **Depends on:** `ShedToolResults`/`ShedOldToolResults` (shed.go), `EstimateTokens`/`Compact` (compact.go), `Provider`/`Request`/`Message` (llm.go); stdlib `context`/`fmt`/`strings`.
- **Used by / entrypoint:** `CompactWith` is called from `internal/agent/agent.go` and `internal/agent/background.go`. `NewCompactor`/`CompactorChain` are wired in `build.go`, `daemon.go`, root `main.go`, `internal/tui/switches.go`, `internal/agent/{group,groupmut,agent}.go`.

### internal/llm/shed.go
- **Role:** Microcompaction primitives — stub/drop tool-result payloads and old screenshots while keeping call/result pairing valid.
- **Key symbols:**
  - `maxRetainedToolImages` (const) + `imagePrunedStub` (const) + `ShedToolImages(msgs)` — keep only the freshest N tool-result images, stub the rest.
  - `ShedOldToolResults(msgs, keepResults)` — count-based result stubbing for a single long turn.
  - `toolResultStub`/`duplicateResultStub`/`dedupeMinChars` (consts) — stub texts + dedupe threshold.
  - `DedupeToolResults(msgs, last)` — stub older exact-duplicate tool outputs in place (e.g. same file re-read), keep newest.
  - `ShedToolResults(msgs, keepRounds)` — round-based result stubbing outside the recent window (portable `clear_tool_uses`).
- **Depends on:** `userStarts` (compact_summary.go), `Message`/`RoleTool` (llm.go).
- **Used by / entrypoint:** `ShedToolResults`/`ShedOldToolResults` are internal (called by compact_summary.go). `ShedToolImages` and `DedupeToolResults` are called from `internal/agent/agent.go`.

### internal/llm/overflow.go
- **Role:** Detect a context-window-overflow error (so the agent shrinks-and-retries instead of waiting like a rate limit).
- **Key symbols:**
  - `IsContextOverflow(err)` — substring match for 413 / "prompt too long" / "context length" etc., excluding rate-limit phrasing.
- **Depends on:** stdlib `strings`.
- **Used by / entrypoint:** `internal/agent/agent.go` (overflow-retry path) and `internal/agent/background.go`.

### internal/llm/ref.go
- **Role:** Model ref parsing — one user-facing field that can name both backend and model (`provider:model`).
- **Key symbols:**
  - `ParseRef(s)` — split an optional `provider:model` tag (only when the prefix is a known provider; a model id with its own colon like `…-v1:0` is never mis-split).
  - `knownProvider(name)` (unexported) — recognized provider/alias (incl. custom).
  - `Ref(provider, model)` — render the one-field form: bare id when the catalog self-tags it, else `provider:model`.
- **Depends on:** `canonicalProvider`/`Lookup` (catalog.go), `customProviderByName` (custom.go).
- **Used by / entrypoint:** `ParseRef` is used by `provider.go`, `catalog.go`, root `main.go`, `internal/config`, `internal/tui`. `Ref` is used by `internal/config/config.go`.

### internal/llm/provider.go
- **Role:** The provider factory — construct a `Provider` from a (provider, model) pair or a ref.
- **Key symbols:**
  - `New(provider, model)` — strip stale `Name()` suffixes, honor an explicit ref tag (forces backend), else `ResolveProvider`; dispatch to the right backend constructor (`NewMantle`/`NewLlama`/`NewConverse`/`NewAnthropic`/`NewCodex`/`NewGrok`/`NewGLM`/`newCustomProvider`).
- **Depends on:** `ParseRef`/`ResolveProvider`/`canonicalProvider` (ref.go/catalog.go) and every backend constructor (sibling files, not in this slice); stdlib `strings`.
- **Used by / entrypoint:** The single construction entrypoint — called from root `main.go`/`router.go`, `internal/agent`, `internal/tui`, `internal/chat`, `internal/telegram`, etc.

### internal/llm/review.go
- **Role:** One-shot cross-vendor artifact review (one model critiques another's work).
- **Key symbols:**
  - `reviewPrompt` (const) — the critical-reviewer instruction template.
  - `ReviewArtifact(ctx, reviewer, reviewerID, authorID, artifact, focus)` — run the reviewing model and return its critique.
  - `authorVendorLabel(authorID)` (unexported) — human vendor label for the prompt framing.
- **Depends on:** `VendorOf`/`Vendor*` (vendor.go), `Provider`/`Request` (llm.go); stdlib `context`/`fmt`.
- **Used by / entrypoint:** `ReviewArtifact` is called from `/router.go` (the review tool path). `authorVendorLabel` is shared with council.go.

### internal/llm/council.go
- **Role:** Adversarial cross-vendor planning council — one model authors a plan, another vendor adversarially critiques, author revises, repeat until APPROVE or round cap.
- **Key symbols:**
  - `CouncilConfig`/`AdversaryOption`/`CouncilTurn`/`CouncilResult` (types) — council setup, fallback adversaries, transcript turn, outcome.
  - `councilAuthorDraft`/`councilCritique`/`councilRevise` (consts) — the three prompt templates.
  - `Council(ctx, cfg, task, taskContext)` — the loop: author draft → pick first working adversary → critique/revise rounds → result.
  - `complete(ctx, timeout, p, system, user)` (unexported) — one bounded single-shot completion.
  - `verdictApprove`/`lastVerdict`/`stripVerdict` (unexported) — parse the trailing `VERDICT:` line.
  - `FormatCouncil(res)` — render the result (provenance header + hardened plan + standing dissent).
- **Depends on:** `VendorOf` (vendor.go), `authorVendorLabel` (review.go), `Provider`/`Request` (llm.go); stdlib `context`/`fmt`/`strings`/`time`.
- **Used by / entrypoint:** `Council` and `FormatCouncil` are called from `/router.go` (the council/plan tool path); adversary lists come from `CrossVendorAdversaries` (vendor.go).

### internal/llm/discover.go
- **Role:** Read-only model auto-discovery — probe each credentialed provider's listing endpoint and report models the catalog doesn't know.
- **Key symbols:**
  - `Discovered` (type) — one provider's `Known`/`New`/`Err` split.
  - `Discover(ctx)` — probe anthropic/converse/grok/glm/llama listings, classify each id against `Lookup`.
  - `errSkip` + `isSkippable` (unexported) — "not configured here" sentinel (no creds / connection refused).
  - `discoverClient` (var) + `listJSON`/`readAll`/`truncErr` (unexported) — lenient JSON listing fetch/decode.
  - `listAnthropicModels`/`listBedrockModels`/`listGrokModels`/`listGLMModels`/`listLlamaModels` (unexported) — per-provider listing calls.
- **Depends on:** `Lookup` (catalog.go), credential/sign helpers from sibling backends (`claudeOAuthToken`/`claudeCredentialsPath`, `loadAWSCreds`/`signV4`, `firstNonEmpty`, `anthropicVersion`/`anthropicBetas`, `grokDefaultBaseURL`/`glmDefaultBaseURL`); stdlib `net/http`/`encoding/json`.
- **Used by / entrypoint:** `Discover` is called from root `main.go` (the model-discovery command).

### internal/llm/embed.go
- **Role:** Text embeddings — the non-chat model kind behind semantic retrieval (OpenAI-compatible `/v1/embeddings`, default local BGE service).
- **Key symbols:**
  - `Embedder` interface — `Embed(ctx, texts)`, `Dims()`, `ModelID()`.
  - `httpEmbedder` (type) — the OpenAI-dialect client.
  - `NewEmbedder()` — build from `EIGEN_EMBED_*` env (or (nil,false) when unset; retrieval is optional).
  - `firstNonEmptyEnv`, `maxEmbedBatch` (const), `embedReq`/`embedResp` (types), `embedBatch` (unexported) — batching + wire shapes.
  - `CosineSim(a, b)` + `sqrt32` (unexported) — cosine similarity for vector ranking.
- **Depends on:** `os`/`net/http`/`encoding/json`/`math`; `firstNonEmpty` (sibling helper) only via `firstNonEmptyEnv` wrapper (self-contained otherwise).
- **Used by / entrypoint:** `NewEmbedder` is used by `retrieve_run.go` (root); `CosineSim` by `internal/retrieve/index.go`.

### internal/llm/version.go
- **Role:** Build identity — the package version plus a git-stamped full version string shared by daemon, CLI, GUI, and TUI.
- **Key symbols:**
  - `FullVersion()` — base `Version` annotated with the build's short git revision and a `-dirty` marker for uncommitted builds (e.g. `0.1.0+7c6737f-dirty`); falls back to bare `Version` when no VCS stamp is embedded (`go run`). Computed once via `sync.Once` from `debug.BuildInfo` — the same source the daemon uses, so all surfaces report the same string for a given binary.
- **Depends on:** stdlib `runtime/debug`, `sync`; the package `Version` const (defined in sibling `http.go`, not in this slice).
- **Used by / entrypoint:** `FullVersion` is called from root `main.go`, `internal/gui/bridge.go`, and `internal/daemon/host.go` (and re-exported through `http.go` for the HTTP user-agent).

### internal/llm/imagegen.go
- **Role:** Generative image model — Bedrock InvokeModel (Stability / Nova Canvas / Titan dialects), shares SigV4 + AWS-profile auth with Converse.
- **Key symbols:**
  - `ImageGenerator` interface — `Generate(ctx, prompt, opts)`, `ModelID()`.
  - `ImageOpts` (type) — width/height/count/seed controls.
  - `bedrockImager` (type) + `NewImageGenerator()` — build from `EIGEN_IMAGE_*` env (Bedrock default).
  - `novaImageRequest`/`novaImageResponse` (types) — Nova/Titan payload + result.
  - `Generate` — dialect-by-prefix payload build, loop for Stability single-image-per-call, base64-decode results.
  - `aspectRatio(w, h)` (unexported) — pixel dims → Stability ratio enum.
  - `invoke(ctx, body)` (unexported) — signed InvokeModel POST + decode.
- **Depends on:** sibling helpers `loadAWSCreds`/`signV4`/`httpJSON`/`urlPathEscape`/`firstNonEmpty`; stdlib `net/http`/`encoding/json`/`encoding/base64`.
- **Used by / entrypoint:** `NewImageGenerator` is called from `imagegen_run.go` (root).

## Cross-links

- **`internal/llm` backends (sibling, not in this slice):** `mantle.go`, `converse.go`, `codex.go`, `anthropic.go`, `grok.go`, `glm.go`, `llama.go`, `openaichat.go`, `custom.go`, plus shared infra `http.go`, `sigv4.go`, `vendor.go`. They implement `Provider`/`Streamer`/`EffortSetter` and supply the credential/sign/listing helpers this slice calls (`loadAWSCreds`, `signV4`, `httpJSON`, `claudeOAuthToken`, `grokCLIToken`, `customProviderByName`, `firstNonEmpty`, `envBool`, the `*DefaultBaseURL`/`anthropic*` constants). `vendor.go` (`VendorOf`, `CrossReviewer`, `CrossVendorAdversaries`) is the cross-vendor selection logic that `review.go`/`council.go` build on.
- **Root package (`/router.go`, `/main.go`, `/build.go`, `/daemon.go`, `/retrieve_run.go`, `/imagegen_run.go`, `main_gui_wails.go`):** the `autoRouter` is the prime consumer — calls `Route`, `RouteCandidates`, `AllCredentialedModels`, `ReviewArtifact`, `Council`/`FormatCouncil`, `CrossReviewer`/`CrossVendorAdversaries`, `ParseTaskKind`/`ParseDifficulty`; `main.go` wires `ContextBudget`, `Discover`, `DefaultModel`, `Models`, compactor chains, and provider construction via `New`.
- **`internal/agent`:** the agent loop calls `CompactWith`, `EstimateTokens`, `ShedToolImages`, `DedupeToolResults`, `IsContextOverflow`, `ModelEffortLevels`, `NewCompactor`, and type-asserts the `Streamer`/`EffortSetter`/`FastModer` capability interfaces.
- **`internal/tui` & `internal/chat`:** model picker / switches use `Models`, `Lookup`, `ResolveProvider`, `ParseRef`, `EffectiveContextWindow`, `ContextBudget`, `ModelEffortLevels`, `EffortLevels`, `Vision`/`HasVision`; `internal/chat/local.go` type-asserts the `EffortSetter`/`Searcher`/`FastModer` capabilities (and takes a `Compactor` on `SetModel`).
- **`internal/gui` & `internal/app`:** GUI routing/config and the desktop data layer call `CanonicalProvider`, `ResolveProvider`, `ProviderAvailable`, `Models`, `DefaultModel`, `New`, `Discover`, `FullVersion`, and the chat DTO types (`Message`/`Role*`/`ToolCall`/`Image`/`Request`/`Response`); `internal/app/data.go` also uses the custom-provider helpers (`CustomProvider`/`CustomModel`/`LoadCustomProviders`/`UpsertCustomProvider`, sibling `custom.go`).
- **`internal/daemon`:** the session layer carries the chat DTOs (`Message`/`Role*`/`Request`/`Response`/`Image`/`ToolCall`), type-asserts the `EffortSetter`/`Searcher`/`FastModer` runtime toggles on the live provider, passes a `Compactor`, and reports build identity via `FullVersion`.
- **`internal/config`:** persists/loads model refs via `ParseRef`, `Ref`, `Lookup`.
- **`internal/retrieve`:** semantic index uses `CosineSim` (and an `Embedder` built via `NewEmbedder`).
- **`internal/telegram`:** lists models via `Models`.
