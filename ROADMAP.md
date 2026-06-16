# eigen roadmap

eigen is a Go terminal coding agent. The real app is `eigen daemon` (a long-lived
host of many chat sessions over `~/.eigen/daemon.sock`); windows are thin views
(`eigen attach`). The user is the END user — polish/ergonomics are first-class.

This file is the **forward plan**: what's open, in priority order. Completed work
is a terse ledger in `## Shipped` (the long writeups live in git history +
project memory). Detailed design/perf notes live under `docs/`.

---

## Now / Next / Later

**Now (in flight):**
- Nothing in flight — pick the next item.

**Next (queued, well-scoped):**
- **Tier 23 — performance + resource health.** RSS/leak soak, bound growth,
  turn-latency profile, a `make perf` guard, `docs/performance.md`.
- **Tier 27 — plugins / marketplaces.** Install bundled plugins (skills + MCP +
  commands + hooks) from a catalog repo (what Claude/Codex call a marketplace);
  `eigen marketplace add/list` + `eigen plugin install/list/remove/enable`. Builds
  on the existing skill-from-GitHub installer + per-scope `plugins.json`/`mcp.json`.

**Later (parked big bets — pull when wanted):**
- **Tier 20 — eigen in your pocket.** Outbound notify + remote approve with no
  port/Tailscale (channel choice undecided). Distinct from Tier 19's ssh path.
- **Tier 7 leftovers.** Background-task escalation (auto re-run/hand-off a
  failed/stalled/underpowered bg task to a bigger model, merge via `task_status`);
  plus the unbuilt §7 ideas (#13 ultraplan, #23 non-LLM heads, #24 adversarial
  GPT×Claude planning).

**Deferred (decided, not bugs)** — see each shipped tier for the `[~]` items and
their rationale (e.g. Tier 19 network listener = ssh -L is safer; Tier 18 #5
non-LLM models = out of scope; Tier 11 scrollback/reorder polish; etc.).

---

## Open work (detail)

### Tier 23 — performance + resource health (RSS, leaks, profiling)
The daemon is long-lived and hosts many sessions; nothing has profiled steady-
state RSS, goroutine growth, or per-turn allocs.
- [ ] **Baseline + visibility.** Capture daemon RSS/goroutines/heap at rest and
  under load; expose a cheap `eigen daemon status` resource line (or a debug op).
- [ ] **Leak hunt.** Long soak: N sessions, many turns, attach/detach churn,
  bg tasks, MCP/LSP spin-up/down — watch RSS/goroutines/fds for monotonic growth.
- [ ] **Bound what grows.** Confirm/cap the unbounded-over-time structures
  (event replay buffers, per-session history, task records, caches).
- [ ] **Turn latency + allocs.** Profile a turn's hot path (wire encode/decode,
  render, diff/markdown, width math); cut obvious allocs.
- [ ] **CI/regression guard.** A `make perf` (or tagged test) that runs the
  soak/profile and flags regressions.
- [ ] **Document findings** in `docs/performance.md` (baselines, caps, knobs).

### Tier 21 — TUI ergonomics (remaining)
- [x] **Right-panel notepad tab.** SHIPPED: a freeform per-session scratch pad
  tab (internal/tui/notepad.go) — type/edit notes, persisted to
  ~/.eigen/notepad[-instance]/<sessionID>.md (survives detach + daemon restart),
  autosaved; focus contract mirrors the terminal tab (ctrl+g releases).
- [x] **Default steer-vs-queue choice (config).** SHIPPED: `config.input_mode`
  (`steer`/`queue`, default steer) + a clickable `input=` sidebar/status segment,
  `alt+q`, `/steer`//`/queue`, palette. steer injects mid-turn (between tool
  rounds); queue holds for the next turn. A steer that lands as the turn ends is
  consumed by one more round (not stranded).
- [x] **Home page density.** DONE via the design work: "working now" section +
  live count in the banner, panel gutters, full-width rules, filled inspector.

### Tier 20 — eigen in your pocket (outbound notify + remote approve)
A way to be pinged + approve from a phone with NO inbound port and NO Tailscale
(distinct from Tier 19's ssh path, which needs a reachable box). All captured
from a cross-vendor review; channel undecided.
- [ ] **Pick the channel** (build-time): e.g. a relay the daemon polls, or a
  push provider — outbound-only, no listener.
- [ ] **Push "needs you"**: an approval / long-turn / error / done event fans out
  to the channel.
- [ ] **Answer approve/deny remotely**: a tap routes back through the channel to
  the daemon's approval queue (the same gated path).
- [ ] **Read status + recent**: "what's running?" / "what did <session> do?".
- [ ] **Security/constraints** (from the review): outbound-only, fail-closed,
  signed/short-lived tokens, approvals stay strictly gated, audit-logged.

### Tier 7 leftovers (big bets, unordered)
- [ ] **Background-task escalation.** If a bg task fails/stalls/declares itself
  underpowered, auto re-run or hand off to a bigger model (not necessarily the
  orchestrator) and merge the final report back via `task_status`.
- Unbuilt §7 dreams (no commitment): #13 ultraplan, #23 non-LLM heads.

- **Tier 29 — adversarial cross-vendor planning (#24).** ✅ `plan` tool +
  `eigen plan <task>`: the active model AUTHORS a step-by-step plan, a model from
  the OTHER vendor adversarially CRITIQUES it (cite-the-flaw, VERDICT:
  APPROVE|REVISE), the author REVISES, repeat until APPROVE or the round budget
  (default 3). internal/llm/council.go (Council/FormatCouncil) on the existing
  cross-vendor plumbing (VendorOf/CrossReviewer/providerFor). Ordered adversary
  FALLBACK across vendors (CrossVendorAdversaries) + 45s per-adversary timeout so
  a hanging/flaky primary (gpt-5.5) falls through to grok/glm instead of blocking;
  author calls unbounded. EIGEN_PLAN_ADVERSARY pins a specific adversary.
  Live-verified: opus authored + grok-4 critiqued + opus revised → 15KB hardened
  plan with surfaced dissent. Cost: opus-max author ~100s/call (deliberate, not a
  hang).

### Tier 27 — plugins / marketplaces (bundled extensions from a catalog repo)
eigen already installs a SKILL from GitHub (`eigen skill add owner/repo[/sub][@ref]`,
security-scanned) and loads per-scope `plugins.json`/`mcp.json`/`lsp.json`/`hooks.json`.
What it lacks is the layer above: a **plugin** = a bundle of components (skills +
an MCP server + slash commands/prompts + hooks), and a **marketplace** = a catalog
repo listing many plugins (what Claude/Codex ship). Reverse-engineer their on-disk
shape first (Claude: a repo with `.claude-plugin/marketplace.json` cataloguing
plugins, each with `.claude-plugin/plugin.json` + `skills/`,`commands/`,`hooks/`,
`.mcp.json`; tracked in `known_marketplaces.json` + `installed_plugins.json`) and
build ON it — read their format directly so the user's existing marketplaces work.
- [ ] **Marketplace registry.** `eigen marketplace add <owner/repo|url>` clones/fetches
  the catalog repo, parses its manifest, records it in `~/.eigen/marketplaces.json`;
  `marketplace list` / `marketplace remove` / `marketplace update`.
- [ ] **Plugin install.** `eigen plugin install <name>[@marketplace]` fetches the
  plugin bundle, security-scans it (reuse the skill scanner), and wires its
  components into the right per-scope configs: skills → `skills/`, MCP server →
  `mcp.json` (niche, auto-described), commands/prompts → a prompts dir, hooks →
  `hooks.json`. `plugin list` / `remove` / `enable|disable` (reuse the existing
  disable-marker mechanism). Record installs in `~/.eigen/plugins-installed.json`.
- [ ] **Read existing Claude/Codex marketplaces.** Parse `.claude-plugin/*.json`
  (and the Codex equivalent) so a user's already-installed marketplaces are
  usable in eigen without re-authoring — import, don't reinvent.
- [ ] **App page + commands.** Extend the read-only `[plugins]` page into a
  browse/install/enable surface; `/plugin` + `/marketplace` slash commands;
  `search_tools`-style disclosure for plugin-provided tools.
- [ ] **Safety.** Untrusted code: scan before install (RISKY → blocked unless
  `--force`), MCP servers stay niche + gated, hooks/commands are opt-in, nothing
  auto-runs on install. The agent CANNOT install plugins — user action only
  (same rule as `/add-dir`).
- [~] **Authoring/publishing** a marketplace from eigen = later; v1 is consume +
  manage, mirroring how `eigen skill add` consumes without authoring.

---

## Shipped (terse ledger — full writeups in git history + project memory)

- **Tier 1 — core capabilities.** Agent loop, tools, sessions, perm gating, MCP/LSP.
- **Tier 2 — tools + catalog.** read/list/glob/grep/symbols/tree/diff/write/edit/
  multiedit/apply_patch/move/bash/fetch/todo/skill/memory/task/goal_achieved.
  (`think` tool skipped — redundant with streamed reasoning.)
- **Tier 3 — plugins + extension.** `plugins.json` external-command tools; MCP servers.
- **Tier 4 — dreaming + learning.** Idle reflection → durable memory; consolidation.
- **Tier 28 — memory v2 (codex-style structured memory).** ✅ Reverse-engineered
  codex's `~/.codex/memories` pipeline + claude's banthis, ported natively
  (docs/memory-system.md, S1–S7). Per scope `~/.eigen/memory/<key>/`: tiers
  `raw/`(per-session rollout summaries, never injected) → `MEMORY.md`(curated)
  → `SUMMARY.md`(the ONLY injected tier — fixes prompt bloat) + `bans.md`.
  `index.sqlite` (pure-Go modernc) = leased job queue + usage/forgetting; the
  whole memory dir is git-versioned. dream.Stage1 = structured rollout summary
  (outcome / verbatim-quote→rule preferences / key / failures-&-do-differently /
  reusable); dream.Consolidate + dream.Summarize drive the tiers; pipeline runs
  via `eigen dream` AND a daemon nightly dreamer (idle-gated, machine-wide, small
  model). banthis NATIVE: bans.md hard prohibitions via `/ban`//`/unban` + the
  memory tool's `kind=ban`, injected as system-priority. Self-improvement:
  recurring friction → PROPOSED subskills (`eigen skill proposed|accept|reject`,
  never auto-installed). Global profile: cross-project working-style distilled
  into global scope. Migration: pre-v2 flat `<key>.md` → `<key>/MEMORY.md`
  non-destructively. Lesson baked in: a flaky-small-model "skip" is NOT a sticky
  watermark. All live-verified.
- **Tier 5 — TUI/UX polish.** Steer+queue, mouse, autocomplete, rich tool blocks, diffs.
- **Tier 6 — agent quality.** Compaction, failover chain, routing, effort levels, usage.
- **Tier 8 — eigen the app.** Daemon/view architecture; the app shell (home/projects/
  sessions/config/skills/models/providers/memory/crons/plugins); proactive feed.
- **Tier 9 — the chat IS the app.** Clickable chrome around the transcript: named-rect
  layout + hitTest + one action registry; status bar, header, rail, right panel,
  command palette — keyboard-first AND mouse.
- **Tier 10 — app shell clickable + structural.** Mouse parity + framed panels +
  layout rects + per-page clickAt.
- **Tier 11 — superapp panels.** Closable/tabbed right panel [changes][git][term]
  [tasks] (term = real PTY); resizable + persisted widths; inline diffs.
  ([~] later: terminal scrollback/copy/resource caps; rail per-header cursor + drag-reorder.)
- **Tier 11.5 — chrome consolidation.** Headerless left command sidebar IS the design
  (≥80 cols); classic header+status only as the <80col fallback.
- **Tier 12 — subagent observability.** `[tasks]` panel + cross-process cancel via
  marker file + durable task store. ([~] open child transcript viewer; promote-to-session.)
- **Tier 13 — session-list ergonomics.** `LastAttached` ordering; type-to-search +
  source/recency filters; shared fuzzy matcher.
- **Tier 14 — catalog capability correctness.** Probed vision flags; image plumbing in
  all 4 providers; fail-open attach / fail-closed routing.
- **Tier 15 — voice.** Button-first conversation mode: dictate / read-aloud / hands-free;
  VAD endpointing, interrupt-on-speech, mute, streaming TTS (Kokoro vendored), `/voice`
  doctor. ([~] "better than the original" is open-ended; minor polish remains.)
- **Tier 16 — multi-agent orchestration.** `task_group` (read-only parallel fan-out +
  escalation + synthesize) and `task_group_mutating` (parallel writes in isolated git
  worktrees, one apply-time approval, rebase-on-conflict).
- **Tier 17 — workflows.** Authored `~/.eigen/workflows/<name>.md`; `eigen run <name>`
  + in-TUI `/workflow`. ([~] triggers + authoring-from-history = v2.)
- **Tier 18 — non-chat model servers.** Embedder seam + semantic `retrieve`; Bedrock
  `generate_image`; opt-in local-first background routing. ([~] local-routing for main
  turns; #5 non-LLM heads = out of scope.)
- **Tier 19 — remote.** `eigen --remote user@host` (ssh-backed sessions), `eigen remote
  install`, `eigen remote add/list/remove`, `eigen attach --sock` (ssh -L), Machines page.
  ([~] raw network/WebSocket listener = DECIDED AGAINST: ssh -L + Tailscale is the safer
  story for an RCE-grade endpoint.)
- **Tier 22 — design system.** ✅ One visual language across chat + app: deep-teal
  palette + elevation (base/surface/overlay), one glyph vocabulary, one selection +
  one active treatment, one breathing-λ motion signature, framed syntax-highlighted
  code, uniform diff bands, composed spacing, dense command-center home, warmed
  microcopy; the whole View is painted on Base (no terminal-bg leak). Brief +
  inventory in `docs/design-system.md` / `docs/design-inventory.md`; `eigen theme`
  renders the live swatch; a drift-guard test enforces "no raw color outside theme".
- **Tier 24 — roadmap cleanup.** ✅ This pass: split done from open, terse Shipped
  ledger, Now/Next/Later up top.
- **Tier 26 — hierarchical tool disclosure + background shells (2026-06-15).** ✅
  Two big ergonomics wins:
  (1) PROGRESSIVE TOOL DISCLOSURE — the agent no longer carries ~73 MCP schemas
  (~10k tokens) on every request. `tool.Definition` gains `Niche`+`Group`+
  `GroupDesc`; the prompt lists niche GROUPS (one line each), `search_tools <server>`
  reveals tool NAMES, `search_tools <keyword>` returns full schemas + UNLOCKS them
  (sticky per session). MCP server `description` is REQUIRED at the config level
  (mcp.json `description` → server `initialize.instructions` → warn); it's the
  Level-0 frontmatter, so every server (incl. builtins, with backfilled
  descriptions) is clearly named. Live-verified end-to-end (chrome drilled in,
  used).
  (2) BACKGROUND SHELLS (Claude-Code ctrl+b) — `bash background=true` returns a
  shell id immediately and keeps running so the agent parallelizes; `bash_output`
  polls, `kill_shell` stops; the running shells are injected into the prompt each
  step (awareness). `alt+d` (zellij-safe; ctrl+b eaten by zellij) backgrounds the
  bash running RIGHT NOW mid-turn — it's adopted as a shell and the agent
  continues in the same turn. A `[shells]` right-panel tab lists them (click to
  expand/kill). In-memory per-session (NO disk → no startup-signal hazard; kill
  only ever signals a live pgid); 30-finished-shell retention cap. Live-verified
  in the X11 workspace (`sleep 60` → alt+d → "backgrounded as shell-1" → agent
  kept polling → `[sh]` panel showed it).
- **websearch — second general head + honest fallback (2026-06-15).** ✅ Added
  DuckDuckGo + keyless Brave (search.brave.com HTML) general engines, so a
  rate-limit/anti-bot block on one general head still has broad-web fallback
  before dropping to niche/encyclopedic; chain is now Mojeek → DuckDuckGo →
  brave-web → Marginalia → Wikipedia (each keyless head opt-out-able:
  `EIGEN_WEBSEARCH_NO_MOJEEK`/`_NO_DUCKDUCKGO`/`_NO_BRAVE_WEB`). DDG `uddg`
  redirect hrefs are unwrapped; Brave is parsed on STABLE tokens (the `l1`
  result anchor + the `search-snippet-title` title= attribute) so its rotating
  Svelte class hashes don't break it. The keyless chain error also now
  aggregates EVERY engine's failure ("all search engines failed: mojeek: …;
  duckduckgo: …; …") instead of naming one — a real chain failure is
  distinguishable from a
  single-engine rate-limit. The chain already fell through correctly on a
  rate-limited engine (TestChainFailureIsolation); this just gives it a stronger
  general fallback. Live-verified DDG returns + unwraps results.
- **Daemon title-goroutine race fixed (2026-06-15).** ✅ `Host.maybeTitle`'s
  fire-and-forget meta-write goroutine was unwaited, racing test/teardown cleanup
  (the "TestTitleInFlightGuard flake" — a real bug, not a flake). `Host.titleWG`
  + `waitTitles()`; 0 failures in 40 runs (was ~1/14), `-race` clean.
- **Subagent lifecycle + steer (2026-06-15).** ✅ (1) STEER: enter-while-running
  injects a message BETWEEN tool-call rounds (mid-turn course-correct), not at
  end-of-turn. (2) IDLE-STALL: a subagent with no tool call for `stall_idle_min`
  (2) is killed as hung — NOT a global wall-clock; steady tool calls run as long
  as needed. (3) PROMOTION: a foreground subtask still active past
  `front_window_min` (2) moves to the background; the orchestrator gets a task id
  and keeps working. (4) WAKE: a finished background task wakes its idle
  orchestrator with the result (hands-free). (5) alt+z / click the status line
  backgrounds the running turn (ctrl+z is captured by zellij). Routing fix:
  gpt-5.5 lost its Strict affinity (it was wedged) so general work routes to opus.
- **`/add-dir` — extra working directories (user grant).** ✅ The USER (never the
  agent) can extend a session's tool sandbox to more dirs: `/add-dir <path>` +
  repeatable `--add-dir` flag. Policy.AddRoot (RWMutex-guarded; existing-dir +
  not-denied validated), Agent.AddDir/Roots, a daemon `add-dir` op, and
  AddedRoots persisted in session meta + re-validated on restore. Secret/.git
  denials still apply inside an added root; bash cwd stays the primary root.
- **Instance isolation.** `eigen dev` runs eigen on a separate "dev" daemon instance
  (own socket/sessions/tasks) so `/rebuild` never kills production sessions.

---

## Debt / bugs
- [x] **Untitled daemon sessions.** FIXED: `Host.Restore` backfills titles; `info()`
  falls back to a first-message snippet; titler errors log + retry; in-flight guard.

---

## Conventions / verify gate (durable)
- **Verify gate (every change):** `gofmt`, `go build ./...`, `go vet ./...`,
  `go test ./... -count=1`, `staticcheck`. TUI chrome changes ALSO pass the
  size-sweep (`internal/tui/sizes_test.go`). `go` lives at `~/.local/bin/go`
  (not on the default PATH); `staticcheck` via `~/go/bin`.
- **Live-verify** a change with a real model headless: `EIGEN_NO_DAEMON=1 eigen
  --perm auto -p "…"`. For TUI visuals: an isolated `HOME=/tmp/…` + an xterm with
  `-bg '#1b1c20'` (the user's ghostty bg, to catch unpainted cells).
- **Dev workflow:** iterate on eigen with `eigen dev` (isolated "dev" daemon
  instance). `EIGEN_NO_DAEMON=1` is the in-process escape hatch.
- **Commit** via `git commit -F <file>` when the message has backticks/`$()`
  (shell substitution bites). Commit often; push/deploy/delete need explicit OK.
- **Models (user-set):** default `us.anthropic.claude-opus-4-8` effort max;
  failover `glm-5.2` → `us.anthropic.claude-sonnet-4-6` (gpt-5.5 dropped from the chain — it was hanging/500ing). fable-5 REMOVED
  (Bedrock 500s); native Anthropic removed (Bedrock-only). gpt-5.5 capped to
  MEDIUM effort. Routing: opus + gpt-5.x = Tier-3/med; `glm-5.2` (1M ctx) sits
  in Tier-2 ABOVE `sonnet-4-6`.
- **Finish each tier:** ship items or mark `[~] DEFERRED — why` (not a bare `[ ]`).

## Notes / grounding
- read-aloud tool the user has: `readd` at `~/projects/tfqol/readd`.
- skills format = Claude Code SKILL.md (YAML frontmatter `name`,`description`
  [,`allowed-tools`] + markdown body).
- permission postures: `gated` (asks for mutating tools) / `auto`.

## Configuration & extension reference (as shipped)
Tools (~28): read, list, glob, grep, symbols, tree, diff, write, edit, multiedit,
apply_patch, move, bash (+ background=true), bash_output, kill_shell, fetch, todo,
skill, memory, task, task_status, task_group, task_group_mutating, goal_achieved,
retrieve, generate_image, websearch (always available — keyless by default),
search_tools (unlocks niche tools on demand) (+ plugins + MCP + LSP, all niche/
disclosed via search_tools; builtin MCP: `workspace` sandbox, `chrome` bridge).

Files (under `~/.eigen/`, plus project-local `./.eigen/`):
- `config.json` — defaults: `provider`,`model`,`perm`,`effort`,`theme`,`nerd_font`,
  `input_mode`,`max_tokens`,`daemon_timeout`,`tts_cmd`,`notify_cmd`,`judge_model`,
  `route`,`route_providers`,`local_background`,`skills_dirs`,`dream_on_idle`,`idle_minutes`
- `skills/<name>/SKILL.md` — discovered skills (also `EIGEN_SKILLS_DIRS`, colon-sep)
- `plugins.json` / `mcp.json` / `lsp.json` / `hooks.json` — extensions (per-scope)
- `memory/global.md` + `memory/<project>.md` — durable notes (auto-injected)
- `daemon/sessions/*.jsonl` — durable daemon sessions · `sessions/*.eigen.jsonl` — local
- `workflows/<name>.md` — authored workflows · `hosts.json` — remote machines
- `tasks/` — background-task records · `index/` — semantic retrieval index
- `daemon.sock`/`daemon.pid`/`daemon.log` (+ `-<instance>` suffix for named instances)

Env: `EIGEN_PROVIDER`, `EIGEN_PERMISSION`, `EIGEN_MAX_CONTEXT_TOKENS`,
`EIGEN_REASONING_EFFORT`, `EIGEN_THEME`, `EIGEN_NERD_FONT`, `EIGEN_DAEMON_TIMEOUT`,
`EIGEN_INSTANCE`, `EIGEN_SRC`, `EIGEN_NO_DAEMON`, `EIGEN_TTS_CMD`, `EIGEN_NOTIFY_CMD`,
`EIGEN_CLIPBOARD_CMD`, `EIGEN_SKILLS_DIRS`, `EIGEN_LLAMA_BASE_URL`,
`EIGEN_EMBED_BASE_URL`, `EIGEN_IMAGE_MODEL`, `EIGEN_SMALL_MODEL`, `EIGEN_SUGGEST_MODEL`.
Web search (keyless by default — these only pick a PREFERRED head): `TAVILY_API_KEY`,
`BRAVE_API_KEY`, `EIGEN_SEARXNG_URL`, or `EIGEN_WEBSEARCH_URL` (+ `EIGEN_WEBSEARCH_KEY`);
`EIGEN_WEBSEARCH_NO_MOJEEK` / `_NO_DUCKDUCKGO` / `_NO_BRAVE_WEB` opt out of those
keyless scrapes; `EIGEN_WEBSEARCH_ALLOW_LOOPBACK`
/ `EIGEN_WEBSEARCH_ALLOW_PRIVATE` permit a local/LAN SearXNG past the SSRF guard.

CLI: `eigen [task]` · `-p` print · `--resume/-c` · `--list` · `--list-skills` ·
`--list-tools` · `eigen dev` · `eigen daemon [status|stop|install|prune|stdio]` ·
`eigen attach [--sock]` · `eigen --remote <host>` · `eigen remote <install|add|list|remove>` ·
`eigen run <workflow>` · `eigen theme` · `eigen dream` · `eigen models` ·
`eigen memory <show|backups|consolidate> [--global]` · `eigen skill <add|list>` ·
`eigen chrome [status]`.

TUI: `/help` lists all slash commands; keys: `/` commands · `@` files · ctrl+k
palette · alt+s sessions · alt+w tray · while running: enter steers or queues
(per `input_mode`, toggle `alt+q`) · esc interrupts · alt+d backgrounds the
running bash step · alt+z backgrounds the whole turn.
