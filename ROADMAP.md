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
12. **Sub-agents** — *(partially shipped: depth-bounded `task` tool; foreground routing/model override/background handles landed in task-v2; expand: named roles, parallelism, and escalation)*.
    - [ ] Background task escalation: if a background task fails/stalls/declares it is underpowered, automatically re-run or hand off to a bigger model (not necessarily back to the orchestrator) and merge the final report back through `task_status`.
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

## Tier 9 — the chat IS the app (chat-window chrome)

The chat window is still a "chat"; the app shell got all the super-app
treatment. This tier makes the **chat window itself an arm of the app**:
chrome (header + rails + panels) wrapped around the transcript, everything
**keyboard-first AND clickable**, so a session is a workstation, not a REPL.

The user's words: a side list of running sessions to jump to; a header with
general actions; clickable config params on the status line and the title; a
side view of the diff of files edited in the last turn; "and more".

**Enlarged vision (what he'll want, built ahead):**
- Direct manipulation: status segments, title, header actions, rail rows, and
  changes-panel files are all click targets — but every click has a keyboard
  AND palette equivalent (mouse is additive, never required; tmux/zellij eat
  some ctrl/alt keys, so a non-modifier path always exists).
- Spatial awareness: you see the OTHER running sessions (rail) and what THIS
  turn changed (changes panel) without leaving the conversation.
- One action layer: keys, slash commands, clicks, and the (future) palette all
  dispatch the SAME action ids through one validated handler — no second code
  path, no click that bypasses "can't run mid-turn" checks.
- Designerly restraint: chrome is subtle, single-purpose, collapsible, and
  degrades cleanly on narrow terminals (panels drop right-first, then the rail,
  then the header compacts) — never clutter.

**Wave 0 — geometry + action foundation (prerequisite, no visible chrome).**
✅ SHIPPED (e28f552). internal/tui/layout.go = named rects (header/plan/
transcript/spinner/comp/input/status/leftRail/rightPanel) via computeLayout()
mirroring relayout's sizing; hitTest(x,y)→{region,action,localX,localY} with
explicit z-order (chrome > rails/panels > input > comp > transcript > plan),
widths in cells (ansi.StringWidth), ANSI-stripped. internal/tui/action.go =
ONE actionRegistry (actionID→{label,enabled,run}); keys/slash/clicks all go
through m.dispatch, which gates disabled/idle-only actions identically and
notes why a blocked action did nothing. internal/tui/overlay.go = a reusable
confirm/text prompt for actions that must not fire silently. Existing mouse
features rebase through the region mapper; chrome_test.go covers layout
stacking + hit regions + the gate.

**Wave 1 — clickable status line (pilot of the foundation).**
✅ SHIPPED (e28f552). Each status segment carries an action: model→picker,
perm→confirm (not a blind toggle), effort→cycle, search→cycle, route→toggle,
context→compact-confirm, read-aloud→toggle. statusBarLayout() packs segments
with cell-width column ranges; statusActionAt maps a click to its segment.
Compact/config are idle-only (refused mid-turn with a hint). Live-verified.

**Wave 2 — header bar (one line, above the plan).**
✅ SHIPPED (707e1e6). internal/tui/header.go: title + dim project breadcrumb
left (truncated), `[home] [sessions] [+new] [config]` right (accent when
enabled / dim when disabled). Clicking the title opens the rename prompt (the
SINGLE rename surface). topHeight includes the header so screenToContent/
toggleAtRow rebase for free. Live-verified.

**Wave 3 — left session rail (high value; before the diff panel).**
✅ SHIPPED (9198e97). internal/tui/rail.go: a 22-col column of daemon sibling
sessions with status glyphs (●○◆✗), current marked ·; click a row hops the
window via the EXACT switcher path (hopToSession → switchTo + quit → Run's
Detach keeps the daemon turn alive; no second switching path). Daemon-hosted
backends only, terminals ≥80 cols (else hidden, reachable via alt+s /
[sessions]); ~1.2s railTick poll. The transcript origin shifts right by the
rail; screenToContent rebases x. /rail toggles. Live-verified (a 2nd window's
session appeared in the rail within seconds).

**Wave 4 — right changes panel (reduced first cut, then deepen).**
✅ v1 SHIPPED (d384557). internal/tui/changes.go: a CHANGE INDEX of files
touched by the last edit-producing run (runs split at user messages — latest
segment with edits wins; survives streaming/resume/retries), +adds/−dels per
file, click/key jumps to the tool block. Same-file edits aggregate;
apply_patch splits per +++ header. Memoized by a cheap transcript signature
(not recomputed per View()). Degrades right-FIRST (hides before the rail when
the transcript would drop below 40 cols). /changes toggles. Live-verified
(an edit turn showed 'note.txt +1 −1').
- [x] v2 = inline diff rendering in the panel — SHIPPED: each file row is
      followed by its colored diff (the SAME renderDiff path as transcript
      inline diffs; apply_patch details filtered per file via patchSection).
      View memoized by transcript-sig + panel width (changesView lines +
      per-line file map); wheel over the panel scrolls it (clamped); a click
      on ANY diff row jumps to that file's tool block.

**Wave 5 — and more (captured; build after the foundation proves out).**
- [x] command palette (fuzzy, ctrl+k) over the action registry — SHIPPED
      (67dcf5f, internal/tui/palette.go): launches every registry action
      (validated via dispatch) + chrome toggles + common slash commands; fuzzy
      filter (substring > subsequence), arg-slash entries prefill the input,
      disabled actions dim. Live-verified (ctrl+k → 'config' → config panel).
- [x] resizable panel layout — SHIPPED: the rail's separator column and the
      right panel's gutter column are grabbable edges (press→drag→release);
      widths clamp per-panel (rail 14–44, right 24–100) AND to the
      transcript's 40-col minimum; the embedded terminal PTY reshapes on
      resize. Keyboard parity: widen/narrow palette actions (4-col steps).
      Widths are per-window (not yet persisted).
- [ ] notifications/approvals tray; persisted panel widths; multi-pane (two
      transcripts side by side).

## Tier 10 — the app shell, clickable + structural (mouse parity + framing)

The chat window became a clickable, framed workstation (Tier 9). The APP SHELL
(internal/app — the home dashboard: rail of pages + live sessions, per-page
content) is still **keyboard-only and FLAT** (no mouse, no borders). So clicking
the chat's `[home]` button drops you into a surface you can only drive by
keyboard — an odd seam. This tier closes it: the shell gets **mouse parity** and
**structural UI** (edges, frames, bars, side bars), and **embraces being wider
than a normal terminal** (the user accepts + wants the wide layout).

**Principles (carried from Tier 9 + the cross-vendor review):**
- Geometry ownership FIRST: one app layout pass computes named rects
  (title/rail/content/status, optional right inspector) at every breakpoint;
  render AND hit-testing read the same rects so they never drift. Pages receive
  the INNER content rect (post border/padding), not the outer panel size.
- Heterogeneous pages → page/component-EMITTED hit regions, not a shell-side
  `rowAt()`. The shell owns global chrome hits (rail page, live entry, title,
  footer); each page owns its internal hits (feed rows, session rows, config
  keys, detail panes). A shared list/viewport helper emits visible-row regions
  so pages don't hand-roll y-offset math.
- Hit regions carry STABLE targets ({page/component/itemID/action}), not raw
  indices (lists change between renders; a stale click must never panic).
- Mouse is additive: every click has a keyboard equivalent. Single click
  selects/focuses; Enter (or click on the already-selected row) opens; explicit
  buttons activate immediately. Ignore motion; dispatch on press; route the
  wheel by the region under the cursor.
- Lipgloss frame math is explicit (GetHorizontal/VerticalFrameSize; the
  JoinHorizontal spacer is a real column); rendered lines never exceed their
  panel width (tested) or terminal wrapping breaks hit-testing. Display width,
  not len().
- Wide-terminal UX uses breakpoints: narrow = compact/no rail; normal = rail +
  content; wide = rail + content + right inspector panel; cap only prose/forms,
  never waste the width by centering a narrow dashboard.

**Wave 1 — central layout + structural framing.**
✅ SHIPPED (bd0f8c6). internal/app/layout.go: computeLayout() → named rects
(title/rail/content/status + optional right inspector) by breakpoint
(narrow/normal/wide≥130), 1-col gutters, frame math via GetH/VFrameSize; pages
receive the INNER content rect. app.go View framed: title breadcrumb bar,
bordered rail + content (+ inspector), status bar; narrow drops borders.
layout_test.go asserts rects at 60/80/120/160/220 cols + line-width-≤-terminal.
Live-verified.

**Wave 2 — mouse foundation + top-level chrome hits.**
✅ SHIPPED (9232095). internal/app/hit.go: hitTest(x,y)→{region,target}
z-ordered over the layout rects; rail row math mirrors railContent; STABLE
targets (Page / live session ID). tea.WithMouseCellMotion; handleMouse — click
a rail page switches, a live entry attaches, wheel over content scrolls the
list; motion/right-click ignored; no-op while the palette is open. hit_test.go
covers it. Live-verified (clicked rail → skills → home).

**Wave 3 — page-local hit maps + clickable content.**
✅ SHIPPED (4063669). internal/app/list.go clickMap: the renderer records each
selectable item's content-local line during view() (clicks.mark(lineCount(out),
idx)) — robust across sectioned/variable-height pages, no analytic row math.
Per-page clickAt (home/sessions/projects/live/config): single click selects,
click-again activates (open chat / resume / attach / drill / open field); Enter
unchanged; bounds-checked (stale click = safe no-op). click_test.go covers it.
Live-verified the FULL loop: rail click → session-row click opened the framed
chat → [home] returns — mouse round-trip, no keyboard.

**Wave 4 — wide-terminal richness (after the spine proves out).**
The wide breakpoint already reserves a right inspector rect (Wave 1) with a
placeholder. Remaining: fill it with real per-selection detail (session/
project/config), wider rail labels, more table columns where width allows.
- [ ] right inspector content (selected item detail) + wider-breakpoint density.

## Tier 11 — superapp panels: closable, tabbed, real content, navigable

Tiers 9/10 made both windows clickable + framed. This tier makes the chrome a
real **workbench**: panels you open/close, panels with TABS showing live
content (git status, an in-session terminal), a left rail that organizes
sessions BY PROJECT with liveness, and consistent sub-page back-navigation —
every bit driven by BOTH keyboard and mouse. (User's words, captured verbatim
below so nothing is lost.)

**The asks (verbatim → spec):**
1. **Sub-page back-navigation everywhere.** "i click config, i want to go back
   from config." Today the chat's `/config` panel and the app's drill-ins
   (config dropdown, skills preview, project sessions) have ad-hoc esc handling
   and NO click affordance. Spec: every sub-view has a visible back control
   (a framed `‹ back` / breadcrumb segment) that is clickable AND `esc`/
   `backspace`; a nav STACK so nested drill-ins pop one level at a time.
2. **Header inside a border.** The chat header is a bare line; frame it (a
   bordered top bar matching the app shell's title bar) so the window reads as
   structured chrome, not floating text.
3. **Status-line config items clickable to set in place.** Extend Tier 9 Wave 1:
   clicking effort/search/route/perm doesn't just cycle — where it makes sense
   it opens an in-place picker right at the segment (a small popover above the
   status bar) so you SET the value, not blind-cycle. Keyboard parity via the
   existing pickers.
4. **Both side panels closable.** Each panel (left rail, right panel) gets a
   clickable `[x]` in its header + a key; closing reflows the transcript.
   Reopen via header buttons / palette / key. (Tier 9 has /rail /changes
   toggles — make them a visible, clickable close control.)
5. **All panels show real content** (no placeholders) — see 6 & 7.
6. **Right side = two OPTIONAL TABS** (pick what's shown, or close):
   - **git** — branch, ahead/behind, staged/unstaged/untracked counts, the
     working-tree diff (reuse internal/feed/git.go helpers + the diff tool /
     renderDiff). Read-only v1; actions (stage/commit) later.
   - **terminal** — "dead simple to run a command in session": a one-line input
     that runs a command in the session's dir and shows output in the panel.
     Bounded, non-interactive v1 (run → capture stdout/stderr → show); rooted
     at the session dir. (Not a full PTY — that's a later, bigger bet.)
   - (the existing "changes (last turn)" panel becomes a third tab or folds
     into git — decide during design.)
7. **Left rail = sessions grouped per PROJECT**, with liveness:
   - group rows by project dir; a project header is collapsible.
   - **light up projects that are open somewhere** (a window/view attached).
   - **loading/working mark** for sessions mid-work (agent looping or a turn
     running) — a spinner glyph, distinct from idle ○.
   - click a project header to collapse/expand; click a session to hop; all
     keyboard-navigable too.
8. **Keyboard + click parity for ALL of it.** "its a superapp."

**Decomposition (review-adjusted — minimal foundations first, no framework
astronautics):**
- **Wave 0 — panel frame + close controls + one-level back.** Do NOT build a
  browser-like nav stack yet. Build the minimal shared primitives:
  - reusable panel header (`title`, optional tabs later, clickable `[x]`), hit
    rects match rendered labels, close action has keyboard + palette parity;
  - one-level back affordance (`‹ back`, esc/backspace/alt-left + click) for
    subviews; focus moves deterministically when a panel closes;
  - layout tests at 60/80/100/120/160 cols and short heights.
  Apply first to the existing right changes panel and left session rail (close
  visibly; reopen via /rail, /changes, header/palette), plus the chat config
  panel's visible back control.
- **Wave 1 — bordered chat header + consistent sub-page back.** Frame the chat
  header (account for the extra height in computeLayout/hitTest) and apply the
  back affordance to app subviews (project drill-in, skills preview, config
  dropdown/editor) without overbuilding a global stack.
- **Wave 2 — right panel TABS skeleton + git tab (read-only, cheap first).**
  Tab bar in the panel header (clickable + key cycle), selected tab scoped to
  the session/project. Start with real but cheap git content: repo root/current
  branch, ahead/behind, staged/unstaged/untracked counts, short diff stat (not
  full diff first). Handle no repo / detached head / deleted cwd.
- **Wave 3 — REAL embedded terminal tab (PTY + VT emulator).** ✅ SHIPPED. Not
  a command-runner — a genuine terminal, the standard recipe everyone uses
  (creack/pty + charmbracelet/x/vt SafeEmulator), so interactive programs
  (vim/less/top/htop) work because they get a real TTY. internal/tui/termpanel.go:
  lazy `$SHELL -i` on a PTY sized to the panel (creack/pty starts it in its own
  session via Setsid → killable process group), a fresh emulator per generation,
  a reader goroutine (PTY→emulator, never touches model state) + a waiter
  (cmd.Wait reaps the child → termExitedMsg), a single gen-guarded repaint tick
  (70ms; never multiplies; stops when hidden/exited), resize ONLY from Update
  (ensureTermSize on window-resize/tab-switch/tick, never View). Keys: when the
  term tab is focused it grabs ALL keystrokes incl. esc/ctrl+c (encoded to PTY
  bytes — arrows/ctrl-chords/runes/alt) so vim and job control work; ctrl+g
  RELEASES focus back to the chat (shell keeps running); click focuses. Teardown
  closes the PTY (SIGHUP) then SIGTERM→SIGKILL the group; the shell lives in the
  VIEW process (one per window), torn down on window close — never accumulates
  in the daemon. /term opens it, ctrl+r cycles changes→git→term. Cross-vendor
  reviewed (caught: needed cmd.Wait, fresh-emulator-per-gen, single tick chain,
  resize-out-of-View, pid>0 guard, gentle-then-hard kill, no redundant Setpgid)
  — all fixed. Tests cover encodeKey/focus-routing/lifecycle + a REAL forked-zsh
  pipeline (`printf … | wc -l` → emulator renders `3`), all `-race` clean.
  - [ ] later: scrollback view, copy-from-terminal, per-terminal resource caps,
        graceful daemon-side terminals if windows ever share one.
- **Wave 4 — left rail grouped by project + liveness.** SHIPPED. Sessions
  group under collapsible project headers (only when they span >1 project dir
  — a single project stays flat). Header click toggles collapse; collapsed
  headers show `▸ name (n)` plus the most-urgent status glyph so hidden
  activity stays visible. Working sessions animate a braille spinner (the
  rail poll speeds to 300ms while anything is working, back to 1.2s idle).
  Projects with a window attached anywhere highlight (SessionInfo gained
  `views` — attached view count — over the wire). Renderer and click hit-test
  share ONE railRows() row model so geometry can't drift. Keyboard parity:
  actRailCollapse (palette: "collapse/expand rail projects") collapses or
  expands all.
  - [ ] later: per-header keyboard cursor in the rail, drag to reorder.
- **Wave 5 — status-line in-place setters + polish.** Segment popovers for
  effort/search/route/perm; consistent close/reopen controls; palette entries
  for every new surface.

## Tier 11.5 — chrome consolidation: no top header, left command sidebar
User proposal: the current top header duplicates controls and consumes vertical
space. Move chrome into a consistent left-side command/sidebar surface and keep
the transcript/input cleaner.

Target layout:
- **No separate top header** in the normal chat view. The transcript starts at
  the top of the content area (or under only transient overlays/todos when
  expanded).
- **Top-left opener / command rail** owns navigation and metadata:
  - session title (rename affordance)
  - cwd / project breadcrumb
  - dropdown sections for: todo list, sessions, config/settings, maybe tools
    and subagents
  - left-sidebar open/close state and project/session list (current rail folded
    into this instead of a separate top header + rail)
- **Status line moves up** into the left sidebar/top-left chrome: model,
  provider, perm, effort, search, route, token budget, current turn tok/s.
  These become in-place setters/dropdowns from the same action registry.
- **Right-sidebar opener** is part of the same chrome language (not hidden in a
  slash command): visible toggle for changes/git/term/subagents panel.
- **Cleaner input area:** input stays focused on composition only; fewer global
  hints/status fragments below it. Hints should be contextual and brief.
- **Consistent dropdown model:** todo dropdown, sessions dropdown, config
  dropdown share one overlay/list component and keyboard/mouse behavior.

Implementation waves:
1. **Layout experiment behind a toggle** (`/chrome` or config flag): render a
   compact left command sidebar while keeping current header path available.
   Do this with computeLayout rectangles first; never special-case coordinates.
2. **Move header actions into sidebar:** home/sessions/+new/config, title click,
   cwd, left/right panel toggles. Preserve keyboard shortcuts and palette.
3. **Status relocation:** move model/perm/effort/search/route/context from the
   bottom status bar into sidebar rows with click/popover setters. Bottom bar
   shrinks to turn-specific ephemeral info only (or disappears when idle).
4. **Dropdown unification:** implement shared dropdown component for todos,
   sessions, config/settings, and later subagents. No separate one-off pickers.
5. **Remove old header path** after size-sweep + live verification proves the
   sidebar chrome at narrow/short sizes.

Constraints: geometry-owned-first; one action registry; every mouse surface has
keyboard/palette parity; sidebars must degrade gracefully under 80 cols; size
sweep must include headerless+sidebar mode before defaulting it.

**Constraints (unchanged):** geometry-owned-first (computeLayout rects, render
+ hit-test share them), one action layer (no click bypasses a key's gate),
mouse additive + full keyboard parity, restrained design, degrade on narrow
terminals, each wave ships with tests + live verification + a commit; keep
build/vet/test/staticcheck green. Async panel data (git/terminal) must be
keyed by session/project and stale-safe. The terminal IS a real PTY (Wave 3
shipped it — creack/pty + a VT emulator, the standard recipe; we don't reinvent
solved problems) running in the per-window VIEW process so it's torn down on
window close.



## Tier 12 — subagent observability
Goal: make delegated work visible, not just a final `task_status` result. The
orchestrator should be able to see *what subagents are doing now* and recover
context without guessing.

- [ ] **Subagent activity surface.** A live `agents`/`tasks` pane (or right-panel
  tab) showing active foreground/background task-tool runs: id, routed/override
  model, kind/difficulty, status (queued/running/done/error), current tool,
  last note/output excerpt, elapsed time, token use, and parent session.
- [ ] **Streaming child events.** Subtasks currently suppress most child events
  except the route note/result; add a sanitized event bridge so parent TUI can
  tail child progress without dumping the whole child transcript into the main
  chat. Background tasks should append the same progress to `~/.eigen/tasks`.
- [ ] **Controls.** Open child transcript, collect result, cancel/kill, promote a
  child into a full session, retry/escalate failed/stalled work on a larger
  model. All controls must go through the action registry + command palette.
- [ ] **Notifications.** Parent session gets concise state-change notes (started,
  waiting on approval, tool error, done) with a clickable/task_status handle; no
  spam for every token.
- [ ] **Persistence/restart.** Background tasks survive daemon/view restart well
  enough to inspect/collect; stale running tasks are marked `lost` with the last
  known transcript snapshot.

## Debt / bugs
- [ ] **Untitled daemon sessions still appear.** Several persisted daemon meta files
  have empty `title` even when the transcript has a user message (`s4/s5/s7/s9`
  observed). Likely title generation only runs on the agent Persist hook for
  newly appended messages, so restored/imported/command-only/failed-title
  sessions can remain nameless forever. Fix direction: daemon/app listing should
  backfill titles for untitled sessions from transcript head (cheap heuristic
  immediately; async small-model title when available), persist the meta, and
  expose title-failure logs/status so this is diagnosable.

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
