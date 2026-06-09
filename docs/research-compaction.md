# Research: Compaction & token-saving in coding-agent harnesses

Reverse-engineered from the installed Claude Code 2.1.170 binary and the Codex
binary (string + structure inspection — no `claude -p` quota burned), plus a
read of eigen's own compaction code. Companion to `research-codex-memory.md`.
Goal: learn the best harnesses' techniques, then build the worthwhile ones on
top of eigen's existing machinery rather than redesigning.

---

## 1. What eigen does today (baseline)

- **One estimator**: `llm.EstimateTokens` = `chars/4 + 16/msg`. Rough, provider-agnostic.
- **One budget**: `Agent.MaxContextTokens` (auto = min(window·0.85, 200k); override
  `--max-tokens`/`EIGEN_MAX_CONTEXT_TOKENS`).
- **Trigger**: at the *start of every turn* (`Session.drive`), if over budget,
  compact. Also on `/compact` (forces to budget/2).
- **Two strategies** (`internal/llm/compact*.go`):
  - `Compact` (deterministic): drop oldest whole rounds (cut only at user
    boundaries so no tool-call/result pair is orphaned), note "N messages omitted".
  - `CompactWith` (model summary): keep ~45% recent budget verbatim, replace
    older history with one structured summary (intent/decisions/files/state/
    next/gotchas), preserve the original task verbatim, fall back to `Compact`
    on any failure. Fails safe.
- **Prompt caching**: already wired — Converse `cachePoint` after system prompt
  and after the tool-definition prefix; native Anthropic `cache_control:
  ephemeral` on the system block. This is a *cost* lever (cache reads are ~10%
  the price), not a *context-size* lever.
- **Tool-output cap**: `maxToolOutput = 100_000` chars, then truncate.

**Verdict**: eigen's summary compaction is already structurally similar to
Claude Code's `/compact`. The gaps are everything *around* it — when it fires,
what it preserves cheaply, and avoiding repeated full re-summarization.

---

## 2. Claude Code (the gold standard here)

### 2.1 Autocompact — a *threshold* with a *buffer*, not "when full"
- Setting `autoCompactThreshold` / auto-compact window; the *effective* trigger
  is `min(setting, model_max_window)`. Override env
  `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`. Observed candidate ratios cluster at
  **0.92** (the strings carry 0.91–0.99 — a tunable band).
- An **"Autocompact buffer" / "Compact buffer"**: space reserved *below* the
  limit so the compaction request itself (which must send the whole history to
  summarize) doesn't blow the window.
- Proactive nudge before it fires: *"Autocompact will trigger soon, which
  discards older messages. Use /compact now to control what gets kept."* — the
  user gets agency before automatic loss.
- It can be disabled (`/config`), with a clear fallback message pointing at
  `/compact`.

### 2.2 Microcompaction — clear stale tool results, keep the conversation
- Separate from full compaction: `microcompact_boundary`,
  `tengu_time_based_microcompact`, `applyAutoCompactWindow`.
- The mechanism (confirmed by the `anthropic-beta` flags Claude Code sends):
  **server-side context management**. The request carries
  `context_management: { edits: [{ type: "clear_thinking_20251015", keep: "all" }] }`
  and the beta `context-management-2025-06-27`. Anthropic's API itself drops old
  **thinking blocks** (and can drop old tool results) so the model keeps recent
  reasoning but doesn't pay for stale chain-of-thought every turn.
- Key idea eigen can copy *client-side*: **old tool results are the cheapest
  thing to shed**. A `read` of a 2k-line file 20 turns ago rarely matters now;
  its result can be replaced with a stub (`[output cleared to save context —
  re-read if needed]`) long before you must summarize the actual conversation.

### 2.3 "Fixed prefix" — protect the cached stable head
- `autocompact: fixed prefix ~<n>` in the logs. Compaction is computed against a
  **fixed prefix** (system prompt + tool defs + maybe the first user turn) that
  is *never* rewritten, so the prompt cache prefix stays valid across compaction.
  Naive summarization invalidates the whole cache; a fixed prefix preserves it.

### 2.4 Circuit breaker — don't thrash
- `autocompact: circuit breaker tripped after …` and *"Autocompact is thrashing:
  the context refilled to the limit within …"*. If compaction is immediately
  followed by re-overflow, Claude Code **stops auto-compacting and warns** rather
  than entering a summarize-every-turn death spiral (which burns tokens *and*
  degrades quality). eigen has NO such guard — its start-of-turn check could in
  principle re-summarize repeatedly.

### 2.5 Quality awareness
- Both Claude Code and Codex warn that **repeated compaction degrades accuracy**
  (Codex: *"Long threads and multiple compactions can cause the model to be less
  accurate. Start a new thread when possible."*). Compaction is lossy; the harness
  should minimize *how often* it summarizes, not just *whether* it fits.

### 2.6 Error-driven compaction
- Claude Code parses `prompt is too long … N tokens > M` and `request_too_large`
  from API 400/413s and reactively compacts / strips the offending media. It also
  special-cases "single exchange cannot be compacted" (the bloat is system prompt
  + tools + attachments, not history). eigen currently just surfaces the error.

---

## 3. Codex (cross-check)

- **Per-goal token budget**: `token_budget` / `model_auto_compact_token_limit_scope`
  — budgets are scoped to a *goal/thread*, not global. *"The active thread goal
  has reached its token budget."*
- **Checkpoint-handoff prompt**: *"You are performing a CONTEXT CHECKPOINT
  COMPACTION. Create a handoff summary for another LLM that will resume the
  task."* — same intent-preserving-handoff framing eigen's summary prompt uses.
- **Sub-agent summary handoff**: *"Another language model started to solve this
  problem and produced a summary of its thinking process. You also have access to
  the state of the tools that were used…"* — compaction and sub-agent delegation
  share the same "summarize state for a fresh context" primitive. eigen's `task`
  tool could reuse the compaction summarizer.
- **`/compact` is the headline advice** for long threads; **start-a-new-thread**
  is the recommended escape when quality matters more than continuity.
- **`max_output_tokens` per tool** (default 10k for exec) — tool-result budgets
  are per-tool, not one global cap.

---

## 4. Gap analysis for eigen (ranked by value)

| # | Technique | Have? | Value | Effort |
|---|-----------|-------|-------|--------|
| 1 | **Microcompaction**: shed/stub old tool results before summarizing | ❌ | ★★★ | low–med |
| 2 | **Circuit breaker**: stop re-compacting when it doesn't help; warn | ❌ | ★★★ | low |
| 3 | **Threshold + buffer + proactive nudge** (compact at ~85–90%, not at 100%) | partial (start-of-turn only) | ★★ | low |
| 4 | **Fixed prefix** so compaction keeps the prompt cache valid | ❌ | ★★ | med |
| 5 | **Error-driven compaction** on 413/"prompt too long" | ❌ | ★★ | low |
| 6 | **Per-tool output budgets** (not one 100k cap) | partial | ★ | low |
| 7 | Real token usage from API `usage` (replace the chars/4 estimate) | ❌ | ★ | med |
| 8 | Compaction-quality nudge ("consider /clear for a fresh thread") | ❌ | ★ | trivial |

Deliberately **not** adopting: server-side `context_management` (Anthropic-only,
ties us to one provider — but #1 is the portable client-side equivalent);
per-goal budget scoping (eigen has no multi-goal threads yet).

---

## 5. Recommended build order (smallest, highest-value first)

1. **Microcompaction (client-side tool-result shedding).** Before doing a full
   model summary, walk the history oldest→newest and replace tool *results*
   older than the recent window with a short stub
   (`[result elided to save context]`), keeping the tool *call* so pairing stays
   valid. Re-estimate; only if still over budget do the expensive summary. This
   is the single biggest token saver for coding agents (tool output dominates),
   it's lossy in the cheapest possible place, and it's provider-agnostic.
   - Tunable: keep last N rounds' results verbatim; stub older ones.

2. **Circuit breaker.** Track the last compaction's (before,after) and the turn
   count since. If a fresh turn is already over budget within K turns of the last
   compaction AND the last compaction freed < X%, stop auto-compacting, surface a
   note: *"context keeps refilling — consider /clear or a more focused task"*.
   Prevents the summarize-every-turn spiral. ~30 lines + a field on Session.

3. **Threshold + proactive nudge.** Compact at a *fraction* of the budget (e.g.
   0.85) with a reserved buffer for the summary request, and emit a one-line TUI
   nudge as usage crosses ~0.8 so the user can `/compact` deliberately first.
   (The colored ctx indicator already shipped — this is the action half.)

4. **Fixed prefix.** Make `CompactWith` never rewrite the first user turn / system
   framing, so the cache prefix survives. Partly true already (original task is
   preserved) — formalize it so cache hit-rate stays high across compaction.

5. **Error-driven compaction.** Detect 413 / "prompt is too long: N > M" in the
   providers, and on that error force a compaction + one retry (eigen already has
   overload→failover plumbing in the TUI; this is the context-size sibling).

Each is independently shippable, testable with the existing fake-provider
harness, and degrades safe. None requires a provider-specific API except the
deliberately-skipped server-side context_management.

---

## 6. Cautions (from the sources)

- **Compaction is lossy and compounds.** Both vendors warn repeated compaction
  hurts accuracy. The win is fewer, smarter compactions (shed tool results first;
  circuit-break the spiral), not more aggressive summarizing.
- **The summary request itself costs the whole window.** Always reserve a buffer
  below the limit, or the compaction call 413s — the bug the buffer prevents.
- **No context-anxiety UX for the agent** (user's standing rule): these are
  mechanical levers; do NOT surface a budget gauge to the model. The colored ctx
  bar is for the *human*; compaction stays automatic.
- **Estimate vs. actual**: real `usage` numbers would make thresholds precise,
  but it's the lowest-value item — the chars/4 estimate is honest and the 200k
  cap already leaves headroom.
