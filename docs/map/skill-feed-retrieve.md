# skill/, feed/, retrieve/

> Three small, independent "knowledge & nudge" packages that feed the agent and the user. **`skill/`** discovers, loads, installs, security-scans, and proposes SKILL.md instruction files (markdown the autonomous agent reads and follows on demand). **`feed/`** is the proactive action feed: cheap, read-only background scanners over git state, project memory, GitHub (assigned issues / review requests), and a model-driven "suggest" source produce one-keystroke session starters surfaced on the GUI/TUI home and project pages. **`retrieve/`** is per-project on-demand context retrieval (the `retrieve` tool): a persisted, incremental file index ranked by BM25 lexically and fused with cosine similarity (RRF) when an embedder is configured. None of the three depend on each other; each is consumed by the CLI entry files (`main.go`, `build.go`, `daemon.go`, `retrieve_run.go`), the GUI `Bridge`, and the legacy TUI `app`.

## Files

### internal/feed/feed.go

- **Role:** Core feed model — the `Item`/`Feed` types, cache load/save, the `Scan` orchestrator, ranking, dismissals, and the per-kind `Top` selector.
- **Key symbols:**
  - `Item` (struct) — one offered action: Kind (`git`/`github`/`memory`/`suggest`), Title, Detail, Dir, ready-made `Task` prompt, optional URL.
  - `Feed` (struct) — cached scan result (`Items` + `Scanned` time).
  - `CachePath() string` — `~/.eigen/feed.json` location (see dead-code note).
  - `Load() (Feed, bool)` — returns the cached feed and whether it is still fresh (<10min TTL).
  - `Scan(ctx, projectDirs, Suggester) Feed` — runs every scanner in order (git → memory → github → suggest), each failure-isolated and ctx-bounded; ranks, caches, returns.
  - `(Item) Key() string` — content-based sha256 identity (kind+title+dir) for dismissals.
  - `rank([]Item)` / `score(Item) int` — orders items by actionability (review-requested 90 … suggest 35).
  - `Dismiss(Item)` / `loadDismissed()` / `FilterDismissed([]Item)` — 14-day dismissal store at `~/.eigen/feed-dismissed.json`, filtered at render time.
  - `Top(items, limit, perKind) []Item` — caps each kind so one noisy source can't crowd the home page, backfilling overflow.
- **Depends on:** none internal (stdlib only).
- **Used by / entrypoint:** `internal/app/data.go` (`Load`, `FilterDismissed`), `internal/app/home.go` (`Top`, `Dismiss`), `internal/app/app.go` (`Scan`), `internal/gui/feed.go` (`Load`, `Scan`, `Top`, `FilterDismissed`, `Dismiss`, `Item.Key`).

### internal/feed/git.go

- **Role:** Git scanner — probes each project's local git state for actionable loose ends.
- **Key symbols:**
  - `scanGit(dirs) []Item` — emits items for uncommitted files, unpushed commits, and behind-upstream commits (capped at `maxGitItems`=6).
  - `isGitRepo(dir) bool` — `git rev-parse --is-inside-work-tree`.
  - `dirtyFiles(dir) int` — count from `git status --porcelain`.
  - `unpushed(dir) int` / `behind(dir) int` — ahead/behind counts vs `@{u}` via `git rev-list --count` (local refs only, stays offline/fast).
  - `gitIn(dir, args...) (string, error)` — runs a git command in dir with a 3s timeout.
- **Depends on:** none internal.
- **Used by / entrypoint:** `scanGit` called by `Scan` (feed.go); `isGitRepo`, `dirtyFiles`, `gitIn` also reused by `suggest.go`.

### internal/feed/github.go

- **Role:** GitHub scanner — asks the `gh` CLI for review requests and assigned open issues.
- **Key symbols:**
  - `scanGitHub(ctx) []Item` — two `gh search` calls (review-requested PRs, assigned issues); silently no-ops when `gh` is missing/unauthenticated or ctx is done.
  - `ghResult` (struct) — the JSON shape parsed from `gh search` (number/title/url/repository).
  - `ghSearch(parent, what, args, label, taskFmt) []Item` — runs one `gh search` with an 8s timeout and maps results to Items.
  - `ghCommandCount` (atomic.Int64) — counts gh invocations (used by `github_test.go` to assert no-call paths).
- **Depends on:** none internal.
- **Used by / entrypoint:** `scanGitHub` called by `Scan` (feed.go).

### internal/feed/memory.go

- **Role:** Memory scanner — extracts stated intents ("TODO", "still need to", "want to finish") from each project's memory notes and offers them back as session starters.
- **Key symbols:**
  - `intentRe` (regexp) — matches forward-looking intent phrasings in memory bullets.
  - `scanMemory(dirs) []Item` — opens each project's memory store, splits bullets, matches intents, emits offers (capped `maxMemoryItems`=4).
  - `splitBullets(notes) []string` — splits a memory file into top-level `- ` bullets (also reused by suggest.go).
  - `firstSentenceAround(bullet, re) string` — extracts the clause around the regex match for the feed line.
  - `clip(s, n) string` — rune-safe ellipsis truncation (shared helper across the package).
- **Depends on:** `internal/memory` (`memory.Open`, `store.Read`).
- **Used by / entrypoint:** `scanMemory` called by `Scan` (feed.go); `splitBullets`/`clip` reused by suggest.go and github.go.

### internal/feed/suggest.go

- **Role:** Model-driven "suggest" source — a mid-tier model proposes the step *forward* (missing test, PR for a finished branch, next feature) over a bounded local-context snapshot; runs on its own slow cadence (90min TTL) and is failure-isolated to a stale cache.
- **Key symbols:**
  - `Suggester` (func type) — `func(ctx, system, prompt) (string, error)`, injected by the app so feed has no provider dependency (nil disables the source).
  - `suggestCachePath()`, `suggestCache` (struct), `loadSuggestCache()`, `saveSuggestCache()` — persisted suggest state at `~/.eigen/feed-suggest.json` with TTL freshness.
  - `scanSuggest(parent, dirs, Suggester) []Item` — returns the fresh cache or a new model call; falls back to the stale cache on error.
  - `parseSuggestions(out, dirs) []Item` — lenient JSON-array extraction + validation; never trusts hallucinated dirs (`""` roots at CWD).
  - `suggestContext(dirs) string` — builds the bounded snapshot (README intro, branch, working-tree summary, recent commits, memory tail).
  - `readmeIntro(dir)`, `gitLine(dir, args...)`, `gitOut(dir, args...)` — context-gathering helpers.
- **Depends on:** `internal/memory` (`memory.Open`); reuses `isGitRepo`/`dirtyFiles`/`gitIn` (git.go) and `splitBullets`/`clip` (memory.go).
- **Used by / entrypoint:** `scanSuggest` called by `Scan` (feed.go). The `Suggester` is wired in by `main_gui_wails.go:guiSuggester` (GUI) and `internal/app/data.go:suggester` (TUI app).

### internal/retrieve/index.go

- **Role:** Package doc + the persisted per-project file index: chunking, incremental sync, embedding, and the RRF-fused search.
- **Key symbols:**
  - `Chunk` (struct) — one indexed line-window span (Path/Start/End/Text/Vector).
  - `fileMeta` (struct) — mtime+size for incremental re-embedding.
  - `Index` (struct) — a project's index (root, storage dir, embedder model id, chunks, file metas, lazy `bm25`).
  - `Result` (struct) — a retrieval hit (Path/Start/End/Snippet/Score).
  - `Open(root, llm.Embedder) (*Index, error)` — prepares the index under `~/.eigen/index/<hash>/`; emb may be nil (BM25-only, distinct `"bm25"` model tag).
  - `(*Index) Sync(ctx) (int, error)` — enumerates files, re-embeds changed ones, drops deleted/changed chunks; returns count (re)embedded.
  - `(*Index) embedFile(ctx, rel)` — chunks one file, embeds (when configured), appends; degrades to lexical-only on embed failure.
  - `(*Index) Search(ctx, query, k) ([]Result, error)` — fuses BM25 lexical rank + cosine vector rank via RRF; re-validates hits against disk.
  - `fuseRRF(lexRank, vecRank, n)` — Reciprocal Rank Fusion (rrfK=60), stable tiebreak.
  - `(*Index) validate(c Chunk)` — re-reads the chunk's lines from disk, dropping stale hits.
  - `(*Index) listFiles` / `ripgrepFiles` / `walkFiles` — file enumeration (ripgrep gitignore-aware, else bounded walk).
  - `skipDir`, `denied`, `chunkFile`, `looksTextual` — exclusion + chunking + binary-detection helpers.
  - `(*Index) load()` / `save()` / `persisted` — JSON persistence (model change invalidates all vectors).
  - `(*Index) Len() int` — current indexed chunk count.
- **Depends on:** `internal/llm` (`llm.Embedder`, `llm.CosineSim`, `Embedder.Embed`/`ModelID`).
- **Used by / entrypoint:** `retrieve.Open`/`Sync`/`Search`/`Len`/`Index`/`Result` all called from `retrieve_run.go` (the `retrieve` tool runner). Reached via `tool.Retrieve(retrieveRunner(...))` registered in `build.go:203` and `main.go:747`.

### internal/retrieve/bm25.go

- **Role:** Okapi BM25 lexical ranking — the always-available retrieval floor and one RRF input.
- **Key symbols:**
  - `tokenize(text) []string` — code-aware tokenizer: lowercases, splits on non-alphanumerics AND camelCase/snake_case, keeps both sub-tokens and the joined identifier.
  - `splitIdentifier(w) []string` — breaks camelCase/PascalCase into sub-words.
  - `bm25Index` (struct) — per-chunk term frequencies, document frequencies, length stats.
  - `buildBM25(chunks) *bm25Index` — tokenizes every chunk's path+text into the lexical index.
  - `(*bm25Index) score(i, queryTerms) float64` — standard BM25 (k1=1.2, b=0.75) for one chunk.
  - `(*bm25Index) rank(query) []int` — chunk indices ordered by score, zero-score excluded.
- **Depends on:** none internal (uses `Chunk` from index.go, same package).
- **Used by / entrypoint:** `buildBM25`/`rank` called by `(*Index).Search` (index.go); `tokenize`/`splitIdentifier`/`score` are internal to BM25.

### internal/skill/skill.go

- **Role:** Package doc + the core `Set`/`Skill` discovery, frontmatter parsing, fuzzy name resolution, catalog rendering, body loading (with plugin-root expansion), and `Save`.
- **Key symbols:**
  - `Skill` (struct) — discovered skill (Name/Description/Path; body read on demand).
  - `Set` (struct) — ordered, name-keyed, mutex-guarded collection that remembers its source dirs for in-place Rescan.
  - `Discover(dirs...) *Set` — scans `*/SKILL.md` in each dir (first-wins on name).
  - `(*Set) scan()` / `Rescan()` — (re)populate from dirs + explicit `--skill` paths.
  - `isBuiltInCapabilitySkill(name)` — hides legacy `get-oriented` SKILL.md (promoted into the native orientation harness).
  - `(*Set) AddPath(path) ([]string, error)` — registers an explicit `--skill` file/dir; remembered for Rescan.
  - `skillFilesFromPath`, `fileExists` — resolve an explicit path to SKILL.md file(s).
  - `(*Set) List()`/`Len()`/`Names()`/`Get(name)`/`Catalog()` — read accessors; Catalog renders the system-prompt skill list.
  - `normalizeName(s)`, `(*Set) Resolve(hint)`, `resolveLocked(hint)`, `sharesWord`, `overlapAll` — 4-tier fuzzy resolution ladder (exact → normalized → word-containment → fuzzy subsequence) with ambiguity fail-closed.
  - `(*Set) Body(name) (string, error)` — loads a skill's instruction body on demand (resolves the name, strips frontmatter, expands `${EIGEN_PLUGIN_ROOT}`).
  - `expandPluginRoot(body, skillDir)` — substitutes the plugin bundle path from the `.eigen-root` sidecar.
  - `parse`, `parseFrontmatter`, `stripFrontmatter`, `firstSentence` — SKILL.md parsing helpers.
  - `Save(dir, name, desc, body) (string, error)` — writes `dir/<name>/SKILL.md`, refusing to overwrite.
- **Depends on:** `internal/fuzzy` (`fuzzy.Score`).
- **Used by / entrypoint:** `Discover` from `daemon.go`, `remote_session.go`, `main.go`, `internal/gui/skills.go`, `internal/app/data.go`. `Set.*` methods consumed by `internal/tool/skill.go` (the `skill` tool — `Names`/`Body`/`Resolve`), `internal/tui/commands.go`, `internal/app/pages.go`/`inspector.go`, `build.go`/`main.go` (`Catalog`). `Save` is internal to `finishInstall` (install.go) + tests.

### internal/skill/install.go

- **Role:** Installing a skill from a local path or GitHub: read/parse the source, optionally security-scan it, and write it into the skills dir.
- **Key symbols:**
  - `Installed` (struct) — install result (Name/Path/Scan).
  - `InstallOptions` (struct) — Dir, Name override, Scanner, Force, Overwrite.
  - `InstallFromPath(ctx, src, opts) (Installed, error)` — install from a SKILL.md file or directory.
  - `readSkillFromPath(src) (content, name, err)` — loads content + fallback name from disk.
  - `Fetcher` (func type) — injectable URL fetcher (testable GitHub installs).
  - `GitHubRef` (struct) + `ParseGitHubRef(s) (GitHubRef, error)` — parse `owner/repo[/path][@ref]` (with `github.com/`/`gh:`/`https://` prefixes).
  - `(GitHubRef) rawURL(file)` — builds the raw.githubusercontent.com URL.
  - `InstallFromGitHub(ctx, ref, Fetcher, opts) (Installed, error)` — fetch SKILL.md, scan, install.
  - `finishInstall(ctx, content, fallbackName, opts) (Installed, error)` — resolve name → scan (abort on RISKY unless Force) → `Save`.
  - `RiskyError` (struct) + `Error()` — returned when a scan flags the skill and Force is unset.
- **Depends on:** none internal directly (uses sibling `Scanner`, `parseFrontmatter`, `stripFrontmatter`, `Save`).
- **Used by / entrypoint:** `InstallFromPath`/`InstallFromGitHub`/`ParseGitHubRef`/`DefaultFetcher`/`InstallOptions`/`Installed` from `main.go:installSkill`, `internal/app/install.go:installSkillSource`, `internal/plugin/install.go` (`ParseGitHubRef`, `RiskyError`). `Fetcher`/`GitHubRef` types are used as parameters via those call sites.

### internal/skill/fetch.go

- **Role:** The production `Fetcher` — HTTP GET with timeout + byte cap for GitHub SKILL.md installs.
- **Key symbols:**
  - `DefaultFetcher(ctx, url) ([]byte, error)` — 20s-timeout GET, 512KB cap, maps 404 to a clear error.
- **Depends on:** none internal.
- **Used by / entrypoint:** passed as the `Fetcher` to `InstallFromGitHub` from `main.go:1724` and `internal/app/install.go:445`.

### internal/skill/scan.go

- **Role:** Security scan of a skill's content — a small model judges supply-chain / prompt-injection risk before install (fails closed on an unparseable verdict).
- **Key symbols:**
  - `scanPrompt` (const) — the security-reviewer system prompt (flags only exfiltration / destructive / remote-code-exec / security-disabling / prompt-injection content).
  - `ScanResult` (struct) — Safe + Reasons.
  - `Scanner` (interface) — `Scan(ctx, name, content) (ScanResult, error)`.
  - `ProviderScanner` (struct) — Scanner backed by an `llm.Provider` (the same small/cheap model used for titling/dreaming).
  - `(ProviderScanner) Scan(...)` — sends the skill to the model; a scan error is returned (never silently passes).
  - `parseScan(s) ScanResult` — parses the VERDICT/REASONS block; non-SAFE or unparseable → RISKY.
- **Depends on:** `internal/llm` (`llm.Provider`, `llm.Request`, `llm.Message`, `llm.RoleUser`).
- **Used by / entrypoint:** `Scanner`/`ProviderScanner`/`ScanResult` consumed by `finishInstall` (install.go) and constructed at `plugincmd.go`, `main.go`, `internal/tui/plugin_commands.go`, `internal/app/install.go`, `internal/plugin/install.go`.

### internal/skill/propose.go

- **Role:** Skill proposals — dreaming stages a candidate skill under `~/.eigen/skills-proposed` for the user to review; never auto-installs.
- **Key symbols:**
  - `ProposedDir() string` — `~/.eigen/skills-proposed` (see dead-code note).
  - `activeSkillsDir() string` — `~/.eigen/skills`.
  - `safeName(name) error` — filesystem-safe name validation.
  - `Propose(name, description, body) (string, error)` — stages a proposal (skips if a skill of that name is already active).
  - `Proposal` (struct) + `Proposals() []Proposal` — list staged proposals (sorted by name).
  - `Accept(name) (string, error)` — moves a proposal into the active skills dir (cross-device fallback copy).
  - `Reject(name) error` — deletes a staged proposal.
  - `frontmatterDesc(s) string` — pulls the description from SKILL.md frontmatter (best-effort).
- **Depends on:** none internal (uses sibling helpers conceptually but is self-contained).
- **Used by / entrypoint:** `Propose` from `daemon.go:864` and `main.go:1571` (dream pipeline); `Proposals`/`Accept`/`Reject` from `main.go` (CLI) and `internal/gui/skills.go` (GUI). `ProposedDir` is used only internally within this file.

## Cross-links

- **internal/memory** — `feed/memory.go` and `feed/suggest.go` open project memory stores (`memory.Open`, `store.Read`) to mine stated intents and build suggest context.
- **internal/llm** — `retrieve` uses `llm.Embedder`/`llm.CosineSim` for vector search; `skill/scan.go` uses `llm.Provider`/`Request`/`Message` for the security scan. `retrieve_run.go` calls `llm.NewEmbedder`.
- **internal/fuzzy** — `skill/skill.go` resolves loose skill-name hints via `fuzzy.Score` (the same ranker the palette/search use).
- **internal/tool** — `retrieve_run.go` implements `tool.RetrieveRun` (the `retrieve` tool); `internal/tool/skill.go` drives the `skill` tool against a `*skill.Set` (`Names`/`Body`/`Resolve`).
- **internal/plugin** — `plugin/install.go` reuses `skill.ParseGitHubRef`, `skill.Scanner`, and `skill.RiskyError` to install plugin-bundled skills/commands.
- **internal/gui (Bridge) + internal/app + internal/tui** — consume `feed` (home/project feed) and `skill` (discovery, install, propose/accept/reject) for the desktop GUI, legacy app UI, and terminal UI respectively.
- **CLI entry files** — `main.go`, `build.go`, `daemon.go`, `remote_session.go`, `retrieve_run.go`, `main_gui_wails.go`, `plugincmd.go` wire all three packages into the agent runtime, the `retrieve`/`skill` tools, and the dream/install commands.

## Dead-code notes (verified)

- `feed.CachePath()` (feed.go:41) — **exported** but grepped across the whole repo finds callers only inside `feed.go` itself (Load/save). No external or test caller. Not internally dead (it backs the cache path), but it is needlessly exported — could be unexported to `cachePath`. Confidence: medium.
- `skill.ProposedDir()` (propose.go:18) — **exported** but used only inside `propose.go`. No external/test caller. Same situation: over-exported, not internally dead. Confidence: medium.
- `skill.Save()` (skill.go:503) — **exported**; no external package calls it, only `finishInstall` (same package) + tests. Possibly meant as public API; flag as over-exported, not dead. Confidence: low.
- `retrieve.Chunk` (index.go:43) — **exported** type with no external caller, but it is used pervasively inside the package and in `bm25_test.go`, and is part of the persisted JSON shape. **NOT dead.**
- `feed.ghCommandCount` (github.go:18) — looks like an unused package var but is read by `github_test.go` to assert no-gh-call paths. **NOT dead** (test observability hook).
