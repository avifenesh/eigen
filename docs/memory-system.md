# eigen memory v2 — codex-style structured memory (the plan)

Goal: replace eigen's flat append-a-bullet memory with a **codex-style tiered,
structured, self-maintaining memory pipeline**, fold in **banthis** (the user's
negative-constraint skill) as a first-class section, and wire it cleanly from
the system level (where it's stored) up to the agent level (how the agent reads
and contributes to it). Then extend the same substrate to tasks, subskills, and
the proactive feed.

This builds on what already exists in eigen — `internal/dream` (Distill /
Consolidate / SynthesizeSkill / RenderSession), `internal/memory` (Store,
Sections), the `BgRegistry` background-job infra, `internal/transcript`, and the
skill `Discover`/`Catalog` loader — and on the reverse-engineered codex design
(see project memory, 2026-06-16): a `memories_1.sqlite` leased job queue
(stage1 per-session → phase2 consolidate) producing three tiers
(`raw_memories.md` → `MEMORY.md` → `memory_summary.md`, only the summary
injected), per-session structured **rollout summaries**, `usage_count` for
forgetting, and a git-versioned memory dir. claude contributes the
**banthis "Banned behaviors"** block: hard prohibitions that outrank the current
turn.

---

## Design principles (from the research)

1. **Three tiers; inject only the summary.** Raw (append-only, never injected) →
   curated (structured, the working memory) → summary (small, the thing loaded
   into every prompt). Eigen today injects the *entire* growing file — the bug
   behind the 925-line `projects` memory.
2. **The unit is a structured per-session summary**, not a loose bullet. Fields
   (codex's shape, proven high-signal): outcome (success/partial/failed),
   **preference signals = verbatim user quote → inferred rule**, key steps,
   **failures & how to do differently** (negative knowledge), reusable knowledge.
3. **Self-maintaining.** Consolidation + forgetting run automatically (idle /
   nightly), not just a manual command. Usage tracking ranks what survives.
4. **Negative constraints are first-class** (banthis): a managed
   "Do NOT" block of hard rules that win over the current turn.
5. **Self-improvement, not just facts.** Recurring friction → proposed subskills
   / workflows / config, surfaced for the user (never auto-applied).
6. **Provenance + safety throughout.** Transcripts are DATA not instructions;
   never store secrets; weight user messages over assistant claims; every
   rewrite keeps a backup (already true) + git history (new).

---

## LAYER 1 — System level: where memory is stored

`~/.eigen/memory/` becomes a structured store (git-versioned), per scope
(project key = session dir hash, as today; plus `global`):

```
~/.eigen/memory/
  <project>/                     # was a single <project>.md
    raw/                         # tier 1: append-only, NEVER injected
      <ts>-<slug>.md             #   one structured rollout summary per session
    MEMORY.md                    # tier 2: curated working memory (structured)
    SUMMARY.md                   # tier 3: the small file injected into prompts
    bans.md                      # banthis-managed "Banned behaviors" block
  global/                        # same shape, cross-project
  index.sqlite                   # job queue + usage/forgetting bookkeeping
  .git/                          # versioned memory (git init on first write)
```

- **`index.sqlite`** (mirrors codex's `memories_1.sqlite`): tables
  `summaries(scope, slug, session_id, generated_at, raw_path, usage_count,
  last_used, in_memory bool)` and `jobs(kind, scope, status, lease_until,
  retry_remaining, last_error, watermark)`. Leased queue so generation is a
  background job, never blocks a turn, survives restart, retries.
- **Migration**: existing flat `<project>.md` files import as the initial
  `MEMORY.md` for that scope (one-time, on first v2 run); a first `SUMMARY.md`
  is generated from it. The mis-keyed `projects` file is consolidated in place
  (it's legitimately one parent-dir session's context).
- **Backups + git**: every consolidation keeps the existing timestamped `.bak`
  AND commits to the memory git repo (cheap, local, fully revertable history).

New surface in `internal/memory`:
- `Store` gains tiered paths (`RawDir`, `MemoryPath`, `SummaryPath`, `BansPath`).
- `Store.InjectedContext()` returns SUMMARY.md + bans.md (NOT raw, NOT full
  MEMORY.md) — this is what `memory.Sections` returns to the prompt.
- `memstore` (new, small): the `index.sqlite` accessor + job queue.

## LAYER 2 — Generation: the pipeline (background jobs)

Reuse `BgRegistry`'s leased-job pattern; add memory job kinds run by the daemon
(machine-idle) and on `eigen dream`:

- **`mem_stage1` (per session)** — input: a session transcript (eigen daemon
  sessions + foreign sources via `internal/transcript`, as dream already reads).
  Output: a structured **rollout summary** written to `raw/<ts>-<slug>.md` and a
  row in `summaries`. Prompt extends `dream.reflectPrompt` to emit the codex
  fields (outcome / preference-signals-as-quote→rule / key steps / failures /
  reusable). Idempotent per session_id + transcript watermark (don't re-summarize
  unchanged sessions). Robust to the codex failure modes we saw (context
  overflow → chunk; model-not-found → fall back; validation → skip + record).
- **`mem_consolidate` (per scope)** — input: the raw summaries + current
  MEMORY.md. Output: a rewritten, de-duplicated, **structured MEMORY.md**
  (grouped: User Profile / Preferences / Reusable knowledge / Failures /
  per-area facts), via an evolved `dream.Consolidate`. Triggered when MEMORY.md
  crosses a size/age threshold, or nightly. Keeps backup + git commit.
- **`mem_summary` (per scope)** — distills MEMORY.md → the small `SUMMARY.md`
  that is actually injected (the User Profile + top preferences + most-used
  reusable facts + bans). This is the codex `memory_summary.md` tier — the fix
  for unbounded prompt growth.
- **`mem_forget` (per scope)** — uses `usage_count`/`last_used` to demote rarely-
  used facts out of SUMMARY.md (kept in MEMORY.md/raw, just not injected).

Trigger model:
- **Idle (TUI)** — keep today's idle-dream timer, but it now enqueues
  `mem_stage1` for the just-ended session, then `mem_consolidate`/`mem_summary`
  if thresholds crossed.
- **Nightly (daemon)** — a daemon timer (systemd or internal ticker) runs the
  full pipeline machine-wide across all scopes when idle — the real "dreaming".
- **Manual** — `eigen dream` runs stage1+consolidate+summary now; `eigen memory
  consolidate` stays.

## LAYER 3 — Agent level: how the agent reads + contributes

**Reads (passive, every turn):** `memory.Sections` returns `SUMMARY.md` +
`bans.md` for the session's scope (project) and global — injected as today via
`Options.Memory`. Small + structured, so the prompt stays lean. The bans block
is rendered with the banthis framing ("hard prohibitions, outrank the current
turn") so the model treats it as system-priority.

**Contributes (active):**
- The existing **`memory` tool** keeps writing durable notes mid-session — but
  now they land in `raw/` as a lightweight summary entry (and feed the next
  consolidation), not appended to the injected file.
- A **`ban` capability** (port banthis natively, since eigen is self-contained):
  the `memory` tool gains a `kind: "ban"` (or a `/ban` command) that adds a hard
  prohibition to `bans.md` — the user's banthis skill, first-class in eigen.
- **Awareness**: the system prompt tells the agent it has structured memory +
  how to contribute (record a durable fact, add a ban) — one short paragraph,
  like the tool-awareness section.

**The agent never sees the raw tier or runs consolidation inline** — that's the
background pipeline's job, keeping turns fast.

---

## Where else this substrate is used (beyond chat memory)

The same raw→structured→summary + job-queue machinery generalizes:

1. **Tasks / Task Groups (codex's organizing unit).** Stage1 summaries already
   carry outcome + task framing. Group sessions into **task groups** (a piece of
   work spanning sessions) so memory and the app can show "what was this effort,
   how did it go, what's the reusable takeaway" — and the proactive feed can
   surface "you left task X partial".
2. **Subskills (the self-improvement headline).** `dream.SynthesizeSkill` exists;
   wire it into the pipeline: when consolidation detects **recurring friction or
   a repeated manual recipe** across summaries, propose a SKILL.md draft (or a
   workflow) into a staging area; the user accepts → it installs to
   `~/.eigen/skills/`. Memory becomes the source that grows eigen's own skills.
3. **Banned behaviors → behavior, everywhere.** `bans.md` isn't only chat: it's
   injected for subagents and (eventually) gates risky actions. The negative-
   constraint layer is a cross-cutting safety/quality net.
4. **Proactive feed.** Forgetting + usage data + partial-task detection feed the
   feed/suggestions ("consolidate memory", "finish task X", "promote recipe Y to
   a skill") — dreaming becomes proactive, not silent.
5. **Cross-project / global profile.** The `global` scope's SUMMARY.md is the
   user profile (codex's "User Profile" + preferences) — the substrate for "move
   all my work onto eigen": consistent working-style memory everywhere.

---

## Build order (staged; substrate first)

- **S1 — Tiered store + injection fix (urgent).** `internal/memory` tiered paths
  + `InjectedContext()`; migrate flat files → MEMORY.md; generate SUMMARY.md;
  inject only summary+bans. Stops prompt bloat immediately.
- **S2 — index.sqlite + job queue + git versioning.** The bookkeeping/lease
  layer; `mem_*` job kinds; git init/commit on rewrite.
- **S3 — Structured stage1 summaries.** Evolve the reflect prompt to codex's
  fields; write `raw/<ts>-<slug>.md`; idempotent by session+watermark.
- **S4 — Auto consolidate + summary + forget.** Thresholds; nightly daemon
  trigger; usage tracking.
- **S5 — banthis native (bans.md + /ban + injection framing).**
- **S6 — Self-improvement: subskill/workflow proposals from recurring friction.**
- **S7 — Reuse: task groups, feed surfacing, global profile.**

Each stage ships behind the existing verify gate (build/vet/test/-race/
staticcheck) and is live-verified before the next.
