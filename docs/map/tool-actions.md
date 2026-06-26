# Tools — actions, search, agentic

> This slice is the part of `internal/tool` that goes beyond editing the local
> tree: the **tool contract + registry** itself (`tool.go`, `policy.go`), the
> **read-only search/discovery** tools (`grep`, `symbols`, `retrieve`,
> `search_tools`, `websearch`), the **network** tool (`fetch`), the **agentic /
> meta** tools that drive Eigen's multi-agent and cross-vendor behavior
> (`task`/`task_group`/`task_group_mutating`/`task_status`/`task_promote`,
> `plan`, `review`, `goal_achieved`, `todo`, `memory`, `skill`,
> `generate_image`), and the backgrounded-shell registry (`shells.go`) that the
> bash tools stream into. Almost every file here is a `tool.Definition`
> constructor: a tiny function that returns a name + description + hand-written
> JSON Schema + a `Run` closure. The heavy lifting (constructing agents,
> providers, judges, the cross-vendor planner/reviewer, the retrieval index, the
> image model) lives in `main.go` / `build.go` and is **injected** into these
> constructors as function values, so `internal/tool` stays free of import cycles
> with `internal/agent` and the provider packages. The registry (`Registry`)
> normalizes schemas, supports progressive disclosure of "niche" tools (MCP
> servers), and produces the provider-neutral specs the agent loop sends to each
> model.

## Files

### internal/tool/tool.go
- **Role:** The tool contract (`Definition`, `Result`), the `Registry`, and the progressive-disclosure ("niche" tool) machinery. The package's spine — every other file produces a `Definition` this registry holds.
- **Key symbols:**
  - `Definition` (struct) — one tool: `Name`, `Description`, hand-written `Parameters` JSON Schema, `ReadOnly` (auto-runs in gated mode), `Disabled` (built but omitted, e.g. git-only tools off a non-git root), `Niche`/`Group`/`GroupDesc`/`Capability`/`CapabilityDesc` (progressive-disclosure metadata), and the executors `Run` (text) / `RunRich` (image-capable).
  - `Result` (struct) — a tool's output: `Text` + optional `[]llm.Image` (screenshots/renders threaded into the tool-result message).
  - `Definition.Spec()` — provider-neutral `llm.ToolSpec`.
  - `Definition.Invoke(ctx,args)` — normalizes `Run` vs `RunRich` into one `Result` (`RunRich` wins).
  - `compactJSON(raw)` — strips insignificant whitespace from a schema; never corrupts on error.
  - `Registry` (struct) + `NewRegistry(defs…)` — ordered, name-keyed tool set; validates non-empty unique names + non-nil executor, drops `Disabled`, compacts each schema once at registration.
  - `Registry.Specs()` / `CoreSpecs(unlocked)` — all specs vs only non-niche + unlocked niche specs (the actual list sent to the model under disclosure).
  - `Registry.HasNiche()` — whether disclosure machinery is worth wiring.
  - `NicheGroup` / `NicheCapability` (structs) — Level-0 group summary / Level-1 capability summary for the catalog.
  - `Registry.GroupCatalog(unlocked)` — the system-prompt Level-0 catalog: one line per niche group + loose ungrouped niche tools.
  - `Registry.GroupCapabilities` / `GroupCapabilityTools` / `GroupTools` / `GroupNames` — the Level-1/Level-2 disclosure layers `search_tools` walks.
  - `Registry.MatchNiche(query)` / `MatchNicheInGroup(group,query)` / `matchNiche` — TOKENIZED match over niche tools (name/description/group/capability): whole-query substring OR every meaningful token lands in some field (hyphen/underscore-flattened, crude singular/plural stem). `tokenizeQuery` drops stopwords incl. generic intent verbs (open/get/show/use…) so a paraphrased ask ("open the page", "new tab") resolves instead of looping. Was plain whole-string substring — too strict; the model had to guess one literal phrase.
  - `firstLine(s)` — first line of a description, truncated to 140 chars (catalog formatting).
  - `Registry.Get` / `Definitions()` — lookup / list in registration order.
  - `Registry.Subset(names…)` — a NEW immutable registry with only named tools (used to give parallel `task_group` children a role allowlist sharing the same `Definition`s).
  - `Registry.AllReadOnly()` — whether every tool is read-only (parallel fan-out requires this so children never race the single approval window).
- **Depends on:** `internal/llm` (`ToolSpec`, `Image`). Stdlib only otherwise.
- **Used by / entrypoint:** `Registry` is consumed by `internal/agent/agent.go` (`a.Tools.Specs()`, `CoreSpecs`, `GroupCatalog` at ~agent.go:1284-1288) to build the per-step tool list, and constructed via `NewRegistry(...)` in `build.go:259`/`build.go:316` and `main.go:851`. `Definition.Invoke` is called by the agent dispatch path.

### internal/tool/policy.go
- **Role:** The filesystem fence (`Policy`) plus denied-path helpers shared by every path-taking tool — defense in depth, independent of the agent's approval gate.
- **Key symbols:**
  - `Policy` (struct, `sync.RWMutex` + `roots`) — confines tools to absolute, symlink-resolved roots; safe for the daemon's many-sessions-one-process model.
  - `NewPolicy(roots…)` / `DefaultPolicy()` — build a policy from roots / from cwd.
  - `Policy.Dir()` — primary root (bash cwd + relative-path base).
  - `Policy.Roots()` — copy of allowed roots.
  - `Policy.AddRoot(path)` — the user-invoked `/add-dir` grant (agent can't widen its own sandbox); validates existence + non-denied, dedupes.
  - `Policy.Resolve(path)` — the workhorse: relative paths resolve against the PRIMARY root (not process cwd), then symlink-resolve and check `within` + `deniedReason`; returns a `*DeniedError` on violation.
  - `Policy.within(path)` / `resolveSymlinks(abs)` — prefix check against roots / resolve longest existing ancestor so a symlinked parent can't escape on create.
  - `deniedSegments` / `deniedBasenames` (vars) — sensitive dirs (`.ssh`, `.aws`, `.gnupg`, `.git`) and file globs (`.env*`, `*.pem`, `id_rsa`, …) never traversable/readable.
  - `deniedReason(path)` / `IsDenied(path)` — classify a path against the deny lists.
  - `DenyGlobs()` — ripgrep `-g !…` exclude args so search/listing never enumerate sensitive files (used by grep/symbols/list).
  - `FilterDeniedLines(out, pathOf)` — drop output lines whose extracted path is denied (defense-in-depth behind `DenyGlobs`).
  - `TruncateUTF8(s,max)` — byte-truncate without splitting a rune.
  - `DeniedError` (struct) + `Error()` — the denial error type.
- **Depends on:** stdlib only (`os`, `path/filepath`, `sync`, `unicode/utf8`).
- **Used by / entrypoint:** `*Policy` is injected into nearly every tool constructor (grep/symbols/list/read/write/edit/bash/…); created once in `main.go:604` (`DefaultPolicy`) / `build.go:92`/`build.go:315` (`NewPolicy`) per session. `DenyGlobs`/`FilterDeniedLines`/`TruncateUTF8`/`IsDenied` are called from sibling tool files (`grep.go`, `symbols.go`, `list.go`, fs tools).

### internal/tool/grep.go
- **Role:** The `grep` tool — regex content search over file contents, powered by ripgrep. Read-only.
- **Key symbols:**
  - `Grep(policy) Definition` — validates `pattern`, resolves `path` (default `.`) through the policy, runs ripgrep with `DenyGlobs()`, then post-filters with `FilterDeniedLines`; returns `file:line:match` or `(no matches)`.
- **Depends on:** `Policy` (`policy.go`), `runRipgrep` (`fsutil.go`), `DenyGlobs`/`FilterDeniedLines` (`policy.go`).
- **Used by / entrypoint:** registered as `grep` in `build.go:195`/`build.go:317` and `main.go:747`.

### internal/tool/symbols.go
- **Role:** The `symbols` tool — find where a function/type/class/etc. is *defined*, across languages, via a ripgrep definition-keyword regex. Read-only.
- **Key symbols:**
  - `Symbols(policy) Definition` — builds a multi-language definition pattern (`func|type|def|class|fn|struct|enum|trait|interface|impl|module|package` near the quoted name, or a `name = function|(=>|class` binding), runs ripgrep with deny globs + line filter; returns `file:line:definition`.
- **Depends on:** `Policy`, `runRipgrep` (`fsutil.go`), `DenyGlobs`/`FilterDeniedLines`. Stdlib `regexp`.
- **Used by / entrypoint:** registered as `symbols` in `build.go:196`/`build.go:318` and `main.go:748`.

### internal/tool/retrieve.go
- **Role:** The `retrieve` tool — semantic + lexical (BM25, fused with vectors when an embedder is configured) search over the project index. Read-only. The index/ranking itself is injected.
- **Key symbols:**
  - `RetrieveRun` (func type) — injected by `main`/`buildSession`: `(ctx, query, k) → formatted top-k hits`.
  - `Retrieve(run) Definition` — validates `query`, clamps `k` to [1,20] (default 8), reports "unavailable" when `run` is nil.
- **Depends on:** nothing internal (the index lives behind the injected `RetrieveRun`).
- **Used by / entrypoint:** registered as `retrieve` in `build.go:203` and `main.go:768`; `RetrieveRun` is wired from the per-project retrieval index in `main.go`/`build.go` (`retrieveRunner(...)`).

### internal/tool/searchtools.go
- **Role:** The `search_tools` meta-tool — hierarchical progressive disclosure of niche tools (MCP servers): list groups → capability categories → tool names → unlock full schemas. Read-only.
- **Key symbols:**
  - `SearchTools(reg func() *Registry, unlock func(names []string)) Definition` — `reg` resolves the full registry lazily (built after this tool); `unlock` records which tool names become callable. The `Run` walks the disclosure levels: empty query → `GroupCatalog`; exact group name → `GroupCapabilities` (or `GroupTools` fallback); `<group> <capability>` → **unlocks that capability's whole (small) batch with schemas in ONE call** (was: list names + force another round-trip — the extra hop per tool was a real cost); keyword/tool match → schemas + unlock. ANTI-LOOP: a scoped miss (`<known group> <bad keyword>`) falls back to `groupGuide` (that server's capabilities) instead of dead-ending; a top-level miss that fuzzily names a group (`closestGroup`) guides into it; a true zero-match returns a "try broader words / a capability / a server name" hint, never a bare "no tools match".
  - `renderAndUnlockMatches(query, matches, unlock)` — formats matched tools' full schemas and calls `unlock`; guards a too-broad keyword that matches a whole big server (shows capabilities/names instead of dumping schemas); zero-match returns the broaden-your-query guidance.
  - `groupGuide(r,group,tail)` / `closestGroup(r,q)` — anti-loop fallbacks: render a server's capability menu when a keyword missed inside it, and fuzzily resolve a group the query refers to without naming literally.
  - `uniqueCapabilities(matches)` / `groupToolCount(r,group)` / `splitGroupQuery(r,q)` / `capabilityMatches(c,q)` / `singleGroup(matches)` — disclosure helpers. `capabilityMatches` is now FUZZY (hyphen/underscore/space-insensitive + substring + gist), so "page read" reaches the `page-read` capability.
- **Depends on:** `Registry` + its `Group*`/`MatchNiche*` methods (`tool.go`), `NicheCapability` (`tool.go`), `firstLine` (`tool.go`).
- **Used by / entrypoint:** registered as `search_tools` in `build.go:253` and `main.go:841` (appended after the core defs, only when there are niche tools); `reg`/`unlock` close over the session's full registry (`deps.registryRef()` in `build.go`) + the agent's live unlocked-set.

### internal/tool/fetch.go
- **Role:** The `fetch` tool — a single GET against an http(s) URL returning the (truncated) response body as text. Mutating (network egress → gated).
- **Key symbols:**
  - `Fetch() Definition` — validates scheme (http/https) + host, 30s timeout (`defaultFetchTTL`), reads up to 256 KiB (`maxFetchBytes`) with truncation marker, returns `HTTP <code>` + body (or a non-text-body note when not valid UTF-8).
  - `maxFetchBytes` / `defaultFetchTTL` (consts).
- **Depends on:** stdlib only (`net/http`, `net/url`, `io`, `unicode/utf8`). NOTE in source: no SSRF protection beyond the scheme check — run gated if that matters.
- **Used by / entrypoint:** registered as `fetch` in `build.go:200` and `main.go:759`.

### internal/tool/websearch.go
- **Role:** The `websearch` tool — web search with a keyless fallback chain (works out of the box) and configurable keyed heads. Mutating (network egress → gated). This file holds the tool + the keyed/configured backends (Tavily, Brave API, SearXNG, generic JSON); the keyless engines + chain live in `websearch_engines.go`.
- **Key symbols:**
  - `WebSearch() Definition` — clamps `count` to [1,8] (default 5), runs the `searchChain`, formats results; on total failure suggests setting `BRAVE_API_KEY`/`TAVILY_API_KEY`/`EIGEN_SEARXNG_URL`.
  - `searchResult` (struct) — normalized `{Title,URL,Snippet}`.
  - `buildSearchChain()` — orders engines best-first: configured keyed heads (Tavily/Brave/SearXNG/generic) then the always-present keyless tail (Mojeek → DuckDuckGo → brave-web → Marginalia → Wikipedia), each opt-out via env.
  - `tavilyBackend` / `braveBackend` / `searxngBackend` / `genericBackend` (structs) + their `name`/`class`/`host`/`search` — the keyed/configured backends.
  - `parseGenericResults(raw)` / `firstString(m,keys…)` — lenient JSON-shape extraction for the generic backend.
  - `formatResults(rs)` / `collapseWS(s)` / `queryEscape(s)` / `envOr(key,def)` — output + URL/env helpers.
  - `maxSearchResults` / `websearchTimeout` (consts).
- **Depends on:** the `searchEngine`/`searchChain`/SSRF machinery in `websearch_engines.go`; stdlib `net/http`, `encoding/json`.
- **Used by / entrypoint:** registered as `websearch` in `build.go:206` and `main.go:773`.

### internal/tool/websearch_engines.go
- **Role:** The web-search engine interface, the best-first fallback `searchChain`, the keyless engines (Marginalia, Wikipedia, Mojeek, DuckDuckGo, public brave-web), HTML SERP parsing, URL dedup, and the SSRF host check. Ported natively to Go from `@agent-sh/harness-websearch` v2 (no MCP dependency).
- **Key symbols:**
  - `engineClass` (type) + `classGeneral`/`classNiche`/`classVertical` — coverage labels so an empty result is interpreted correctly (a general-engine empty is authoritative; an encyclopedic-only empty while a general engine errored is "degraded").
  - `searchEngine` (interface) — `name`/`class`/`host`/`search`.
  - `searchChain` (struct) + `run` — tries engines in order, dedups by `normalizeURL`, isolates per-engine failures, fast-paths a sufficient first engine, splits the deadline per engine (`engineCtx`).
  - `normalizeURL(raw)` — dedup key (lowercase scheme/host, drop www/default ports/fragment/tracking params, sort query).
  - `marginaliaEngine` / `wikipediaEngine` / `mojeekEngine` / `duckduckgoEngine` / `braveWebEngine` (structs) + `search` — the keyless engines (JSON APIs + HTML scrapers).
  - `parseMojeek` / `mojeekTitleAnchor` / `mojeekSnippet` / `mojeekChallenged` — Mojeek SERP block parsing + anti-bot detection.
  - `parseDuckDuckGo` / `unwrapDDGRedirect` — DDG lite-HTML parsing + redirect unwrapping.
  - `parseBraveWeb` / `htmlUnescapeBasic` / `braveBrowserUA` — public brave.com scraping (anchors on stable `l1`/`search-snippet-title` tokens; sends a browser UA).
  - `attrValue(tag,attr)` / `stripTags(s)` / `searchHTTPGet(...)` / `hostOf` / `envOr2` / `envTrue` / `getenv` — shared HTML/HTTP/env helpers (`getenv` is a stub indirection for tests).
  - `ssrfCheck(host)` / `classifyIP(addr)` / `ssrfOptedIn(block)` / `ssrfEnvFor(block)` — refuse loopback/LAN/metadata/reserved hosts unless an opt-in env is set; runs before every request.
  - consts: `searchUserAgent`, `searchClientTimeout` (12s overall budget), `maxSearchBodyBytes` (4 MiB).
- **Depends on:** stdlib only (`net`, `net/http`, `net/url`, `encoding/json`).
- **Used by / entrypoint:** consumed by `websearch.go` (`buildSearchChain` wires these engines; `ssrfCheck` is the chain's `checkSSRF`). Not reached directly by a tool registration.

### internal/tool/task.go
- **Role:** The agentic delegation tools — `task` (one sub-agent, foreground or background), `task_status` (inspect/collect background tasks), `task_promote` (turn a background transcript into a resumable session), `task_group` (parallel read-only fan-out), `task_group_mutating` (parallel write fan-out in isolated worktrees, merged behind one approval). All heavy logic is injected to avoid an `agent`→`tool` import cycle.
- **Key symbols:**
  - `TaskOpts` (struct) — one delegation's routing controls (`Kind`/`Difficulty`/`Model`/`Role`); `main` adapts it to `agent.SubtaskOpts`.
  - `GroupSubtaskArg` (struct) — one child of a fan-out as the model supplies it.
  - `TaskRun` / `TaskStatusRun` / `TaskPromoteRun` / `TaskGroupRun` / `TaskGroupMutatingRun` (func types) — the injected backends.
  - `Task(run) Definition` — read-only; foreground returns the final answer, `background=true` returns a task id (jsonl under `~/.eigen/tasks`).
  - `TaskStatus(run) Definition` — read-only; per-id status/result or a listing; `verbose`/`tail` for attempt history + transcript tail.
  - `TaskPromote(run) Definition` — **mutating** (writes a new `~/.eigen/sessions/*.eigen.jsonl`); deliberately separate from read-only `task_status`.
  - `TaskGroup(run) Definition` — read-only fan-out (children are read-only, which is what makes parallelism safe); optional `workers` + `synthesize` merge step.
  - `TaskGroupMutating(run) Definition` — **not** read-only; parallel implementers in isolated git worktrees, diffs merged behind one approval; requires a clean git repo rooted at the repo root.
- **Depends on:** nothing internal directly (everything is behind the injected func types). Stdlib `encoding/json`, `strings`.
- **Used by / entrypoint:** registered as `task`/`task_status`/`task_promote`/`task_group`/`task_group_mutating` in `build.go:201-202` and `main.go:763-767`. The `*Run` values are built in `main.go`/`build.go`, closing over the agent + provider construction and the worktree-merge machinery. NOTE: the doc comment block describing `TaskGroup` sits physically above `TaskPromote` (a cosmetic comment-placement quirk, not a behavior issue).

### internal/tool/plan.go
- **Role:** The `plan` tool — adversarial cross-vendor planning council (author drafts, the OTHER vendor critiques, revise until approved or budget runs out). Read-only (only mutates plan thinking). Logic injected.
- **Key symbols:**
  - `Planner` (func type) — injected `(ctx, task, context) → hardened plan`.
  - `Plan(run) Definition` — validates `task`, reports "not available" when `run` is nil.
- **Depends on:** nothing internal (council/provider construction is behind `Planner`).
- **Used by / entrypoint:** registered as `plan` in `build.go:205` and `main.go:772`.

### internal/tool/review.go
- **Role:** The `review` tool — cross-vendor review (GPT reviews Claude, Claude reviews GPT, never self-review). Read-only. Logic injected.
- **Key symbols:**
  - `Reviewer` (func type) — injected `(ctx, artifact, focus) → critique`.
  - `Review(run) Definition` — validates `artifact`, reports "not available" when `run` is nil.
- **Depends on:** nothing internal (reviewer/provider construction is behind `Reviewer`).
- **Used by / entrypoint:** registered as `review` in `build.go:205` and `main.go:771`.

### internal/tool/goal.go
- **Role:** The `goal_achieved` tool — the model claims the current goal is done; a fresh-context judge model verifies the evidence and only a confirmed verdict clears the goal. Read-only (mutates only goal state). Judge injected.
- **Key symbols:**
  - `GoalJudge` (func type) — injected `(ctx, evidence) → (achieved, reason, err)`.
  - `GoalAchieved(judge) Definition` — validates `evidence`; on confirm returns "CONFIRMED ... cleared", on reject returns the gaps + retry instruction.
- **Depends on:** nothing internal (judge/provider construction is behind `GoalJudge`).
- **Used by / entrypoint:** registered as `goal_achieved` in `build.go:205` and `main.go:770`.

### internal/tool/todo.go
- **Role:** The `todo` tool — the model passes the COMPLETE task list each call (idempotent set, not a delta) for a live checklist. Read-only (no fs side effects).
- **Key symbols:**
  - `Todo() Definition` — validates each item's `content` + `status` + optional `priority`, enforces exactly ≤1 `in_progress`, sorts by priority (high→medium→low→unset, stable within a band), returns a `[x]/[~]/[-]/[ ]` rendered plan with a done count and a `(priority)` suffix on each line.
  - `validTodoStatus` (var) — allowed lifecycle set (`pending`/`in_progress`/`completed`/`cancelled`).
  - `validTodoPriority` (var) — allowed optional priority band (`high`/`medium`/`low`).
  - `todoPriorityRank(priority)` — render-order rank (unset/unknown sorts last).
  - `todoPriorityTag(priority)` — `" (priority)"` suffix, empty when unset.
  - `todoGlyph(status)` — status → plain-text marker.
- **Depends on:** stdlib only (`sort`, `strings`).
- **Used by / entrypoint:** registered as `todo` in `build.go:200`/`build.go:320` and `main.go:760`.

### internal/tool/memory.go
- **Role:** The `memory` tool — record or inspect Eigen's own durable memory (project vs global scope; notes vs hard "ban" rules). Read-only with respect to the *user's* project (writes only to Eigen's memory store), so it auto-runs.
- **Key symbols:**
  - `MemoryStore` (interface) — minimal write view: `Append(note)` + `AddBan(title,rule)`.
  - `memoryLister` / `memoryReader` / `memorySearcher` (interfaces) — optional capabilities discovered by type assertion (`list`/`read`/`search` actions).
  - `Memory(project, global MemoryStore) Definition` — dispatches `add`/`list`/`read`/`search`; `scope=global` routes to the global store when present; `kind=ban` records a titled hard prohibition.
- **Depends on:** `internal/memory` (`memory.SearchHit`).
- **Used by / entrypoint:** registered as `memory` in `build.go:201` and `main.go:762`; satisfied by `*memory.Project`/global store from `internal/memory`.

### internal/tool/skill.go
- **Role:** The `skill` tool — load a skill's full instructions by (loosely matched) name into the conversation. Read-only.
- **Key symbols:**
  - `SkillSet` (interface) — `Body(name)`, `Names()`, `Resolve(hint) → (name, ok)`; satisfied by `*skill.Set` (declared here to avoid an import cycle).
  - `Skill(set) Definition` — loads `Body`; when a fuzzy hint resolved to a different registered name, prefixes a "(loaded skill X for hint Y)" note so a fuzzy resolve is never silent.
- **Depends on:** the `SkillSet` interface (satisfied by `internal/skill`).
- **Used by / entrypoint:** registered as `skill` in `build.go:200` and `main.go:761`.

### internal/tool/imagegen.go
- **Role:** The `generate_image` tool — render image(s) from a text prompt, save PNGs into the project, and return the saved paths AND the images inline (so the model can see what it made). **Mutating** (writes files). The renderer is injected.
- **Key symbols:**
  - `ImageGenRun` (func type) — injected `(ctx, prompt, width, height, count) → (paths, []llm.Image, err)`.
  - `GenerateImage(run) Definition` — the only `RunRich` tool in this slice; validates `prompt`, reports "unavailable" when `run` is nil, returns a `Result` with paths text + inline images.
- **Depends on:** `internal/llm` (`Image`).
- **Used by / entrypoint:** registered as `generate_image` in `build.go:204` and `main.go:769`; `ImageGenRun` is wired from the configured image model (Bedrock by default) in `main.go`/`build.go` (`imageGenRunner(...)`).

### internal/tool/shells.go
- **Role:** The backgrounded-shell state machine + per-session registry. Not a tool itself — it's the support layer the bash tools (`bash`/`bash_output`/`kill_shell`, in `bash.go`/`bashoutput.go`, *not* owned by this slice) stream into, plus the awareness block the agent's system prompt shows.
- **Key symbols:**
  - `ShellInfo` (struct) — lock-safe snapshot for listing (panel / `SessionState`).
  - `Shell` (struct) — one backgrounded command: rolling capped buffer, drop count, status/exit, pgid, incremental read offset; methods `write`/`snapshot`/`readNew`/`setStatus`/`setPgid`/`running`/`finishedAt`/`kill`.
  - `ShellRegistry` (struct) + `NewShellRegistry()` — thread-safe per-session set; `add` (register + evict oldest finished past `maxFinishedShells`), `evictFinishedLocked`, `Infos`, `Get`, `List`, `RunningCount`, `KillByID`, `StatusBlock` (the system-prompt "background shells" awareness block).
  - `lastShellLine(out)` — last non-empty output line (one-line hint).
  - consts: `maxShellBuffer` (256 KiB), `maxFinishedShells` (30).
- **Depends on:** stdlib only (`bytes`, `sync`, `sync/atomic`, `syscall`, `time`).
- **Used by / entrypoint:** consumed by `bash.go`/`bashoutput.go` (which call `add`/`write`/`snapshot`/`readNew`/`kill`/etc.; `truncShellCmd` lives in `bash.go`) and by the TUI/daemon (`Infos`, `RunningCount`, `KillByID`, `StatusBlock`, `ShellInfo`) for the shells panel + `SessionState`. Constructed via `NewShellRegistry()` per session in `main.go:742`/`build.go:192`.

## Cross-links
- **internal/agent** — the agent loop consumes `Registry.Specs()`/`CoreSpecs()`/`GroupCatalog()` to assemble the per-step tool list and dispatches via `Definition.Invoke`; it also feeds the agent + worktree-merge backends behind the injected `Task*`/`Plan`/`Review`/`Goal`/`Retrieve`/`ImageGen` func types.
- **internal/llm** — `Definition.Spec()` → `llm.ToolSpec`; `Result.Images`/`ImageGenRun` carry `llm.Image`.
- **internal/memory** — the `memory` tool's `MemoryStore`/searcher interfaces and `memory.SearchHit`.
- **internal/skill** — the `skill` tool's `SkillSet` interface (satisfied by `*skill.Set`).
- **internal/mcp** — niche/grouped tools surfaced through `search_tools` are the MCP-server tools (workspace/chrome) registered into the same `Registry`.
- **internal/tool (fs/editing slice)** — shares `Policy` (`policy.go`), `runRipgrep` (`fsutil.go`), and the `ShellRegistry` (`shells.go`) with the read/write/edit/bash tools; `truncShellCmd` lives in `bash.go`.
- **main.go / build.go (package main)** — the wiring point: both build the `Registry` via `NewRegistry(...)` and inject every backend func value; `build.go` is the daemon/GUI session builder, `main.go` the TUI/CLI.
- **internal/tui & internal/daemon** — read `ShellInfo`/`StatusBlock`/`Infos` for the shells panel and `SessionState`.
