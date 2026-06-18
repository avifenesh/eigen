# eigen roadmap

eigen is a Go terminal coding agent. The real app is `eigen daemon` (a long-lived
host of many chat sessions over `~/.eigen/daemon.sock`); terminal windows are
thin views (`eigen attach` / normal `eigen` auto-attach). The user is the END
user — durability, polish, and day-to-day ergonomics are first-class.

This file is the **forward plan**. It is intentionally not a changelog: shipped
work is a terse ledger, with details in git history, docs, and project memory.

---

## Roadmap audit — 2026-06-16

Audit outcome: the previous roadmap had several fully shipped tiers still sitting
under “Open work.” The actual unshipped backlog is now much smaller.

**Current source of truth:**
- Repo is clean at the time of this audit.
- Recently shipped but previously under-recorded:
  - native Codex/gpt-5.5 backend and fast mode follow-ups;
  - session persistence/data-loss hardening;
  - Codex streamed `server_error` retry;
  - daemon build identity in `eigen daemon stats`.
- Fully shipped and no longer “open”: Tier 30 token efficiency, Tier 23 daemon
  resource health, Tier 21 TUI ergonomics, Tier 22 design system, Tier 26 tool
  disclosure/background shells, Tier 31 custom commands, Tier 32 Codex backend.

**Actual open backlog:**
1. Tier 20 v2 — control Eigen from another machine (phone/pocket baseline is done).
2. Tier 7 leftovers — background-task escalation and bigger planning/research
   experiments.

**Recently completed:**
- Tier 27 v1.1 — plugin/marketplace UX + plugin slash commands + Claude/Codex
  `agents/`/subagent prompt compatibility.

**Operational note:** when rebuilding Eigen, a running daemon keeps executing the
old binary until the user restarts it. `eigen daemon stats` now exposes the
running executable + exact binary SHA so we can verify this directly.

---

## Now / Next / Later

**Now (in flight):**
- Tier 27 v1.1 is complete; no plugin/marketplace work is knowingly in flight.

**Next (queued, well-scoped):**
- **Tier 20 v2 — control from another machine.** Phone/pocket baseline is done;
  the remaining product is a safe cross-machine control surface.

**Later (parked big bets — pull when wanted):**
- **Tier 7 leftovers.** Background-task escalation (rerun/hand off failed or
  underpowered background tasks to a bigger model), plus unbuilt planning dreams
  (#13 ultraplan, #23 non-LLM heads, #24-style adversarial expansion if wanted).

**Deferred by decision, not bugs:**
- Raw daemon network listener: stay with Unix socket / ssh / Tailscale.
- Agent-side install of skills/plugins: CLI/user action only.
- Full marketplace authoring/publishing from Eigen: later; v1 consumes/manages.
- Non-LLM local heads as first-class planning modules: parked until a concrete
  use-case beats normal model/tool routing.

---

## Open work — detail

### Tier 27 v1.1 — plugins / marketplaces product surface

**Goal:** make installed marketplaces/plugins feel first-class in the app and the
chat command surface, while preserving the safety boundary: the agent cannot
install code; only the user can.

Already shipped:
- Marketplace registry: `eigen marketplace add|list|remove/delete|enable|disable|update`.
- Plugin install engine: `eigen plugin install|list|remove/delete|enable|disable`.
- Claude `.claude-plugin` and Codex `.agents/plugins` / `.codex-plugin` format parsing.
- `url` / `git-subdir` / local marketplace source forms, including real `agent-sh/agentsys`.
- Skills/MCP/hooks/commands wiring with scanner/rollback safety.
- Claude/Codex `agents/*.md` are installed as native Eigen task roles.
- Custom slash commands from `~/.eigen/commands` and project `.eigen/commands`;
  plugin commands are wired as prefixed commands.

Remaining v1.1 work:
- [x] **Slash command wrappers.** Chat commands now cover the existing safe CLI
  operations (`/plugin list|install|remove/delete|enable|disable`,
  `/marketplace list|add|update|remove/delete|enable|disable`) and bare `/plugins`, `/plugin`, and
  `/marketplace` open the plugins page. Install/remove remain user-typed slash
  commands, never model tools.
- [x] **App browse/install page.** Upgraded from raw status display to a
  first-class plugins surface: tabbed Plugins / Marketplace / Wiring page,
  installed/enabled state, batch install, install/remove/delete/enable/disable
  actions, marketplace add/remove/delete/enable/disable/refresh, catalog
  browsing, pre-install manifest/component preview, scanner-risk history, and
  rollback/result detail display.
- [x] **Claude/Codex `agents/` compatibility.** Plugin-provided agents are
  installed under `~/.eigen/agents/` and exposed as foreground/background
  `task` roles using the installed role name. App/chat command palettes surface
  installed agent roles, and the system prompt lists enabled role names plus
  routing/read-only metadata when `task`/`task_group` are available. Agent
  frontmatter can provide routing metadata (`kind`, `difficulty`, `model`) and
  read-only tool metadata; read-only plugin agents are admitted to `task_group`,
  while mutating/unknown agents inherit normal task tools and stay blocked from
  parallel fan-out.
- [x] **Docs + smoke test.** Added fixture coverage for marketplace source
  polymorphism, GitHub PAX tarballs, Codex local marketplaces, Codex plugin
  manifests, and agent adaptation. docs/plugins.md updated.

Suggested next tier:
1. Tier 20 v2 remote cross-machine control.
2. If a future real marketplace requires non-GitHub git fetch, add it as a
   compatibility follow-up rather than reopening Tier 27 v1.1.

### Tier 20 v2 — control from another machine

**Goal:** control/approve Eigen from another trusted machine without disturbing
running local sessions and without opening an unsafe raw daemon listener. The
phone/pocket baseline is considered done; this v2 is about cross-machine control.

Open work:
- [ ] **Remote control surface.** See running sessions, attach/read recent output,
  and send user input from another machine.
- [ ] **Remote approve/deny.** Route decisions back into the existing daemon
  approval queue; fail closed; audit logged.
- [ ] **Status/recent.** “What’s running?”, “what changed?”, “show recent result”
  for a session.
- [ ] **Security constraints.** Prefer SSH/Tailscale/outbound relay over a raw
  public listener; allowlist enforced; short-lived/signed callback payloads if a
  relay is introduced; approvals stay strictly gated.

Non-goals for v2:
- arbitrary unauthenticated daemon access;
- bypassing normal approval gates;
- interrupting important prod sessions during setup.

### Tier 7 leftovers — background-task escalation + research bets

Open work:
- [x] **Background-task escalation.** Direct `task(background=true)` subtasks and
  foreground subtasks promoted into the background now retry once at the next
  difficulty tier when they fail, stall, or explicitly report an underpowered
  model; explicit model overrides are respected and not escalated. Context-window
  failures retry with compacted task/transcript prompts rather than blind model
  reroute. `task_status` verbose mode shows attempt history plus state/transcript
  paths.
- [x] **Child transcript viewer / promote-to-session.** `task_status(verbose=true)`
  exposes transcript/state paths and attempt summaries; `task_status(tail:N)`
  shows the last N transcript messages inline; `task_promote(id)` copies the
  background transcript into `~/.eigen/sessions` as a resumable Eigen session
  and returns the `eigen --resume` command.
- [ ] **Ultraplan / adversarial expansion / non-LLM heads.** Parked research
  items. Pull only when there is a concrete product reason.

### retrieve (semantic search) — deferred enhancements

v1 semantic `retrieve` is shipped (Tier 18); `internal/retrieve` indexes project
files with line-window chunks and brute-force cosine. Parked upgrades:

- reranker pass over brute-force cosine hits;
- index session transcripts and memory, not just project files;
- AST-aware chunking for code (split on declarations, not fixed windows);
- ANN / approximate-nearest-neighbor index to replace brute-force cosine at repo
  scale.

### Reliability watchlist (no active build unless it reproduces)

- **Codex stream failures.** Pre-output `response.failed` server errors are now
  retried. If users still see `codex stream failed: server_error`, determine
  whether retries were exhausted or whether the failure happened after partial
  streamed output (unsafe to auto-retry without duplicating visible text).
- **Session durability.** Current hardening is file-based and intentionally small:
  shutdown flush, pending-steer flush, immediate `/clear` persistence, fsync,
  rotating backups. Do not jump to a database/journal unless the file approach
  shows another concrete failure.
- **Prod/dev identity.** `eigen daemon stats` exposes binary identity; use it
  before assuming a running daemon is on the latest build.

---

## Shipped ledger

### Tier 33 — session durability + Codex resilience (2026-06-16)

Shipped after a real data-loss incident. The goal was not overengineering; it was
to close the concrete holes that cost time.

- **Lossless daemon shutdown.** `Host.Shutdown` flushes every session transcript,
  interrupts in-flight turns, waits briefly for cancellation to unwind, then
  flushes again.
- **Pending user input is durable.** Steered mid-turn input is flushed on
  shutdown so a follow-up typed during a running turn is not lost.
- **Immediate `/clear` persistence.** Clearing a session writes the empty
  transcript immediately, so restart cannot resurrect stale history.
- **Transcript hardening.** Atomic write via temp+rename; fsync temp; best-effort
  fsync directory; keep five backup generations (`.bak` through `.bak.4`);
  session delete removes backups too.
- **Persist errors are visible.** Daemon logs transcript save failures instead of
  swallowing them silently.
- **Codex streamed failure retry.** Pre-output Codex SSE `response.failed`
  (`server_error` / `rate_limit` / `overloaded`) is retried with backoff; failures
  after visible output are surfaced to avoid duplicated text.
- **Build identity in stats.** `eigen daemon stats` reports version, executable,
  binary SHA, Go version, and embedded VCS revision/dirty bit when available.

Validation used: focused daemon/transcript/agent/llm tests, full `go test ./...`,
targeted staticcheck, and `git diff --check`.

### Tier 32 — native Codex provider + fast mode (2026-06-16)

Native provider over the ChatGPT-account Codex backend
`https://chatgpt.com/backend-api/codex` using `~/.codex/auth.json` OAuth tokens.
This is not api.openai.com; api-key OpenAI remains the mantle/openai path.

Key shipped behavior:
- `gpt-5.5` is the default main agent through provider `codex`.
- Codex backend requirements handled:
  - system prompt in top-level `instructions`,
  - `store:false`,
  - `stream:true`,
  - `include:["reasoning.encrypted_content"]`,
  - carry item-level `encrypted_content` back on the next turn.
- SSE parsing uses `response.output_item.done` for tool calls/messages/reasoning;
  the `completed` event is not trusted as the only output source.
- Legacy bare reasoning IDs self-heal by being dropped instead of causing 404.
- Fast mode = `service_tier:"priority"`, surfaced in TUI/sidebar/status and
  inherited by trivial/easy subtasks when safe.
- Failover/routing adjusted around Codex + opus + GLM.

### Tier 31 — custom slash commands

Claude-format markdown commands from user/project/plugin command directories;
argument expansion in TUI; plugin commands prefixed to avoid collisions.
Telegram intentionally does not expand command arguments.

### Tier 30 — token efficiency

Shipped:
- cache read/write token accounting;
- prompt-cache hit reporting in daemon stats;
- tool schema caching/progressive disclosure;
- memory injection kept to compact `SUMMARY.md`;
- compaction trigger tightened (0.85) and stale tool output shedding;
- output/effort discipline for subtasks;
- performance docs and `make perf` / `make perf-tokens` style checks.

### Tier 29 — adversarial cross-vendor planning

`plan` tool / `eigen plan <task>`: author model writes a plan, other-vendor
adversary critiques, author revises until approval or round budget. Includes
fallback adversary ladder and timeout so a flaky primary does not block.

### Tier 28 — memory v2

Codex-style structured memory pipeline:
- `~/.eigen/memory/<scope>/raw/` rollout summaries;
- `MEMORY.md` curated layer;
- `SUMMARY.md` is the only injected tier;
- `bans.md` hard prohibitions;
- sqlite job queue/index;
- `eigen dream` + nightly idle dreamer;
- proposed skills generated but never auto-installed.

### Tier 27 — plugins / marketplaces v1

CLI marketplace/plugin consume-and-manage shipped:
- marketplace add/list/remove/delete/enable/disable/update;
- plugin install/list/remove/delete/enable/disable;
- Claude `.claude-plugin` and Codex `.agents/plugins` / `.codex-plugin` parsing;
- skills/MCP/hooks/commands wiring plus initial agents-as-skills adaptation
  (v1.1 now maps plugin agents into native task roles);
- scanner + rollback safety;
- agent has no install tool.

v1.1 remains open above.

### Tier 26 — progressive tool disclosure + background shells

- Niche MCP/browser/desktop tools are not all injected every turn.
- `search_tools` reveals grouped tool names or matching schemas and unlocks
  them for the session.
- Bash supports `background=true`, `bash_output`, `kill_shell`, and mid-turn
  detach (`alt+d`) with a shells panel.

### Tier 23 — performance + resource health

Shipped:
- `stats` daemon op and `eigen daemon stats`;
- RSS/heap/goroutine/session/running/bg-task visibility;
- background-task in-memory reap cap;
- replay buffer bounded;
- soak tests and benchmarks;
- docs/performance.md.

### Tier 22 — design system

One visual language across chat and app:
- deep-teal palette with surface/elevation;
- one glyph vocabulary;
- one selection treatment;
- one working motion (breathing λ);
- framed syntax-highlighted code;
- uniform diff bands;
- dense command-center home;
- warmed empty states;
- no terminal background leaks.

### Tier 21 — TUI ergonomics

Shipped:
- per-session notepad tab;
- configurable steer vs queue input mode;
- home density/working-now improvements.

### Tier 20 — pocket mode

Phone/pocket baseline is done; remaining cross-machine control work is tracked
above as Tier 20 v2.

### Tier 19 — remote

SSH-backed remote sessions, `eigen remote install`, host registry, attach over
forwarded socket/stdout. Raw network daemon listener deliberately rejected.

### Tier 18 — non-chat model servers

Embedder seam + semantic `retrieve`; image generation; local-first background
routing where configured. Non-LLM planning heads parked.

### Tier 17 — workflows

Authored markdown workflows in `~/.eigen/workflows/<name>.md`; `eigen run` and
TUI `/workflow`. Triggers/authoring-from-history deferred.

### Tier 16 — multi-agent orchestration

`task_group` read-only fan-out with synthesis and `task_group_mutating` parallel
isolated-worktree implementers with one merge approval.

### Tier 15 — voice

Button-first voice mode: dictate/read-aloud/hands-free, VAD endpointing,
interrupt-on-speech, mute, Kokoro TTS, `/voice doctor`.

### Tier 14 — catalog capability correctness

Model capability probing, vision flags, image plumbing, fail-open attach and
fail-closed routing.

### Tier 13 — session-list ergonomics

Last-attached ordering, type-to-search, source/recency filters, shared fuzzy
matcher.

### Tier 12 — subagent observability

Tasks panel, durable background-task store, cross-process cancel markers.
Child transcript viewer/promote-to-session remains optional future work.

### Tier 11 / 11.5 — panels + chrome consolidation

Right-panel tabs `[changes][git][term][tasks]`, real PTY terminal, resize/persist
widths, inline diffs, headerless left command sidebar at normal widths.

### Tier 10 — app shell clickable/structural

Mouse parity, framed panels, named rects, per-page click handling.

### Tier 9 — chat is the app

Clickable chrome around transcript: status/sidebar/header/rail/right panel and a
single action registry.

### Tier 8 — eigen the app

Daemon/view architecture; app shell pages for home/projects/sessions/config/
skills/models/providers/memory/crons/plugins; proactive feed.

### Tier 7 — leftovers

Core shipped enough to support background tasks/subagents; escalation and bigger
research dreams remain open above.

### Tier 6 — agent quality

Compaction, failover chain, routing, effort levels, usage reporting.

### Tier 5 — TUI/UX polish

Steer+queue, mouse, autocomplete, rich tool blocks, diffs.

### Tier 4 — dreaming + learning

Idle reflection into durable memory and consolidation.

### Tier 3 — plugins + extension substrate

`plugins.json` tools, MCP servers, hooks/LSP/config substrate.

### Tier 2 — core tool catalog

Core filesystem/search/edit/bash/fetch/todo/skill/memory/task/review/retrieve/
image/web/search-tools capabilities.

### Tier 1 — core agent

Agent loop, tool use, sessions, permission gating, MCP/LSP wiring.

---

## Debt / bugs

No known critical open data-loss bugs after Tier 33. Watch the reliability notes
above; promote a watch item to this section only when it reproduces with a clear
failure mode.

---

## Verify gate / conventions

- **Normal gate:** `gofmt`, `go build ./...`, `go vet ./...`,
  `go test ./... -count=1`, staticcheck.
- **Project gate:** `make gate` when practical; `make perf` for performance/
  soak-sensitive changes.
- **TUI visual changes:** include the size-sweep tests under `internal/tui`.
- **Live verification:** for model/provider changes, run a real model headless
  when possible (`EIGEN_NO_DAEMON=1 eigen --perm auto -p "..."`).
- **Production daemon:** do not restart it from an agent session. Rebuilds update
  `bin/eigen`; the running daemon stays on its current executable until the user
  restarts it. Use `env -u EIGEN_INSTANCE eigen daemon stats` to verify prod.
- **Prod instance:** default/empty instance. Use `env -u EIGEN_INSTANCE ...` from
  a plain terminal to avoid accidentally targeting dev.
- **Dev instance:** `eigen dev` uses `--instance dev` and separate socket/session
  stores.
- **Commit often locally; ask before pushing.**

---

## Configuration & extension reference

CLI highlights:
- `eigen [task]`
- `eigen -p "task"`
- `eigen -c` / `eigen attach [id]`
- `eigen dev`
- `eigen daemon [status|stats|stop|prune|stdio|install|uninstall]`
- `eigen remote <install|add|list|remove>`
- `eigen run <workflow>`
- `eigen dream`
- `eigen models`
- `eigen memory <show|backups|consolidate>`
- `eigen skill <add|list|proposed|accept|reject>`
- `eigen marketplace ...`
- `eigen plugin ...`

Always-available tool families:
- filesystem/search/edit/diff;
- bash + background shells;
- web/fetch/search;
- todo/plan/review/goal;
- memory/skills;
- subtasks and task groups;
- retrieve/image generation;
- `search_tools` for niche MCP/browser/desktop tools.

Important paths:
- `~/.eigen/config.json`
- `~/.eigen/daemon/sessions/*.jsonl` + `.meta.json`
- `~/.eigen/daemon-dev/sessions/` for dev instance
- `~/.eigen/memory/<scope>/`
- `~/.eigen/tasks/`
- `~/.eigen/commands/`
- `~/.eigen/plugins-installed.json`
- `~/.eigen/marketplaces.json`
- `~/.eigen/daemon.sock` / `~/.eigen/daemon-dev.sock`

Important env:
- `EIGEN_INSTANCE`
- `EIGEN_NO_DAEMON`
- `EIGEN_PROVIDER`, `EIGEN_REASONING_EFFORT`, `EIGEN_MAX_CONTEXT_TOKENS`
- `EIGEN_CODEX_AUTH`, `EIGEN_CODEX_BASE_URL`, `EIGEN_CODEX_SERVICE_TIER`
- `EIGEN_TELEGRAM_TOKEN`, `EIGEN_TELEGRAM_ALLOW`
- websearch opt-outs / preferred engines as documented in tool docs.
