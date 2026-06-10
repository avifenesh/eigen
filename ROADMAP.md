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
- [ ] real token usage from provider responses (vs. estimate) — deferred: needs
      live API verification of each provider's usage fields; the `~estimate` is honest

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
5. **Automation** — scheduled / triggered runs (cron-like, on-event).
6. **Background scan for wide-reaching actions** — proactively flag risky/broad
   operations (mass deletes, wide refactors) before they run.
7. **Computer use built in** — native screen/GUI control.
8. **Agent workspace built in** — native isolated desktop/terminal workspace
   *(today via the agent-workspace-linux MCP; make it first-class)*.
9. **Conversation mode** — voice conversation over the chat: STT for spoken input
   + TTS for spoken replies (builds on the existing read-aloud/speech plumbing).
   Not small — a full audio in/out loop.
10. **Auto-router** — pick the model/provider per task automatically (cost/latency/capability).
11. **Hooks** — pre/post tool, pre/post turn, pre/post compaction user hooks.
12. **Sub-agents** — *(partially shipped: depth-bounded `task` tool; expand: named roles, parallelism)*.
13. **Ultraplan** — dozens of in-depth sub-agents driven by one big plan ahead.
14. **Ping** — *(shipped cf8d2de: terminal bell + optional notify_cmd on
    approval-needed and long-turn-finished)*
15. **AGENTS.md integration** — *(shipped: the repo's AGENTS.md (also
    .eigen/AGENTS.md, CLAUDE.md), nearest-first walking up to the .git root, is
    injected into the system prompt as repository guidance — distinct from
    learned memory; capped per file.)*
16. **tok/s in & tok/s out measurement** — *(shipped 84f13b1: output tok/s,
    live + last-turn in status bar; input-side + real usage fields still open)*
17. **Observability for long-term learning** — structured logs of errors, tool
    uses, outcomes; feed back into memory/dreaming.
18. **`/` config for most things** — *(shipped: /config shows the settings
    table; /config <key> <value> validates + persists to ~/.eigen/config.json.
    Live-session knobs stay /model /perm /effort /search /goal /loop.)*
19. **Auto-discovery of newly available models** — *(shipped: `eigen models`
    lists the catalog, then probes every credentialed provider's listing
    endpoint (Anthropic /v1/models, Bedrock inference-profiles, grok/glm/llama
    /models) and reports models not yet in the catalog. Read-only; new ids are
    usable immediately via --model/-/model.)*
20. **Image integration using other models** — vision/image understanding via
    auxiliary models when the main model lacks it.
21. **Drag-and-drop of files** — *(shipped: a dropped file arrives as a
    bracketed paste of its path; eigen normalizes it (strips file://, unquotes,
    percent-decodes, handles multi-file drops) into clean path tokens the model
    reads like an @file mention. Plain pasted prose is untouched.)*
22. **Image copy-paste** into the conversation — blocked on #20 (vision support
    in the provider wire formats); terminals only paste an image PATH, which
    already works via #21, but the model can't yet receive image data.
23. **Integrate other model types efficiently** — embedders, diffusion, mamba, etc.,
    to offer non-LLM solutions where they fit.

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

TUI commands: /help /resume /save /export /clear /model /perm /skills /tools
/find /copy /read /rebuild /quit · keys: `/` commands · `@` files · ↑↓ select ·
tab/click expand · while running: enter queues · esc interrupts.

TUI features: steer+queue, mouse click-to-expand + wheel, slash & @file
autocomplete, rich tool blocks + live status, LCS diffs, live plan panel (todo
tool), status bar (model·perm·~ctx), read-aloud, clipboard, gated "always allow".
