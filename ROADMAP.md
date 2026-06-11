# eigen roadmap

Persistent, cross-session backlog. Work top-down within a priority tier; check
items off as they land. Every item must ship with tests and keep `go build ./...`,
`go vet ./...`, `go test ./...` green. Do not commit unless asked.

Conventions:
- `[x]` done · `[~]` in progress · `[ ]` todo
- Each feature: implement + test + (if user-facing) wire into `/help`.

## Shipped (this work)
- [x] Robustness: recover panics in TUI `Update`; autosave on quit; fix `sb` Builder-copy crash
- [x] Steer + queue while running (enter queues, esc interrupts via per-turn cancel)
- [x] Mouse: click-to-toggle blocks + wheel scroll
- [x] Slash-command autocomplete dropdown
- [x] `@file` mention autocomplete (shared completion menu + cached file index)
- [x] Rich per-tool block headers + live status glyph (running/done/failed)
- [x] edit/multiedit diff rendering in tool blocks
- [x] Tools: fetch, multiedit
- [x] Commands: `/perm`, `/model` (display)
- [x] Session store tests, TUI driver tests, tool tests

## Tier 1 — core capabilities (do first)
- [x] **todo tool** + live plan rendering in TUI (agent maintains a checklist)
- [x] **skills system**: discover SKILL.md (`~/.eigen/skills`, `./.eigen/skills`), parse
      frontmatter (name/description/body); inject a catalog into the system prompt;
      add a `skill` tool the model calls to load a skill body into context
- [x] **`/skills` command + `--list-skills`** catalog
- [x] **read-aloud**: `say` tool + `/read` toggle that speaks assistant answers via a
      configurable TTS command (default: espeak-ng/piper/`say`, reuse `readd` engines)
- [x] **memory**: two scopes — global (`~/.eigen/memory/global.md`: the user's
      working style/rules, applies everywhere) + per-project
      (`~/.eigen/memory/<project>.md`), both auto-injected at start, appendable via
      the `memory` tool (`scope: project|global`). Hardened: secret redaction,
      staleness framing, snapshot/backup + atomic rewrite, model-driven
      consolidation (`eigen memory consolidate [--global]`, recency-wins,
      fails-closed). See docs/research-codex-memory.md.

## Tier 2 — more tools + catalog
- [x] **tree** tool (bounded directory tree)
- [x] **apply_patch** tool (multi-file unified-diff patch, atomic)
- [x] **`/tools` command + `--list-tools`** catalog (name, description, posture)
- [x] **websearch** tool (gated; pluggable backend: Tavily/Brave/generic JSON via env)
- [ ] ~~**think** tool~~ — skipped: redundant with streamed reasoning

## Tier 3 — plugins + extension
- [x] **plugin tools**: config-defined external-command tools loaded from
      `~/.eigen/plugins.json` / `./.eigen/plugins.json` (name, desc, json-schema, argv;
      stdin=args json, stdout=result)
- [x] **MCP client** (stdio) — connect to MCP servers, expose their tools [big]

## Tier 4 — "dreaming" + learning
- [x] **dream command** (`eigen dream`): background reflection over recent sessions →
      distil durable learnings into `~/.eigen/memory/<project>.md` (small/local model)
- [x] **idle dreaming**: trigger reflection automatically when idle (opt-in: config `dream_on_idle`)
- [x] **skill synthesis**: dreaming proposes new SKILL.md drafts from repeated patterns

## Tier 5 — TUI/UX polish
- [x] live `/model` switch (thread provider/model into TUI; re-create provider)
- [x] copy selected block to clipboard (`/copy`, command-delegated: wl-copy/xclip/pbcopy)
- [x] token/context-budget usage indicator in the status line
- [x] `/find` transcript search
- [x] config file `~/.eigen/config.json` for defaults (provider/model/perm/tts/theme)

## Tier 6 — deeper agent quality (ongoing)
- [x] approval scope memory in gated mode ("always allow this tool this session")
- [x] `diff` tool (read-only `git diff` view of the working tree)
- [x] `symbols` tool (grep-based func/type/def finder)
- [x] `/export` session to a markdown file
- [x] LCS-based diff rendering for edit blocks
- [x] `move` tool (rename/move files), sub-agent **`task`** tool (depth-bounded delegation)
- [x] real token usage from provider responses (vs. estimate) — shipped 9bc7001:
      every provider parses the usage block its API returns (Converse,
      Anthropic, Mantle, OpenAI-compatible non-stream + final stream frame);
      llm.Response.Usage flows agent EventDone → daemon wire → TUI status bar
      ('<in>·<out> <rate> tok/s'), falling back to ~chars/4 when unreported

## Tier 7 — vision / big bets (captured backlog, unordered)
Raw capture from the user — refine/prioritize later. Numbered for reference only.
1. **Token efficiency in a wider scope than compaction** — *(in progress; shipped:
   MCP per-server `tools` allowlist + schema slimming (~70% off workspace-server
   schema cost), in-conversation dedupe of repeated tool outputs, small-model
   compaction summaries via `CompactorChain`)*. Remaining ideas: retrieval
   instead of re-paste, prompt-cache-aware prefixing, per-turn cheap-model
   routing (→ auto-router).
2. **Diff view of edits** — *(shipped a0c1fe2: intra-line change highlighting,
   context folding, ±N −M header stats, apply_patch/write diff rendering)*
3. **"Goal" feature** — *(shipped 3d0cf4b + 011da68 + judge: /goal set/show/clear,
   injected into the system prompt every step, survives compaction, persisted in
   session meta, idle nag pings until achieved, and the model can clear it by
   calling goal_achieved — an independent small-model judge verifies the
   evidence and only a confirmed verdict clears the goal)*
4. **"Loop" feature** — *(shipped: /loop [interval] <prompt> re-submits the
   prompt every interval while the session is IDLE — never interrupts a running
   turn — until /loop clear. Persists across restart/rebuild via session meta.
   Pairs with /goal + a goal file: edit the file between iterations and the
   model picks up the next item without re-prompting.)*
5. **Automation** — *(shipped (the eigen side, per the user's model: eigen runs
   one task headless and exits; the HOST re-launches it — cron/systemd/shell):
   --prompt-file re-reads its task each run; piped stdin also works; both imply
   -p. Exit 0/non-zero lets the host back off. Pairs with --continue for one
   evolving session. docs/automation-example.md has the systemd timer (OnUnit-
   InactiveSec = "start when it closed, do something, go back") + shell loop.)*
6. **Background scan → proactive action feed** — *(shipped c7500c9:
   internal/feed — git (uncommitted/unpushed per project) + project-memory
   stated intents + GitHub (gh review-requests + assigned issues); each item
   carries a ready Task prompt + project Dir; home 'act on' section + per-
   project feed; enter opens a chat rooted there with the task pre-submitted;
   cached ~/.eigen/feed.json 10-min TTL, async refresh. Ranking + per-kind
   diversity + dismissals shipped 82d6041; upstream-drift ('behind upstream
   by N') shipped 9cc3a38.)*
7. **Computer use built in** — *(shipped via #8: the agent-workspace server is
   auto-registered when its binary is present, giving screenshot/click/key/type
   + browser control as first-class tools without mcp.json editing.)*
8. **Agent workspace built in** — *(shipped: internal/mcp/builtin.go auto-
   registers the agent-workspace-linux binary (EIGEN_WORKSPACE_BIN / PATH /
   ~/.local/bin) as the `workspace` MCP server with the curated 27-tool
   allowlist baked in — no mcp.json needed; a user-configured `workspace` server
   still wins. `eigen workspace status|build` reports/installs it (cargo build
   from EIGEN_WORKSPACE_SRC or ~/projects/agent-workspace-linux → ~/.local/bin).
   It's a gpui Rust binary so it runs as a subprocess MCP, not linked in.)*
9. **Conversation mode** — *(shipped: internal/voice — STT (arecord/parecord →
   whisper.cpp, auto-detected; EIGEN_WHISPER_BIN/MODEL, EIGEN_VOICE_RECORD_CMD)
   + TTS (readd/espeak-ng/say; EIGEN_VOICE_TTS_CMD). /voice toggles conversation
   mode, ctrl+t/alt+t push-to-talk records→transcribes→submits a turn, and each
   assistant answer is spoken (cancelable — a new utterance interrupts). Verified
   live on this host (whisper + readd both detected). Reuses the codex-desktop-
   linux conversation-mode design (VAD/interrupt logic) adapted to the TUI;
   continuous auto-listen could replace push-to-talk later.)*
10. **Auto-router** — *(shipped: opt-in per-task model selection. Policy
    (internal/llm/router.go): among CAPABLE candidates (required search/vision
    + context window) that are GOOD ENOUGH (quality ≥ difficulty floor), pick
    the CHEAPEST → tie stronger → tie faster; else the strongest capable. Per-
    model RouterScore (Quality/Cost/Speed, tunable). kind/difficulty come from
    the orchestrator (task tool args, authoritative) or a heuristic classifier
    (fallback); vision forced when an image is attached; search via cue phrases.
    Candidates = catalog models on credentialed + allowed providers (cross-
    provider opt-in via route_providers). /route on|off, 'route' status tag,
    each routed choice noted; respects the failover window; manual /model wins.
    Auxiliary vision routing shipped (d14e50a): an image attached on a
    non-vision model routes to a vision-capable one even with the router OFF
    (capability need overrides the toggle).)*
11. **Hooks** — *(shipped (small/event-surface first, per the user): internal/
    hook — user commands triggered on EXPOSED lifecycle events (session_start/
    stop/resume, tool_start, tool_result, turn_done, note); each hook gets a
    small JSON payload on stdin; fire-and-forget, 30s-bounded, best-effort.
    Config hooks.json (array or {hooks:[…]}), project-or-user. Memory-as-a-hook
    (eigen dream on session_stop) shown in docs/hooks-example.json. More hook
    points added when a concrete need arises.)*
12. **Sub-agents** — *(partially shipped: depth-bounded `task` tool; expand: named roles, parallelism)*.
13. **Ultraplan** — dozens of in-depth sub-agents driven by one big plan ahead.
14. **Ping** — *(shipped cf8d2de: terminal bell + optional notify_cmd on
    approval-needed and long-turn-finished)*
15. **AGENTS.md integration** — *(shipped: the repo's AGENTS.md (also
    .eigen/AGENTS.md, CLAUDE.md), nearest-first walking up to the .git root, is
    injected into the system prompt as repository guidance — distinct from
    learned memory; capped per file.)*
16. **tok/s in & tok/s out measurement** — *(shipped 84f13b1 + 9bc7001:
    output tok/s live + last-turn; input-side tok/s now from REAL provider
    usage (Usage{InputTokens,OutputTokens} on every backend) shown as
    '<in>·<out> <rate> tok/s', estimate fallback when unreported)*
17. **Observability for long-term learning** — *(shipped: internal/observe —
    structured JSONL activity log at ~/.eigen/observe/events.jsonl (metadata
    only: kind/step/tool/is_error/text+result lengths, not content). Logger.Wrap
    composes onto the agent EventSink in both headless and TUI paths; config
    `observe` (default on). Feeds future dreaming/learning + debugging.)*
18. **`/` config for most things** — *(shipped: /config shows the settings
    table; /config <key> <value> validates + persists to ~/.eigen/config.json.
    Live-session knobs stay /model /perm /effort /search /goal /loop.)*
19. **Auto-discovery of newly available models** — *(shipped: `eigen models`
    lists the catalog, then probes every credentialed provider's listing
    endpoint (Anthropic /v1/models, Bedrock inference-profiles, grok/glm/llama
    /models) and reports models not yet in the catalog. Read-only; new ids are
    usable immediately via --model/-/model.)*
20. **Image integration using other models** — *(shipped: vision input end to
    end. llm.Image + Message.Images, catalog Vision flag (Claude family),
    Converse + native Anthropic image-block serialization; the TUI attaches
    referenced/dropped image files (png/jpeg/webp/gif, ≤8MB) when the active
    model supports vision, with a 'vision' status-bar tag. Note: this attaches
    images to the MAIN model when it is vision-capable; routing images to an
    AUXILIARY vision model when the main one lacks it shipped (d14e50a): an
    image on a non-vision model forces a route to a vision-capable model.)*
21. **Drag-and-drop of files** — *(shipped: a dropped file arrives as a
    bracketed paste of its path; eigen normalizes it (strips file://, unquotes,
    percent-decodes, handles multi-file drops) into clean path tokens the model
    reads like an @file mention. Plain pasted prose is untouched.)*
22. **Image copy-paste** into the conversation — *(shipped: ctrl+v/alt+v grabs
    a raw image from the system clipboard (wl-paste/xclip/pngpaste, png/jpeg/
    webp/gif) and stages it for the next message on vision models; clipboard.
    PasteImage. Combined with #20/#21, both image PATHS and raw clipboard
    images now attach.)*
23. **Integrate other model types efficiently** — embedders, diffusion, mamba, etc.,
    to offer non-LLM solutions where they fit.
24. **Iterative planning (Anthropic × GPT, head-to-head)** — both vendors plan
    TOGETHER and adversarially: one drafts a plan, the other critiques/counter-
    proposes, iterate until convergence (or a bounded number of rounds), then
    execution starts from the merged plan. Builds on the router's tier-3 pair
    (gpt-5.5 strict vs opus design) — their disagreement is signal: strictness
    catches handwaving, design sense catches over-rigidity. Likely surface:
    /plan or an ultraplan (#13) phase; needs convergence criteria + a merge step.
25. **Cross-vendor reviewer — GPT reviews Claude, Claude reviews GPT, always** —
    *(shipped: llm.VendorOf + CrossReviewer (always the OTHER vendor; grok/glm
    get the strict GPT reviewer; picks the strongest available), llm.Review-
    Artifact (critique prompt framing the author's vendor + focus), the `review`
    tool the model invokes with an artifact, and goal_achieved judging now
    defaults to a CROSS-VENDOR judge (GPT judges Claude's claims and vice
    versa). EIGEN_JUDGE_MODEL still pins a specific judge. The iterative-planning
    loop (#24) will reuse this as its critique step.)*


## Tier 8 — eigen the app (the TUI you live in)

Eigen upgrades from "a chat you open in a directory" to a full TUI **app** —
what nvim is to editing, eigen is to working with agents. Not just for code:
the thing you use for everything.

**Entry modes:**
- `eigen` (bare, no path) → opens the APP: a home dashboard, not a chat.
- `eigen .` / `eigen <path>` → opens a chat session rooted at that project
  (today's behavior, preserved).
- `eigen --resume/-c` → straight into the resumed session.

**The app is paged** (like nvim dashboards / lazygit panels). Pages, each a
first-class surface, reachable by keys and a command palette:
- **Home** — greeting, quick actions, the proactive feed (#6: offered actions
  from memory + project + GitHub scanning), recent sessions/projects.
- **Projects** — every project eigen knows (discovered from sessions, config);
  each project gets ITS OWN page: its sessions, its memory, its feed, quick
  "new session here".
- **Sessions** — all sessions across projects; resume/inspect/delete/export.
- **Crons** — scheduled/automation runs (#5): list, status, last result, edit.
- **Config** — the /config surface as a page (view + edit defaults).
- **Plugins** — plugins.json + MCP servers: status, tools, enable/disable.
- **Skills** — installed skills, preview, add/remove (the /skills surface).
- **Models** — catalog + discovery (eigen models), router tiers, availability.
- **Providers** — credential status per provider, default model, budgets.
- **Memory** — global + project memory: view, edit, consolidate, backups.

**Layout & UX:**
- Side tabs (left rail): running sessions + pages — switch instantly; running
  sessions show live status (working/idle/needs-approval) at a glance.
- **Daemon/view architecture (the real shape of the app):** the REAL app is a
  long-lived background process (eventually with a tray presence) that OWNS the
  sessions — each agent loop, its checkpoints, its small-model triggers, its
  per-session state, all live in the daemon. Terminal windows are VIEWS: they
  attach to the daemon, render, and interact — nothing more.
  - A session keeps running whether or not any window shows it.
  - Two windows can attach to the SAME chat (mirrored, both live) or to
    different chats.
  - Each chat is a whole, as today — its own tools rooted in its project dir,
    project memory, goal/loop, approvals — sharing only the truly global
    things (global memory, config, session store, small-model jobs).
  - Approvals broadcast to attached views; any view can answer.
  - Build order: ✅(1) buildSession per-directory builder; ✅(2) internal/daemon
    — host + Unix-socket protocol (list/new/attach/input/events/approve, event
    replay on attach, PID lifecycle, `eigen daemon status|stop`, shutdown
    watchdog); ✅(3) attach view; ✅(4) app live page + rail glyphs (●○◆✗),
    attach/new/interrupt/stop from the app; ✅(5) approval broadcast — gated
    sessions block, any view answers y/n, 10-min timeout denies (fail closed).
  - ✅ **SHIPPED BEYOND THE PLAN (the flip + parity):** interactive `eigen` IS
    a daemon session by default (auto-start daemon, EIGEN_NO_DAEMON escape);
    `eigen attach` runs the RICH chat TUI over a Backend seam (chat.Local /
    chat.Remote; internal/view deleted); sessions are DURABLE
    (~/.eigen/daemon/sessions/*.jsonl + meta, Host.Restore on start, verified
    kill -9); /rebuild restarts the daemon on the new binary and reattaches
    (sessions survive); full remote parity (/clear /resend /model images
    effort/search tool-args /resume-to-history, daemon errors → failover);
    app lists daemon sessions (enter = attach, never fork); small-model
    auto-titles; obs+hooks run daemon-side; tools root at the SESSION dir
    (multi-project daemon correctness); `eigen daemon install|uninstall` =
    systemd user-unit autostart + credential snapshot.
  - REMAINING: tray presence (maybe drop: terminal-first + live rail covers
    it). Chat-as-a-page SHIPPED (3fb907f): alt+s in-window session switcher.
- A command palette (fuzzy) for everything; consistent keybindings.
- DESIGN BAR: highly informative, subtle, "a perfect touch of a designer" —
  restrained color, clear hierarchy, no clutter; every effect informative,
  nothing decorative for its own sake.

**Build order (iterate, ship each slice):**
1. ✅ App shell: page router + side rail + home page; `eigen` (bare) opens it.
2. ✅ Sessions page (resume/attach/delete/export across projects; daemon
   sessions first-class) + Projects page (grouping + per-project sessions).
3. ✅ Config / Skills / Models / Providers / Memory pages (read-only v1 —
   EDIT affordances still open: config edit, plugins enable/disable, memory
   edit/consolidate from the page, crons edit).
4. ✅ Multi-session: live page + rail glyphs, and in-window switching
   (3fb907f): alt+s hops between daemon sessions, h returns to the app — one
   window, sessions keep running (Detach never interrupts a daemon turn).
5. ✅ Crons page (systemd user timers via --output=json + crontab; ACTIONS:
   space stop/start a timer, t trigger now — 53209c4).
6. ✅ Proactive feed (#6): internal/feed scans git (uncommitted/unpushed per
   project), project memory (stated intents), and GitHub (gh review-requests +
   assigned issues); each item carries a ready Task + project Dir. Home 'act
   on' section + per-project feed; enter = open a chat rooted there with the
   task pre-submitted. Cached (~/.eigen/feed.json, 10-min TTL), async refresh.
7. ✅ Plugins page (mcp/plugins/lsp/hooks, both scopes, read-only).

## Notes / grounding
- read-aloud tool the user has: `readd` (espeak-ng/piper) at `~/projects/tfqol/readd`.
- skills format = Claude Code SKILL.md (YAML frontmatter `name`,`description`[,`allowed-tools`] + markdown body).
- permission postures: `gated` (asks for mutating tools) / `auto`.

## Configuration & extension reference (as shipped)
Tools (20): read, list, glob, grep, symbols, tree, diff, write, edit, multiedit,
apply_patch, move, bash, fetch, todo, skill, memory, task, goal_achieved,
websearch (when a backend is configured) (+ plugins + MCP + LSP).

Files (under `~/.eigen/`, plus project-local `./.eigen/`):
- `config.json` — defaults: `provider`,`model`,`perm`,`max_tokens`,`tts_cmd`,
  `skills_dirs`,`dream_on_idle`,`idle_minutes`
- `skills/<name>/SKILL.md` — discovered skills (also `EIGEN_SKILLS_DIRS`, colon-sep)
- `plugins.json` — external-command tools `[{name,description,parameters,command,readonly,timeout_seconds}]`
- `mcp.json` — `{"servers":[{name,command,env}]}` (stdio MCP servers)
- `memory/global.md` — cross-project durable notes (working style, global rules)
- `memory/<project>.md` — per-project durable notes (auto-injected, appended by the memory tool / dreaming)
- `sessions/*.eigen.jsonl` — autosaved sessions · `exports/*.md` — `/export`
- `.env` — credentials

Env vars: `EIGEN_PROVIDER`, `EIGEN_PERMISSION`, `EIGEN_MAX_CONTEXT_TOKENS`,
`EIGEN_TTS_CMD`, `EIGEN_CLIPBOARD_CMD`, `EIGEN_SKILLS_DIRS`, `EIGEN_LLAMA_BASE_URL`, `EIGEN_SRC`.
Web search (enables the `websearch` tool): `TAVILY_API_KEY`, `BRAVE_API_KEY`, or
`EIGEN_WEBSEARCH_URL` (+ optional `EIGEN_WEBSEARCH_KEY`; `EIGEN_TAVILY_URL`/`EIGEN_BRAVE_URL` override the endpoint).
LSP: `.eigen/lsp.json` / `~/.eigen/lsp.json` — `{"servers":[{name,command,extensions,env,language_id}]}`.

CLI: `eigen [task]` · `-p` print · `--resume/-c` · `--list` · `--list-skills` ·
`--list-tools` · `eigen dream` (reflect into memory) · `eigen models` (discover) ·
`eigen memory <show|backups|consolidate> [--global]` ·
`eigen skill add <path | owner/repo[/subdir][@ref]> [--name X] [--force] [--overwrite] [--no-scan]` ·
`eigen skill list`. Installing a skill (from a path or GitHub) scans its content
with the small "haiku" model for instructions dangerous for the agent to follow;
a RISKY verdict aborts unless `--force`. Small-model selection: a local llama if
`EIGEN_LLAMA_BASE_URL` is set, else `EIGEN_SMALL_MODEL` (default
`us.anthropic.claude-haiku-4-5` on converse). Used for session titling, dreaming,
and skill scans.

TUI commands: /help /resume /save /export /clear /rename /sessions /model /perm /skills /tools
/find /copy /read /rebuild /quit · keys: `/` commands · `@` files · ↑↓ select ·
tab/click expand · while running: enter queues · esc interrupts.

TUI features: steer+queue, mouse click-to-expand + wheel, slash & @file
autocomplete, rich tool blocks + live status, LCS diffs, live plan panel (todo
tool), status bar (model·perm·~ctx), read-aloud, clipboard, gated "always allow".
