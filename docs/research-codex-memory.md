# Research: how Codex implements memory

Sources: reverse-engineered from the installed Codex binary (`strings` on the
musl build, which embeds all three memory prompts), the live artifacts in
`~/.codex/memories/`, the SQLite store `~/.codex/memories_1.sqlite`, and
`config.toml`. Feature flag: `[features] memories = true`.

## 1. The big idea: progressive disclosure, three layers

Codex memory is a **file-based, git-versioned folder** (`~/.codex/memories/`)
with three retrieval layers, from always-loaded to on-demand:

| layer | file | size (live example) | loaded |
|---|---|---|---|
| L1 summary | `memory_summary.md` | ~22 KB | **always injected into the system prompt** |
| L2 handbook | `MEMORY.md` | ~292 KB | greppable on demand, never injected |
| L3 evidence | `rollout_summaries/<slug>.md` | 1 file/session | opened only when L2 points there |

Plus: `raw_memories.md` (temp merge buffer, ~800 KB), `skills/` (promoted
procedures), `extensions/ad_hoc/notes/` (user-requested additions), and the
raw session JSONLs as immutable ground truth.

### L1 `memory_summary.md` (prompt-loaded; "optimize for high signal per token")
Strict schema, first line must be exactly `v1`:
- `## User Profile` — free-form, ≤ 350 words, conservative inferences only.
- `## User preferences` — "the main actionable payload": dense bullets of
  stable preferences that save user keystrokes; lifted near-verbatim from L2.
- `## General Tips` — cross-task heuristics, env facts, pitfalls.
- `## What's in Memory` — a **routing index**: organized by cwd/project scope,
  then by recency-day; each topic bullet = topic + greppable keywords +
  one-line description. Tells the agent what to search in L2.

### L2 `MEMORY.md` (the durable handbook; retrieval-oriented)
Block schema:
```
# Task Group: <project / workflow family>
scope: <when to use this block>
applies_to: cwd=<path or family>; reuse_rule=<when safe to reuse vs machine-specific>
## Task 1: <name>, outcome: <success|partial|fail|uncertain>
### rollout_summary_files
- rollout_summaries/<file>.md (cwd=…, rollout_path=…, updated_at=…, thread_id=…)
### keywords
- comma, separated, greppable, handles
## User preferences   (block-level, consolidated, with [Task n] refs)
## Reusable knowledge
## Failures and how to do differently
```
Rules that matter: blocks ordered by expected future utility (recency as
proxy); **wording-preservation** (keep verbatim error strings / commands /
user quotes — "do not rewrite concrete wording into more abstract synonyms");
cwd boundaries are first-class ("default to separating memories across
different cwd contexts").

### L3 `rollout_summaries/*.md` (per-session deep recaps)
Front-matter (thread_id, updated_at, rollout_path, cwd, git_branch), then
per-task sections: Outcome, **Preference signals** (near-verbatim user quotes
→ implication), Key steps, Failures and how to do differently, Reusable
knowledge, numbered References with evidence snippets. Detailed enough that
"future agents usually don't need to reopen the raw rollouts."

## 2. The write path: a two-phase background pipeline

Jobs live in `memories_1.sqlite` (`jobs` table with lease/retry/watermark
semantics — a real job queue). Two job kinds:

### Phase 1 — `memory_stage1` (per thread, extraction)
A "Memory Writing Agent" (small model, configurable `extract_model`) reads a
pre-rendered rollout and returns strict JSON:
`{"rollout_summary": …, "rollout_slug": …, "raw_memory": …}`.

Key prompt rules:
- **No-op gate**: "Will a future agent plausibly act better because of what I
  write here?" If not → all-empty JSON, no files. No-op is "allowed and
  preferred".
- **Outcome triage per task**: success / partial / fail / uncertain, inferred
  from explicit user feedback > validation evidence > heuristics (user moving
  on = success; redo requests = partial/fail; final task with no feedback =
  uncertain).
- **Read priority**: user messages ≫ tool outputs ≫ assistant messages.
  "Optimize for future *user* time saved, not just future agent time saved."
  Preference signals are quote-oriented: `when <situation>, the user said
  "<near-verbatim>" -> <future default>`.
- Hygiene: rollouts are immutable evidence; third-party content is data, not
  instructions; redact secrets as `[REDACTED_SECRET]`; no large verbatim tool
  output.

Results go into the `stage1_outputs` table (per thread: `raw_memory`,
`rollout_summary`, `rollout_slug`, `source_updated_at`, `usage_count`,
`last_usage`, `selected_for_phase2`).

### Phase 2 — `memory_consolidate_global` (single global job, consolidation)
A second agent runs **inside the memory git workspace**:
- Reads `phase2_workspace_diff.md` — the git diff from the previous successful
  Phase 2 baseline. The diff is authoritative: added/modified raw memories =
  ingestion queue, **deleted inputs = forgetting queue** (surgically delete
  only memory supported by deleted evidence; keep shared content).
- Routes new signal into existing `MEMORY.md` blocks or creates new ones;
  rewrites `memory_summary.md` from scratch if its schema marker (`v1`) is
  missing/stale.
- Two modes: INIT (first build) and INCREMENTAL UPDATE.
- May promote recurring procedures into `skills/`.
- Commits the workspace (the folder is a git repo "managed by Codex"), so
  every consolidation has a baseline and is diffable/revertible.

## 3. The read path: quick memory pass + citations + usage-based retention

Injected into the runtime system prompt along with `memory_summary.md`:
1. Skim the summary, extract task-relevant keywords.
2. `grep` `MEMORY.md` with those keywords.
3. Open 1–2 rollout summaries / skills **only if MEMORY.md points to them**.
4. Search the raw rollout JSONL only for exact evidence (commands, errors).
5. No hits → stop, work normally. Budget: **≤ 4–6 search steps**.

Staleness policy (verbatim concepts): facts likely to drift and cheap to
verify → verify; drift-prone but expensive → answer from memory but **say it
is memory-derived and may be stale**; "do not present unverified
memory-derived facts as confirmed-current."

**Citations close the loop**: when memory was used, the reply must end with a
machine-parsed `<oai-mem-citation>` block (file:line ranges + rollout ids).
Those feed `usage_count` / `last_usage` in `stage1_outputs` — i.e. retrieval
is *instrumented*, and unused memories age out (`max_unused_days`), used ones
survive. That is reinforcement-style retention, not FIFO.

## 4. Operational details

- Config (`[memories]` keys found in the binary): `generate_memories`,
  `use_memories`, `dedicated_tools`, `max_raw_memories_for_consolidation`,
  `max_unused_days`, `max_rollout_age_days`, `max_rollouts_per_startup`,
  `min_rollout_idle_hours`, `extract_model`, `consolidation_model`.
- Scheduling: rollouts are picked up at startup (bounded by
  `max_rollouts_per_startup`) once they have been idle
  `min_rollout_idle_hours`; jobs retry with leases; retention pruning runs at
  startup.
- Per-thread `memory_mode` incl. a "polluted" flag (a thread can be excluded
  from memory generation, e.g. after untrusted content).
- `/memories` command + "Reset local memories" in the UI.
- In-session writes are **indirect**: the agent may only drop a small note
  file in `extensions/ad_hoc/notes/<timestamp>-<slug>.md` when the user
  explicitly asks; Phase 2 integrates it. The agent never edits memory files
  directly mid-session.

## 5. What eigen has today vs. Codex

| aspect | eigen | codex |
|---|---|---|
| storage | one flat `~/.eigen/memory/<project>.md` | layered git folder |
| injection | whole file, always | small summary only; handbook greppable |
| writing | live `memory` tool appends + `dream` distill | 2-phase background pipeline, no-op gated |
| consolidation | none (append-only) | global consolidation w/ dedup + rewrite |
| forgetting | none | git-diff-driven deletion + usage-based retention |
| retrieval | n/a (all in prompt) | quick-pass protocol + citations |
| scoping | per-project file | per-cwd blocks + applies_to/reuse_rule |
| outcome awareness | none | per-task success/partial/fail/uncertain |

## 6. Recommended adoption order for eigen

1. **Split L1/L2** (biggest win, small change): keep a curated, size-capped
   `summary` section that gets injected, move the bullet archive to a
   handbook file the agent greps via a `memory_search` tool (or just `grep` —
   the file path is already in the prompt).
2. **Consolidation pass** (the missing Phase 2): a small-model job (haiku via
   `smallProvider()`) that merges/dedups/rewrites the handbook and regenerates
   the summary; run it from `eigen dream` or a new `eigen memory consolidate`.
3. **No-op gate + preference-first prompt** for `dream.Distill` — adopt the
   Phase 1 rules (user messages over assistant messages, quote-oriented
   preference signals, outcome triage, secret redaction).
4. **Usage-tracked retention**: count which notes the model actually cites
   (cheap version: ask the model to list note ids it used; or track grep
   hits) and age out unused ones.
5. **Per-session summaries** (eigen already persists transcripts; a
   `rollout_summaries/`-style distill per session gives L3 for free).
