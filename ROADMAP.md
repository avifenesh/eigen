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
- **`/add-dir` — extra working directories.** Let the agent (and bash) operate
  outside the single session root by adding more allowed roots at runtime.
- **Tier 23 — performance + resource health.** RSS/leak soak, bound growth,
  turn-latency profile, a `make perf` guard, `docs/performance.md`.
- **Tier 21 — remaining TUI ergonomics.** Right-panel notepad tab; a
  steer-vs-queue config default. (Home density already shipped via the design work.)

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

### `/add-dir` — add extra working directories (cross the single-root limit)
Today a session's tools are confined to ONE root (`tool.NewPolicy(p.Dir)` in
build.go) — `tool.Policy.Resolve` rejects anything "outside the allowed roots"
and `bash` runs in `policy.Dir()` (the primary root) with relative paths
resolved there. So the agent can't read/edit/run across a sibling repo, a shared
lib, a scratch dir, etc. — a real friction when work spans dirs.

The Policy ALREADY supports multiple roots (`NewPolicy(roots...)`, `within()`
checks them all) — what's missing is a way to ADD one at runtime and have the
live tools honor it. Like Claude Code's `--add-dir` / `/add-dir`.

Scope:
- [ ] **Mutable roots.** Make `Policy` roots safely extendable at runtime
  (`AddRoot(path)` under a mutex; re-clean/abs/symlink-resolve like NewPolicy).
  Tools capture the `*Policy` pointer, so an added root takes effect immediately
  for read/list/glob/grep/write/edit/move (they go through `Resolve`).
- [ ] **bash across roots.** `bash` still runs in the primary `Dir()`, but a
  command can now `cd`/reference an added root without the resolve denying it.
  Decide whether bash's cwd stays the primary root (yes — predictable) and the
  added dirs are simply reachable.
- [ ] **`/add-dir <path>` command + CLI flag.** A slash command (TUI) +
  `--add-dir` (repeatable, headless) that calls `AddRoot`. Tab-complete the path;
  reject a path that doesn't exist; show the active roots (e.g. in `/help` or a
  status line). Daemon sessions: route through a `set`/new daemon op so the
  added root persists in the session (survives detach) and is applied to the
  hosted agent's policy.
- [ ] **Keep the denials.** `.git`/`.ssh`/`.aws`/secret-file denials still apply
  inside an added root (they're per-path, not per-root). An added root is still
  a deliberate user grant — confirm/echo it so it's never silent.
- [ ] **Persistence + display.** Persist added roots in the daemon session meta
  (so a reattach keeps them); surface them somewhere (sidebar/cwd line or
  `/help`). Tests: Resolve accepts paths in an added root + still denies outside
  all roots + still denies secrets within.

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
- [ ] **Right-panel notepad tab.** A scratch pad in the right panel (a fifth
  tab) — freeform notes per session, persisted; survives detach/restart.
- [ ] **Default steer-vs-queue choice (config).** Today enter-while-running
  always queues; some users want it to interrupt-and-send. A config default
  (`steer`/`queue`) + the per-press override.
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
- Unbuilt §7 dreams (no commitment): #13 ultraplan, #23 non-LLM heads,
  #24 adversarial GPT×Claude planning.

---

## Shipped (terse ledger — full writeups in git history + project memory)

- **Tier 1 — core capabilities.** Agent loop, tools, sessions, perm gating, MCP/LSP.
- **Tier 2 — tools + catalog.** read/list/glob/grep/symbols/tree/diff/write/edit/
  multiedit/apply_patch/move/bash/fetch/todo/skill/memory/task/goal_achieved.
  (`think` tool skipped — redundant with streamed reasoning.)
- **Tier 3 — plugins + extension.** `plugins.json` external-command tools; MCP servers.
- **Tier 4 — dreaming + learning.** Idle reflection → durable memory; consolidation.
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
- **Tier 25 — websearch replaced (native Go port, no MCP).** ✅ `websearch` is now
  ALWAYS available: keyless fallback chain (Mojeek HTML scrape → Marginalia JSON →
  Wikipedia JSON) with per-engine failure isolation + gather/dedup; Brave/Tavily
  key or `EIGEN_SEARXNG_URL` is the preferred head; SSRF host check (loopback/LAN
  refused unless opted in); per-engine timeout slicing. Ported from
  `@agent-sh/harness-websearch` v2 into `internal/tool/websearch{,_engines}.go`.
  Live-verified keyless end-to-end.
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
  failover `openai.gpt-5.5` → `us.anthropic.claude-sonnet-4-6`. fable-5 REMOVED
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
Tools (~25): read, list, glob, grep, symbols, tree, diff, write, edit, multiedit,
apply_patch, move, bash, fetch, todo, skill, memory, task, task_status,
task_group, task_group_mutating, goal_achieved, retrieve, generate_image,
websearch (always available — keyless by default) (+ plugins + MCP + LSP; builtin
MCP: `workspace` sandbox, `chrome` bridge).

Files (under `~/.eigen/`, plus project-local `./.eigen/`):
- `config.json` — defaults: `provider`,`model`,`perm`,`effort`,`theme`,`nerd_font`,
  `max_tokens`,`daemon_timeout`,`tts_cmd`,`notify_cmd`,`judge_model`,`route`,
  `route_providers`,`local_background`,`skills_dirs`,`dream_on_idle`,`idle_minutes`
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
`EIGEN_WEBSEARCH_NO_MOJEEK` opts out of the Mojeek scrape; `EIGEN_WEBSEARCH_ALLOW_LOOPBACK`
/ `EIGEN_WEBSEARCH_ALLOW_PRIVATE` permit a local/LAN SearXNG past the SSRF guard.

CLI: `eigen [task]` · `-p` print · `--resume/-c` · `--list` · `--list-skills` ·
`--list-tools` · `eigen dev` · `eigen daemon [status|stop|install|prune|stdio]` ·
`eigen attach [--sock]` · `eigen --remote <host>` · `eigen remote <install|add|list|remove>` ·
`eigen run <workflow>` · `eigen theme` · `eigen dream` · `eigen models` ·
`eigen memory <show|backups|consolidate> [--global]` · `eigen skill <add|list>` ·
`eigen chrome [status]`.

TUI: `/help` lists all slash commands; keys: `/` commands · `@` files · ctrl+k
palette · alt+s sessions · alt+n tray · while running: enter queues · esc
interrupts · ctrl+z backgrounds the turn.
