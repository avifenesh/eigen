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
- [x] v3 = diff lines WRAP, not truncate — SHIPPED. Today panel diff lines are
      hard-truncated at the panel width (ansi.Truncate + "…"), so resizing or
      a narrow panel hides the tail of every long line. Wrap long diff lines
      within the panel (continuation rows, preserving the +/− gutter color and
      the per-row file map for click-to-jump), and when the panel is RESIZED
      (drag or widen/narrow actions) re-wrap to the new width instead of
      truncating. Same applies to transcript inline diffs at narrow widths —
      wrapping must keep band alignment exact (no row may exceed its slot;
      tab-expansion lesson from a958f99 applies).

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
- [x] ~~notifications/approvals tray~~ (SHIPPED 0ab6095: alt+n / /tray — "what
      needs me" across sessions); ~~persisted panel widths~~ (SHIPPED 032ebd2:
      ~/.eigen/ui.json). Multi-pane (two transcripts side by side) — BACKLOG
      (user: not needed now).

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
- [x] right inspector content (selected item detail) — SHIPPED (36853f5):
  per-page key/value detail of the selected row (sessions/models/providers/
  crons/plugins/projects/skills). Wider-breakpoint density still open.

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
1. [x] **Layout experiment behind a toggle** — SHIPPED: `/chrome` (+palette
   "toggle sidebar chrome", actSidebarToggle). sidebarVisible() gates on the
   rail width threshold; headerHeight()=0 in sidebar mode so every consumer
   (computeLayout, topHeight, hit-test) rebases for free; railWidth() reuses
   the rail column for the sidebar (works for local chats too); the band
   renders sidebarLines instead of railLines. ONE row model (sidebarRows)
   shared by renderer + click hit-test (same convention as railRows).
2. [x] **Move header actions into sidebar** — SHIPPED in the same slice: title
   (click=rename), cwd breadcrumb, ⌂ home / ⇆ sessions / + new / ⚙ config nav
   rows, ◨ right-panel toggle (lit when open), session rail folded in below
   a "sessions" mini-header (project headers collapse, session rows hop).
   Narrow terminals keep the classic header (honest note + pane stretch).
3. [x] **Status relocation** — SHIPPED: statusBarParts render as sbStatus
   sidebar rows (same styles, same click actions — model picker/perm/effort/
   search/route/compact); statusBarHeight()=0 in sidebar mode so the bottom
   bar is gone and the input sits clean at the bottom. The running spinner
   line stays (turn-specific ephemeral info).
4. [x] **Todos folded in** — the plan panel renders as a sidebar section
   (sbTodoHeader "plan (n/m)" + sbTodo rows, maxTodoRows cap); topHeight()=0
   in sidebar mode so the transcript starts at row 0. (Shared dropdown
   component deferred — sections proved enough; revisit if a surface needs
   real dropdowns.)
5. [x] **Default flipped** — sidebar IS the design (user-approved after live
   trial). sidebarVisible() = width ≥ 80 cols; /chrome toggle and sidebarOn
   state REMOVED; the classic header+status-bar survive only as the narrow
   (<80 col) fallback. All legacy chrome tests repointed at narrow widths;
   sidebar size sweep covers 60..160 × 6..40 including todos.

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

- [x] **Subagent activity surface.** SHIPPED as the `[tasks]` right-panel tab
  (`/tasks`, ctrl+r cycle, palette): background tasks with status glyph
  (● running / ✓ done / ✗ error / ⊘ canceled / ? lost), short id, elapsed,
  live tool + tool-elapsed, route/model, token use; click expands result/
  progress detail. v1 scope decision (cross-vendor review): background tasks
  only — foreground subtasks finish into the parent transcript anyway and
  records for them created collect/cancel semantics debt. Sidebar shows a
  `⚒ tasks N●` badge while delegations run (clicking opens the tab),
  refreshed by piggybacking the rail poll.
- [x] **Streaming child events.** SHIPPED as the sanitized event bridge: the
  background sub-agent's OnEvent updates the durable record (steps, last tool
  start/clear, sanitized note, done usage) — bounded by step count, never text
  deltas. The disk store IS the stream surface: the TUI (separate process)
  polls `~/.eigen/tasks` on a 2s tab tick; no daemon wire-protocol changes.
- [~] **Controls.** v1: expand/view result (click/enter on a done task), cancel
  (click `[cancel]` on an expanded running task → confirm overlay →
  `agent.RequestCancel` marker file the hosting daemon polls; cross-process).
  All through the action registry + palette (`actTasksTab`). Deferred: open
  child transcript in a viewer (path shown for lost tasks), promote to full
  session, retry/escalate on a larger model.
- [~] **Notifications.** Existing finish/FAILED note (the immediate completion
  event) + new `canceled` note; the tasks badge + tab make state visible
  without notes spam. Deferred: started/approval-wait notes (started is
  already echoed by the task tool's return value). Original intent (kept for
  the deferred slice): parent session gets concise state-change notes (started,
  waiting on approval, tool error, done) with a clickable/task_status handle; no
  spam for every token. Important clarification from live use: a note like
  `background task bg-… finished — task_status bg-… to collect` is already the
  immediate completion event; `task_status` is the result-collection/open action,
  not polling for whether it is done. UI should render this as `done → collect`
  (click/enter), not as a passive instruction to manually poll.
- [x] **Persistence/restart.** SHIPPED: BgRegistry.Get/List fall back to/merge
  disk records, so task_status finds tasks from previous processes;
  NewBgRegistry adopts stale state on start (durable `lost` line for records
  whose pid is gone — pid+host recorded at start; age beyond
  bgMaxRuntime+grace decides for old/foreign records) and prunes terminal
  tasks older than 7 days (state+transcript+marker; running tasks never
  pruned). Readers parse the LAST COMPLETE jsonl line (mid-append tolerant)
  and exclude `*.transcript.jsonl`.

## Tier 13 — session-list ergonomics (last-used, filter, search)
Goal: the session list is endless now (daemon sessions are durable and
accumulate forever) — finding "the session I was just in" or "that revuto
session from Tuesday" must be instant. Three surfaces show sessions and all
three need the same ergonomics: the app shell sessions page
(`internal/app/sessions.go`, flat list), the in-chat switcher (alt+s /
`/sessions`, `internal/tui/nav.go` + switcher view), and the project drill-in
page. The sidebar rail is exempt (grouped-by-project + collapse already serves
its purpose).

- [x] **Last-used first, verified.** Lists are nominally newest-first already,
  but audit what `Updated` actually means per source before trusting it:
  daemon rows use transcript-file mtime in unix SECONDS (`persist.go`)
  converted with `* 1_000_000_000` against store rows' unix-nano — subtle
  unit/skew bugs here surface as "wrong session on top". The session being
  viewed/last attached should rank above sessions merely touched by
  background persistence; consider a `LastAttached` timestamp in the daemon
  meta sidecar (set on attach, survives restart) so "last used by ME" beats
  "last written by the titler".
- [x] **Type-to-search.** `/` (or just typing) in the sessions page and the
  alt+s switcher starts an incremental filter over title + project dir +
  session id; reuse the palette's `fuzzyScore` (substring < subsequence)
  rather than a new matcher. Esc clears, enter opens the top hit. App-shell
  side must go through the `capturingInput()` gate (typing "q" must type q,
  not quit — the config-page editing pattern). Search narrows the SAME list
  the cursor walks (one row model; no parallel filtered copy drifting from
  clicks).
- [x] **Filters.** One keystroke cycles/toggles structured filters alongside
  free-text search: by project (current dir first — the switcher should
  default to "this project" when the list is long), by source
  (daemon/store/imported), by status (working/idle/approval — switcher
  already has the glyphs), and a recency cutoff ("last 7 days" default view
  with an explicit "show all N" tail row instead of an endless scroll).
  Filter state is per-surface and resets on close (no sticky invisible
  filters that make sessions "disappear").
- [x] **Row affordances at scale.** With hundreds of rows: show relative age
  ("2h", "3d") right-aligned, dim sessions older than the cutoff, and keep
  delete/export working on the filtered view (operate on the row's ID, never
  the visual index — the Tier 8 lesson: every listing surface must agree).

## Tier 14 — catalog capability correctness
Goal: the model catalog (`internal/llm/catalog.go`) is the single source of
truth for capability gating, and it is WRONG in places — most visibly Vision:
only Claude entries carry `Vision: true`, while in reality GPT-5.x is
multimodal and several grok/glm models accept images too. Wrong flags are not
cosmetic, they drive behavior: `HasVision` gates image paste (refuses with
"the active model has no vision support"), image attachment (extractImages is
skipped — images silently dropped), and the ONE top-level auto-route exception
(an image on a "blind" model forces a route AWAY from the user's chosen
orchestrator — today an image while on gpt-5.5 needlessly hops models).
`Search`/`Social` gate the router's kind targeting the same way.

- [x] **Probe, don't trust folklore.** DONE for Vision (2026-06-13, 256x256
  red PNG end-to-end per gateway): mantle gpt-5.5/5.4 SEE (Responses
  input_image; gpt-5.5 hit a transient 500 once); grok-4/grok-build/
  grok-code-fast-1 SEE (chat-completions image_url; xAI rejects <512px
  images); grok-composer-2.5-fast BLIND (real 400 "not supported");
  GLM coding gateway TEXT-ONLY (400 code 1210 on all models — the gateway,
  not the family). Required building the image plumbing first: mantle
  buildInput input_image blocks, openaichat chatPart image_url data URLs.
  Findings recorded as catalog comments.
- [x] **Audit every capability axis, not just Vision:** Vision probed
  (above); Reasoning/EffortLevels probed earlier. Search/Social PROBED
  2026-06-13: Live Search grounds only via the PUBLIC xAI API (XAI_API_KEY)
  — over the grok-cli OIDC proxy (the user's path, no key) search_parameters
  is deprecated and grok falls back to training data (returned a 2024 date);
  grok.go already correctly disables search on the proxy. GLM web_search
  likewise didn't ground. Findings recorded in the Search flag comment.
  ContextWindow/Cache are documented vendor SPEC values (200k/1M-beta Claude,
  256k/512k grok, 200k GLM; Cache on Anthropic+mantle) with detailed catalog
  comments — not folklore, no probe needed.
- [x] **Fail open on uncertainty for vision-attach, fail closed for routing.**
  DONE: llm.Vision(model) returns (has, known); paste/attach refuse only on
  a POSITIVE blind verdict (unknown ids attach and surface the backend's
  real error); the route-away exception fires only when known-blind.
  Original text:
  If a model's vision support is unknown (uncataloged id), prefer attempting
  the attach and surfacing the backend's error over silently dropping the
  image — silent drops are the worst failure mode. Routing can stay
  conservative (only route-away when the catalog POSITIVELY says blind).
- [x] **Keep capability tests honest.** DONE: vision-route fixtures use the
  probed-blind grok-composer-2.5-fast instead of gpt-5.5 (now sees);
  added TestUnknownModelDoesNotForceVisionRoute +
  TestKnownVisionModelAttachesImages. Original text: Router tests and tui vision-gate tests
  encode today's wrong flags as expectations (e.g. "image forces vision route"
  fixtures assume gpt is blind); update fixtures to use explicit fake catalogs
  rather than real ids so flag corrections don't silently flip test meaning.

## Tier 15 — voice for real: conversation mode, button-first
Goal: bring the user's ALREADY-BUILT conversation mode to eigen, better. The
reference implementation is `~/projects/codex-desktop-linux/linux-features/
conversation-mode/patch.js` (+ `read-aloud` Kokoro backend) — a complete,
battle-tested design: RMS VAD with trailing-quiet auto-submit (~1.8s quiet,
capped 2s, softer continuation threshold so low-energy words aren't mistaken
for silence), an interrupt monitor while the assistant is speaking/working
(user starts talking → stop speech, interrupt the old turn, return to
listening, discard stale assistant output via a speech cursor), epoch/serial
guards so stale timers can't restart old output, conversation-scoped loop,
mute + stop controls anchored at the composer, and typing stays available
throughout. Port the SEMANTICS to the TUI; don't redesign from scratch.

eigen's plumbing exists but is weaker on every axis: `internal/voice`
whisperSTT records a FIXED 30s arecord window (no endpointing, no interrupt),
ctrl+t is the only trigger, and on the real machine detection fails
(whisper.cpp checkout has legacy `main` not `whisper-cli`; models dir has
only `for-tests-*` fixtures) so `/voice` reports unavailable.

- [x] **BUTTON, not chord.** SHIPPED (composer bar, e28318d + 5d056ef): mic
  controls anchored at the input — '⏺ speak · ▶ read · ◉ voice' right-aligned
  under the input box, live states (⏺ stop · listening… / ◌ transcribing… /
  ● listening / ◌ thinking / ▷ speaking), click-again stops, esc discards.
  Keybind + palette stay as secondary paths. Original text follows:
  ctrl+t is zellij's tab-mode chord — dead in the
  user's stack (zellij-in-ghostty), and alt+t is luck. The PRIMARY affordance
  must be clickable: a mic button in the sidebar (and/or beside the input
  line) — idle ⏺ / listening ● pulsing / transcribing ◌ / muted ⊘ — same
  states the codex version surfaces via its composer aura + mute/stop
  buttons. Click toggles conversation mode; while active, a stop and a mute
  control render next to it. Keybind stays as a secondary path through the
  action registry (and the palette: "conversation mode", "dictate once",
  "speak last answer") for terminals where it survives.
- [x] **VAD endpointing, not a fixed window.** SHIPPED (aa82b2e, hardened
  5d056ef: heartbeat deadlines fire with zero mic data, cancel-then-Wait
  teardown). recordVAD streams arecord→RMS endpointing. Original text:
  Replace `arecord -d 30` with
  streaming capture (arecord to stdout) + RMS computation in Go — the same
  endpoint logic as patch.js `endpoint()`: speech starts after ~220ms above
  threshold, submit after ~1.8s of trailing quiet, a softer
  possible-speech threshold extends the tail. Tunables via config like the
  codex version's localStorage knobs (silence-ms, vad-threshold).
- [x] **Interrupt-on-speech.** While the reply is being spoken, a mic
  monitor with a HIGHER threshold + grace period (ported patch.js
  `makeMonitor`: 420ms sustained voice above 0.035 RMS, 180ms grace) cuts
  the TTS and returns to listening — `monitorInterrupt` in
  internal/voice/vad.go, batched [speak, monitor] on one ctx in
  voiceTurnDone, epoch-guarded. Frame-based timing so tests pipe audio.
  Mid-TURN interrupt (speech while the model is still working) not built —
  the mic would hear keyboard/fan noise during long turns; revisit if the
  speak-leg interrupt proves trustworthy in daily use.
- [~] **TTS quality: Kokoro, reuse don't rewrite.** Kokoro detection SHIPPED
  (aa82b2e): speech.Detect prefers kokoro_stdin.py via the readd venv (NOT
  'readd speak' — that reads transcripts). Sentence-chunked streaming SHIPPED:
  speechQueue speaks complete sentences as deltas stream in (speech starts
  at the first sentence boundary mid-turn); voiceTurnDone drains the queue
  then relistens; read-aloud toggle streams too without re-speaking.
  Mute SHIPPED: ⊘ on the composer bar in voice mode — stay in the
  conversation, replies still speak, mic parked (no recording, no
  interrupt monitor); unmute resumes listening; exit clears mute. Original text: The user's stack already
  has `kokoro_stdin.py` (Kokoro ONNX → aplay, reads stdin — exactly eigen's
  cmdTTS contract) and the readd daemon (espeak-ng/piper). Default tts_cmd
  detection should prefer kokoro_stdin.py / readd over bare espeak-ng;
  sentence-chunked streaming speech (speak as paragraphs complete, the
  read-aloud queue semantics) instead of waiting for the full answer.
- [~] **STT setup + detection fixes.** Detection SHIPPED (aa82b2e):
  `lookWhisper` accepts legacy `main`, `lookWhisperModel` skips fixtures —
  real machine resolves whisper-cli + ggml-base.en.bin. REMAINING: `/voice
  setup` doctor SHIPPED (internal/voice/doctor.go): `/voice setup`
  diagnoses every component (recorder/whisper/model/tts/kokoro
  pieces/playback) with ✓/✗ + a concrete fix per missing item;
  env-var config keys already exist. Original text: `lookWhisper` accepts the legacy
  `main` binary; `lookWhisperModel` skips `for-tests-*` fixtures; a
  `/voice setup` doctor reports what's missing and offers the fix (download
  ggml-base.en.bin, build/symlink whisper-cli). Config keys beside tts_cmd:
  stt_cmd/whisper_bin/whisper_model (env vars exist; config.json is the
  discoverable surface).
- [ ] **Better than the original.** What the TUI can do that the webview
  couldn't: works over ssh/zellij everywhere eigen runs; whisper.cpp local
  STT (no composer dictation dependency); per-session voice state visible in
  the sidebar; transcripts land in the normal session history/persistence.
  Keep the codex version's discipline: typed turns keep working, explicit
  exit discards pending dictation, switching sessions stops the loop.
- [x] **Verify live.** DONE — user confirmed the full conversation loop
  works on the real machine (listen → submit → reply spoken → interrupt by
  talking over it → relisten). Original text: The workspace harness has no
  mic; verify
  record/VAD/interrupt on the real machine. TTS + state machine are
  verifiable headless (fake STT/TTS backends; pipe TTS to a file sink).

## Tier 16 — multi-agent orchestration (plans → parallel sub-agents)
Goal: turn the depth-bounded `task` tool (one delegated subtask at a time)
into real multi-agent work — a plan decomposed into named roles that run in
parallel, escalate when stuck, and merge their reports back. Builds DIRECTLY
on what exists: `agent.Subtask`/`SubtaskWith`/`SubtaskBackground` (depth-bounded,
routed via the orchestrator's kind/difficulty), the `BgRegistry` durable task
store (~/.eigen/tasks/<id>.jsonl + transcript, cancel-marker protocol, lost
detection — Tier 12), the `task_status` collect surface, and the auto-router
(internal/llm/router.go) that already picks a model per subtask. This is Tier 7
dream #12 (sub-agents: "named roles, parallelism, escalation") made concrete,
and the substrate for #13 (ultraplan).

- [x] **Named roles, not anonymous subtasks.** SHIPPED v1 (7e1ca41): hardcoded
  READ-ONLY roles researcher/reviewer/summarizer (system framing + tool
  allowlist + default difficulty); SubtaskOpts.Role; Registry.Subset builds an
  immutable per-child toolset. Mutating roles (implementer/tester) DEFERRED to
  v2 (need isolated workspaces + serialized approvals). Config-file roles cut
  (hardcoded for v1 per review).
- [x] **Parallel fan-out with a bounded pool.** SHIPPED v1 (7e1ca41): task_group
  tool + Agent.TaskGroup — bounded worker pool (default 3/max 6, max 8
  children), parent-ctx cancel + per-child/group timeouts, result caps, panic
  recovery, stable input-order report. READ-ONLY children only (enforced
  mechanically), which is what makes it safe (no approval race, no concurrent
  writes). -race verified + live-verified with opus.
- [x] **Escalation.** SHIPPED (d0bd692): a read-only fan-out child that fails
  with a HARD error (not cancellation, not an explicit model override) gets ONE
  retry at the next difficulty tier up (router → stronger model), bounded so it
  can't loop. No text heuristics (fragile/injectable) per review — only a real
  error triggers it; report marks "escalated → <model>".
- [x] **Merge step.** SHIPPED (d0bd692): task_group's optional `synthesize`
  question runs a final tool-less sub-agent over the combined report → one
  coherent "--- synthesis ---" answer (reconcile/cite), routed cross-vendor.
- [x] **Observability.** The per-child report (role · model · duration ·
  status + synthesis) is the surface for FOREGROUND fan-out, which joins
  synchronously (nothing to watch live). A live plan-tree in [tasks] only
  applies to BACKGROUND fan-out, which doesn't exist yet — deferred until it
  does (review: "keep task_group foreground; tree-view can wait").
- [x] **Mutating parallel fan-out (v2).** SHIPPED (c0565f5): task_group_mutating —
  implementer children edit in ISOLATED git worktrees, parent captures each
  diff, validates the combined set in a throwaway worktree, applies the clean
  result to the real tree behind ONE apply-time approval; conflicts skipped +
  reported. git-only / repo-root / clean-tree enforced; children get
  read/write/edit/move only (NO bash/git/network); .git denied; worktree ops
  serialized; -race + live verified. Conflicting children now REBASE by redo (6ece734) instead of being
  dropped. DEFERRED: build/test after apply, subdir-session prefix, submodules.
- [x] **Safety.** SHIPPED + test-enforced. Read-only children never call
  Approve (read-only tools auto-run → the N-concurrent-approval race can't
  occur; TestReadOnlyFanoutNeverApproves under a gated parent). Mutating
  children run PermAuto in SANDBOXED worktrees with NO Approve callback —
  nothing real until the parent's single apply-time approval, which respects
  auto mode (c33a797: no prompt in auto, one prompt in gated).

## Tier 17 — workflows (declarative, repeatable multi-step runs)
Goal: name and replay a multi-step process — "review this PR", "cut a release",
"triage the inbox" — as a declarative workflow instead of re-typing the steps.
Distinct from Tier 16 (dynamic, model-decomposed orchestration); a workflow is
AUTHORED structure the user trusts and reruns. Builds on existing primitives:
`/loop` (idle re-submit), `--prompt-file`/automation (Tier 7 #5), hooks (#11,
lifecycle-triggered commands), skills (reusable instructions), and the goal
feature (#3, persistent north star + judge).

- [x] **Workflow definition.** SHIPPED v1 (b354fa9): ~/.eigen/workflows/<name>.md —
  frontmatter (name/description) + "## <id>" step sections; per-step directives
  model:/check:/on_failure:/retries:; {{var.X}} placeholders. internal/workflow
  parser, hand-rolled (skills grain, no YAML dep). A `~/.eigen/workflows/<name>.{json,md}` with
  ordered steps; each step = a prompt (or a skill invocation), an optional
  model/role, an optional success check (a goal_achieved-style judged
  condition), and what to do on failure (stop / retry / continue). Steps can
  reference prior steps' outputs.
- [x] **Runner.** SHIPPED v1 (b354fa9 headless + 687085e in-TUI): `eigen run
  <wf> [--var k=v]` exit-coded (0/2/1), steps on ONE carried session, stderr
  progress + stdout final; in-TUI /workflow <name> plays steps via the queue. `eigen run <workflow>` (headless, automation-friendly,
  exit-coded like #5) and an in-TUI `/workflow <name>` that executes steps in
  sequence, shows progress in the plan panel, and pauses for approval at gated
  steps. Reuse the agent loop per step; carry context forward (or compact
  between steps for long workflows).
- [x] **Branching + conditions (v1).** SHIPPED (b354fa9): per-step opt-in
  check: judged cross-vendor → on_failure stop|continue|retry(retries:N).
  Linear + on-failure, not a DAG (as scoped). Minimal control flow — a step's judged
  outcome picks the next step (success → ship, failure → fix-then-retry). Keep
  it small and legible (not a general DAG engine first); a linear sequence with
  on-failure branches covers most real processes.
- [~] **Triggers — DEFERRED to v2.** Bind a workflow to a hook event / feed
  item. Not built; the runner + def exist, so this is wiring on top. A workflow can be bound to a hook event (#11) or the feed
  (#6) — e.g. "on a new review-requested PR, run the review workflow" — so
  repeatable processes fire proactively, not just on demand.
- [~] **Authoring from history — DEFERRED to v2.** "Save the last N turns as
  a workflow." Not built v1. "Save the last N turns as a workflow" — turn
  an ad-hoc successful session into a replayable workflow, the way skills
  capture reusable instructions.

## Tier 18 — other model types as first-class servers (beyond chat LLMs)
Goal: serve and use NON-chat models where they fit better/cheaper than an LLM —
embedders, rerankers, image generation — as first-class capabilities the agent
and app draw on. Tier 7 dream #23 ("integrate other model types efficiently")
made concrete. The user already runs local model servers (llama.cpp, the BGE
embedder service, whisper, Kokoro). Build order (user-set): 1+2 FIRST (embedder
seam → retrieval, the token-efficiency win), then image gen (we have no image
model today), then local-first routing (OPT-IN). Non-LLM deterministic helpers
are just tools — OUT OF SCOPE here.

- [x] **Provider seam for non-generative models.** SHIPPED (ce1ce38): llm.Embedder (text→vector), OpenAI /v1/embeddings backend = local BGE; CosineSim. Reranker deferred.
- [~] **(orig) Provider seam for non-generative models.** Today `llm.Provider` is
  chat-completions shaped. Add sibling interfaces — `Embedder` (text → vector),
  `Reranker` (query+docs → scores), maybe `Classifier` — with the same
  catalog/credential/discovery treatment (a model entry declares its KIND).
  The local llama.cpp server + the existing BGE embedder are the first backends
  (OpenAI-compatible /embeddings).
- [x] **Retrieval that uses them.** SHIPPED (7e96ddd): the `retrieve` tool + per-project incremental vector index (brute-force cosine, line-window chunks, staleness-checked, optional). Session/memory indexing + auto-assembly + reranker deferred to v2.
- [~] **(orig) Retrieval that uses them (closes Tier 7 #1's "retrieval instead of
  re-paste").** An embedder enables semantic retrieval over the project + past
  sessions + memory, so context is RETRIEVED on demand instead of pasted whole
  — the biggest remaining token-efficiency lever. A `retrieve` tool and/or
  automatic context assembly; a reranker tightens the top-k.
- [x] **Image generation.** SHIPPED (65ca950): generate_image tool → Bedrock
  (Stability stable-image-core default, us-west-2; Nova/Titan dialect too).
  Saves PNG(s) under <project>/eigen-images/ + returns them inline. Live-
  verified (1536×1536 PNG via opus).
- [~] **(orig) Image generation (NEEDED — we have no image model today).** A local or
  hosted image model behind a `generate_image` tool for diagrams/mockups/
  assets — output rides the image-capable tool-result plumbing already built.
  Not optional: eigen currently cannot produce images at all.
- [x] **Local-first routing for the cheap stuff — OPT-IN.** SHIPPED (a399a64):
  config local_background (default off) + a readiness probe (localReady: /health
  ok vs 503-loading vs /v1/models fallback) so background work uses a LOCAL
  model only when opted-in AND actually serving — never stalls on a busy/down
  local server.
- [~] **(orig) Local-first routing for the cheap stuff — OPT-IN.** Titling, dreaming,
  skill scans, embeddings — route to a LOCAL model (llama.cpp) when present,
  saving frontier budget. OPT-IN via config (default off): local quality
  varies, so the user enables it deliberately. Extends the small-model
  selection that already prefers `EIGEN_LLAMA_BASE_URL`.
- [~] **Non-LLM solutions — OUT OF SCOPE (user call).** Deterministic helpers
  (classifier/regex/AST) are just tools the orchestrator already picks; no
  model-serving layer needed. Dropped from this tier.

## Tier 19 — remote: SSH machines + control eigen from another machine
Goal: drive eigen on a remote host, and reach your eigen from another machine.
Two directions, both building on the EXISTING daemon (a line-JSON protocol over
~/.eigen/daemon.sock — `internal/daemon`: Dial/List/New/Attach/Input/…). The
protocol is already transport-agnostic; only the listener is unix-local and the
auth model is "whoever can open the socket." That's the seam to extend.

A. EIGEN REACHES OUT (run agents on remote machines)
- [x] **SSH-backed sessions.** SHIPPED (4666bb2): `eigen --remote user@host[:dir]` → `ssh -T host eigen daemon stdio` → local TUI view drives a REMOTE daemon; whole agent loop runs remote. Transport seam (52d6c68): Client over io.ReadWriteCloser + `eigen daemon stdio` bridge. A session whose tools execute on a remote host
  over SSH: bash runs there, file tools read/write there. Cleanest path is an
  `eigen daemon` running ON the remote (reached over an SSH-forwarded socket),
  so the full agent loop is local to the code — not piping every tool call
  over the wire. `eigen --remote user@host[:dir]` opens/attaches a session on
  the remote daemon; the local TUI is just a view (the daemon/view split
  already supports this).
- [x] **Bootstrap.** SHIPPED (2ff1be0): `eigen remote install <user@host[:dir]|name>` — ssh `uname -sm`, refuse unknown arch/OS, copy the running binary when targets match else cross-compile (CGO_ENABLED=0 static), scp + atomic mv + verify. No GitHub release needed (20MB static exe). ~~systemd unit on remote~~ deferred (the auto-spawning `eigen daemon stdio` makes a persistent remote unit optional). `eigen remote install user@host` copies/builds the binary
  and sets up the remote daemon (systemd user unit + credential snapshot,
  reusing `eigen daemon install`). Detect arch/OS; refuse unknown.
- [x] **Per-host config.** SHIPPED (3d056bb): ~/.eigen/hosts.json (0600), `eigen remote add/list/remove`; a saved name carries ssh/dir/model/perm defaults that seed remote sessions. Remote model/creds/roots (the remote may have
  different AWS profiles, a different repo layout). A `~/.eigen/hosts.json`.

B. CONTROL YOUR EIGEN FROM ANOTHER MACHINE (phone/laptop → desktop daemon)
- [~] **Network listener for the daemon.** DECIDED AGAINST (cross-vendor security review by glm-5.2, 2026-06-13): net-negative for a personal tool. It adds an RCE-grade network endpoint (a connection = full agent control = arbitrary code as the user) guarded by a WEAKER boundary (static shared secret in a 0600 dotfile) than `ssh -L`+Tailscale already give (kernel fs perms + ssh/WireGuard crypto + device identity), for the marginal benefit of skipping one ssh -L command — and no native mobile client (the only thing it'd unlock) exists. Recommendation: ssh -L + Tailscale serve IS the complete remote story. Original idea: optionally bind the daemon to a TCP
  socket (loopback by default; LAN/Tailscale when explicitly enabled) in
  addition to the unix socket — same protocol. Off by default.
- [~] **Auth + transport security.** N/A — network listener decided against (see above). The review also flagged that the drafted bearer-token-from-env was itself a bug: eigen spawns subprocesses (the bash tool) that inherit os.Environ(), leaking an RCE-capable token to every child. ssh/Tailscale crypto is the auth. The unix socket trusts filesystem perms;
  a network socket MUST NOT. Require a token (shared secret in
  ~/.eigen/daemon.env, already 0600) and TLS, or — simpler and safer — document
  "expose only over SSH/Tailscale, never raw." Fail closed: no token ⇒ no
  network listener.
- [x] **Thin remote client.** SHIPPED (7b34f79) as `eigen attach --sock <path>` over an ssh -L forwarded socket — reuses ssh's crypto, no token/TLS/listener. (`--host <ws-addr>` waits on Wave 4b.) `eigen attach --host <addr>` (or the app's
  session list) over the network transport, so a laptop attaches to the
  desktop's running sessions — the daemon already replays transcript + streams
  live, and views are stateless, so this is mostly transport + auth.
- [~] **Mobile-friendly read/notify (stretch).** DEFERRED — needs a network listener (decided against) or a future native client; reach via Tailscale + `eigen attach --sock` over a forwarded socket meanwhile. A minimal read-only status +
  approval-answer surface (the notifications/approvals tray is the model) so a
  phone can see "needs you" and answer an approval remotely.

Constraints / non-negotiables:
- Network exposure is OFF by default and fails closed without auth.
- Remote tool execution inherits the same permission posture + gating; a
  remote mutating tool is still gated in gated mode.
- Reuse the daemon protocol + daemon/view split; do NOT fork a second control
  path. The transport is the only new thing; the session model is unchanged.
- Secrets never cross the wire in the clear; credential snapshots stay 0600.

## Tier 20 — eigen in your pocket (outbound notify + approve, no port, no Tailscale)
Goal: reach your agent fleet from your phone WITHOUT carrying Tailscale or an
ssh client around — the real away-from-desk case is "unblock a stuck agent",
not "code from the phone". The novel move (vs the rejected Tier 19 Wave 4b
network listener): the daemon DIALS OUT to a channel you already carry, so
there is NO inbound port, NO Tailscale, it's NAT-proof, and the transport's
auth is something battle-tested instead of a static dotfile token.

Why this is safe where the network listener wasn't (glm-5.2 review reasoning):
outbound-only (nothing listens/exposed → strictly safer than a WS listener);
auth = bot token (0600) + an ALLOWLIST of your own chat/account id (only you
can talk to it); bounded blast radius — v1 exposes READ + APPROVE, not "run
arbitrary command", so a worst-case compromise answers approvals on tasks you
already started, not fresh RCE. Caveat: messages transit the provider's servers
(not E2E), so v1 sends SUMMARIES + approvals, not full transcripts, by default.

Slots into seams that already exist: the notifications/approvals tray (Tier 9
Wave 5, alt+n) + `NotifyCmd` become the push side; a new outbound long-poll
becomes the answer side; the daemon already brokers approvals across views, so
the phone is just another "view" that can answer.

- [ ] **Pick the channel (decide at build time).** Options captured:
  (1) **Telegram bot** — already on the phone, free, TLS, rich inline buttons
  ([approve]/[deny]), long-poll outbound (getUpdates) so no port; bot token +
  chat-id allowlist. RECOMMENDED default. (2) **ntfy.sh** — lighter, notify +
  action buttons, needs the ntfy app. (3) **self-hosted relay on an existing
  EC2 box (dolly/dev…)** — daemon dials out to a tiny relay you own, a minimal
  phone web page hits it over HTTPS; keeps data off any third party, more to
  build. Telegram for v1; relay as the privacy-max variant later.
- [ ] **Push: "needs you".** An agent hits an approval (or a long turn / error /
  goal-nag) → outbound message to your phone with the session, the tool + args
  summary, and inline [approve]/[deny] (and a "mute this session" action).
  Reuses the tray's "what needs me" model; respects the existing ping/notify
  throttling (no spam per token).
- [ ] **Answer: approve/deny remotely.** Tapping a button routes back through
  the SAME approval path any view uses (the daemon broadcasts → the phone view
  answers); fail-closed on timeout exactly like a local gated approval.
- [ ] **Read: status + recent.** Ask "what's running?" / "what did <session>
  do?" → concise status from the live session list + last-note summaries
  (NOT full transcripts by default — they transit the provider).
- [~] **Converse (opt-in, more power + risk).** Send a follow-up message to a
  running session from the phone. Separate explicit opt-in beyond notify+approve
  — widens blast radius, so gated behind its own setting + the allowlist.
- [ ] **Security/Constraints (carried from the review).** Outbound-only (never
  bind a port); bot token 0600 + chat-id/account allowlist (reject anyone
  else); v1 surface = read + approve only (no arbitrary command); summaries not
  full transcripts by default; a kill switch (`eigen daemon dm off`) that drops
  the outbound poller; audit-log every remote approval (who/when/tool/args
  digest); get a cross-vendor security pass on the channel-auth model before
  shipping (use a background task on a different vendor when the review tool is
  down). OFF by default; enabled deliberately.

## Tier 21 — TUI ergonomics + home polish (user asks)
Captured from live use; all in `internal/tui` + `internal/app`. Keep the
geometry-owned-first + one-action-layer + keyboard/click-parity conventions.

- [ ] **Right-panel notepad tab.** A scratch pad in the right panel (a fifth
  tab beside changes/git/term/tasks) the USER types into — not the agent:
  jot notes, paste a snippet, draft a prompt, keep a TODO for yourself while a
  turn runs. Persist per-session (or per-project) under `~/.eigen/` so it
  survives detach/rebuild. Plain multi-line text edit; focus model like the
  terminal tab (grab keys when focused, ctrl+g releases). Stretch: the agent
  can READ it (a `notepad` tool / inject as context) so "see my notes" works,
  but writing stays the user's.
- [ ] **Default steer-vs-queue choice (config).** Today a message typed while a
  turn runs always QUEUES (sent after the turn). Add a config knob (e.g.
  `steer_default: queue|steer`) + a live toggle so the user picks the default:
  QUEUE (current — wait for the turn) vs STEER (interrupt/inject now). Make the
  behavior obvious in the running-line hint ("enter queues" vs "enter steers")
  and keep the other reachable per-message (e.g. alt+enter does the opposite).
  Respect the daemon turn semantics (a steer must interrupt cleanly, not race).
- [ ] **Home page: less empty space, more inviting.** The home dashboard reads
  as mostly blank — the big EIGEN wordmark + a short feed leave acres of empty
  rows. Make it feel alive + useful: tighten vertical rhythm, fill the width
  (the wide breakpoint especially), surface more at a glance (recent sessions
  with status, top feed actions, quick-start buttons, maybe per-project
  liveness or a stat strip). Designerly restraint still applies — informative,
  not decorative — but the first screen should invite action, not echo.

## Tier 22 — design system (one visual language, roles not hues) ✅ COMPLETE
User-initiated: "create a design system." The brief lives in
[`docs/design-system.md`](./docs/design-system.md) (durable; survives
compaction). Goal: the chat TUI + app shell read as ONE product, every styled
thing means something, and a re-theme is one edit.

THE BRAND RULE (user-set): blue (`theme.Accent` + `theme.Title`) is reserved
for eigen's brand + structural chrome; anything else (selection, active
session, state highlights) uses a DIFFERENT theme role.

- [x] **`Focus` role + first application.** SHIPPED: new non-blue `theme.Focus`
  (rose) for "the session THIS pane drives" — rail pointer/name + sidebar
  title now use Focus, not brand blue, so the active window pops against the
  blue chrome (rail.go railEntryLabel, sidebar.go sbTitle).
- [x] **Lock the role vocabulary.** SHIPPED: added `Sel` (selection/cursor),
  `OnBright` (text on bright fill), and theme-owned animation ramps
  `BreathRamp`/`WorkingRamp` (+ `*Dim`/`*Bright` stops).
- [x] **Apply the brand rule everywhere.** SHIPPED: all selection/active/current
  highlights moved off Accent/Title onto Focus/Sel across tui + app; the
  working `●` glyph + rail spinner moved to Working (orange, matches loader);
  the 3 raw literals folded into roles. Screenshot-verified both surfaces.
- [x] **App-shell parity.** SHIPPED: `internal/app/style.go` aliases all map to
  `theme.*` (no literals); the drift-guard test enforces it tree-wide.
- [x] **Drift guard test.** SHIPPED: `internal/theme/drift_test.go` fails the
  build if any raw `lipgloss.Color(`/`AdaptiveColor{` appears outside
  internal/theme.
- [x] **Living swatch** — SHIPPED: `eigen theme` renders every role (color
  chip), the ramps, the weight scale, the glyph vocabulary + the brand rule
  (internal/theme/swatch.go).
- [x] **Re-theme proof** — SHIPPED: named palettes (nord default + gruvbox) as
  theme.Palette data; config `theme` key / EIGEN_THEME selects one at init and
  re-themes everything (roles + styles + ramps) with zero call-site changes;
  main.go re-execs once to apply. Brand rule holds per-palette (test).

## Tier 23 — performance + resource health (RSS, leaks, profiling)
User-initiated: "memory leak check, RSS, resource usage, performance." eigen's
real process is the **daemon** — long-lived, hosting many sessions in ONE
process (MCP/LSP children, PTY shells, background tasks, event buffers). So a
slow leak or unbounded growth is the failure that actually bites (orphaned
daemons were once the machine-freeze bug). Goal: measure, bound, and keep it
honest over time. Builds on the earlier stability sweep (RWMutex on sessions,
bounded replay buffer, bash process-group kill, MCP/LSP teardown, bg-task
caps) — this tier is about MEASUREMENT + LONG-RUN bounds, not re-fixing those.

- [ ] **Baseline + visibility.** Capture daemon RSS/goroutines/heap at rest,
  per idle session, and per active turn. Wire `net/http/pprof` behind an opt-in
  env (`EIGEN_PPROF=127.0.0.1:port`, loopback only, off by default) so heap /
  goroutine / allocs profiles are grabbable live. Maybe surface a cheap
  `eigen daemon status` line: RSS, goroutines, session count, uptime.
- [ ] **Leak hunt (the real one).** Run a long soak: N sessions, many
  turns/attaches/detaches/rebuilds over hours, watch RSS + goroutine count for
  monotonic growth. Prime suspects to audit: per-session event/replay slices,
  the daemon subs fan-out, BgRegistry records, PTY emulator buffers, MCP/LSP
  client lifecycles, transcript history growth, goroutines that outlive a
  detached view. `go test -race` already clean; add a goroutine-count
  regression assertion (attach→detach→GC leaves no residual goroutines).
- [ ] **Bound what grows.** Confirm/cap the unbounded-over-time structures:
  in-memory transcript vs on-disk (does a 1000-turn session hold everything in
  RAM?), event buffers (replay cap exists — verify others), tool-result image
  retention (ShedToolImages), feed/suggest caches. Each long-lived slice/map
  needs a documented ceiling or eviction.
- [ ] **Turn latency + allocs.** Profile a turn's hot path (wire encode/decode,
  transcript render, context-budget compaction, token counting). Cut obvious
  per-turn allocations; note any O(history) work that should be incremental.
  TUI render cost at scale (huge transcript, 4 panes) — the band/sidebar
  recompute should already be memoized; verify under a big transcript.
- [ ] **CI/regression guard.** A `make perf` (or a tagged test) that runs the
  soak in miniature + asserts no goroutine/heap growth across a fixed workload,
  so a future change that reintroduces a leak fails loudly. Keep it in the
  standard gate or a nightly.
- [ ] **Document findings.** A short `docs/performance.md`: measured baselines
  (RSS at rest / per session / per turn), the bounds each structure carries,
  how to profile (`EIGEN_PPROF`), and the soak recipe — so this stays
  re-runnable, not a one-off.

## Tier 24 — clean up + arrange the roadmap (do NEXT, after current work)
User-initiated: the roadmap has grown to ~1265 lines / 23 tiers, most of them
SHIPPED with long inline "SHIPPED: …" writeups. It's now a history log, not a
forward plan — hard to see what's actually left. This is the immediate next
task once the current work wraps. Scope (a doc-only pass, no code):

- [ ] **Split done from todo.** Move completed tiers/items into a terse
  `## Shipped` ledger (one line each: title + commit/area, NO multi-paragraph
  writeups — those live in git history + project memory). The top of the file
  should show, at a glance, ONLY what's open.
- [ ] **Surface the open work.** A short "Now / Next / Later" (or
  active-vs-backlog) section up top: what's in flight, the immediate queue
  (e.g. Tier 21 ergonomics, Tier 23 perf), and the parked big bets (Tier 7
  leftovers). Renumber or tag so priority is obvious without reading 1200 lines.
- [ ] **Prune redundancy.** Fold per-tier "SHIPPED" detail into one-liners;
  drop notes duplicated in project memory; keep the genuinely-useful grounding
  (conventions, the verify gate, architecture pointers) but trim it.
- [ ] **Reconcile with reality.** Walk each `[~]`/`[ ]` and confirm it's still
  true (some "todo"s may be done; some "done"s regressed). Mark deferred items
  `[~] DEFERRED — why`, not a bare `[ ]`, per the finish-each-tier rule.
- [ ] **Keep it durable.** Result: a roadmap that a fresh (post-compaction)
  instance can read in 2 minutes to know what to do next. Update
  `docs/` cross-links (design-system, performance) so the roadmap points at
  the detailed docs instead of inlining them.

## Debt / bugs
- [x] **Untitled daemon sessions still appear.** FIXED: (1) `Host.Restore` now
  calls `maybeTitle` per restored session, so sessions whose title never landed
  (titler failed, daemon died mid-flight, pre-titler sessions) get backfilled on
  the next daemon start; (2) `Session.info()` falls back to a snippet of the
  first user message while no model title exists, so listings never show
  "(untitled)" for sessions with content; (3) titler errors now log to stderr
  (`eigen daemon: title sN: …`) instead of failing silently, and the next
  Persist retries; (4) an in-flight guard stops duplicate title calls (Persist
  fires after every message). Live-verified: fabricated untitled persisted
  session + daemon restart → meta backfilled with a real small-model title.

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
