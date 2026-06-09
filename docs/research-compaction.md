# Research: Compaction & token-saving in coding-agent harnesses

Reverse-engineered from the installed Claude Code 2.1.170 binary and the Codex
binary (string + structure inspection — no `claude -p` quota burned), the
authoritative Anthropic web docs/essay (fetched via the `.md` doc endpoints),
plus a read of eigen's own compaction code. Companion to
`research-codex-memory.md`. Goal: learn the best harnesses' techniques, then
build the worthwhile ones on top of eigen's existing machinery rather than
redesigning.

Primary web sources (all fetched this session):
- Anthropic, "Effective context engineering for AI agents" (engineering blog, 2025-09-29)
- Anthropic API docs: Context editing (`context-management-2025-06-27`)
- Anthropic API docs: Compaction (`compact-2026-01-12`)
- Anthropic API docs: Client-side SDK compaction (tool_runner)
- Codex (OpenAI) open source `codex-rs`: `core/src/compact.rs`,
  `core/src/session/turn.rs`, `core/src/state/auto_compact_window.rs`,
  `protocol/src/config_types.rs`, `prompts/templates/compact/*.md`
- Chroma "context rot" study (cited by the essay)

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

## 5b. Web sources (authoritative) — cross-checked against the binaries

Fetched the primary vendor materials (via the `.md` doc endpoints + the essay).
These confirm the binary findings and pin down exact parameter names/defaults.

### Anthropic — "Effective context engineering for AI agents" (2025-09-29)
The *why*, and the canonical framework. Key claims, verbatim-sourced:
- **Context rot**: "as the number of tokens in the context window increases, the
  model's ability to accurately recall information from that context decreases"
  (cites Chroma's context-rot study). Emerges across *all* models.
- **Attention budget**: transformers have n² pairwise token relationships; long
  context stretches attention thin. Context is "a finite resource with
  diminishing marginal returns."
- **The guiding principle** (worth quoting in eigen's own compaction prompt):
  *"find the smallest possible set of high-signal tokens that maximize the
  likelihood of your desired outcome."*
- **Tools**: bloated tool sets are a top failure mode — "if a human engineer
  can't definitively say which tool should be used, an AI agent can't be
  expected to do better." Tools must be token-efficient.
- **Just-in-time retrieval**: keep lightweight identifiers (file paths, queries,
  links) and load data at runtime, vs dumping everything up front. Claude Code
  uses a *hybrid*: CLAUDE.md up front + glob/grep just-in-time. (eigen already
  does this — read/grep/glob, not a pre-indexed dump. ✓)
- **Three long-horizon techniques**, in order of preference:
  1. **Compaction** — summarize near the limit, reinitialize. Claude Code keeps
     "the compressed context **plus the five most recently accessed files**."
     Tuning recipe: *maximize recall first* (capture everything relevant), *then*
     improve precision. "The art … lies in the selection of what to keep vs
     discard."
  2. **Structured note-taking** (agentic memory) — NOTES.md / to-do list
     persisted outside context, pulled back later. (eigen has the memory tool. ✓)
  3. **Sub-agent architectures** — clean context per sub-agent; each returns a
     condensed 1-2k-token summary. (eigen has the `task` tool. ✓)
- **Tool-result clearing is named the "safest, lightest-touch form of
  compaction."** ← direct endorsement of recommendation #1.

### Anthropic — Context editing API (beta `context-management-2025-06-27`)
Server-side, applied before the prompt reaches the model; the client keeps the
full history. Two strategies (exact params, for our client-side port):
- **`clear_tool_uses_20250919`** (tool-result clearing):
  - `trigger` default **100k input tokens** (or `tool_uses`)
  - `keep` default **3** most-recent tool-use/result pairs
  - `clear_at_least` — min tokens to clear so it's *worth* breaking the cache
  - `exclude_tools` — never clear these (e.g. a pinned read)
  - `clear_tool_inputs` default **false** → keeps the tool *call*, clears only
    the *result*; replaces each with placeholder text. ← exactly eigen rec #1.
- **`clear_thinking_20251015`** (thinking-block clearing): `keep` =
  `{thinking_turns: N}` or `"all"`. Kept thinking ⇒ cache preserved; cleared ⇒
  cache invalidated at the clear point. Opus 4.5+/Sonnet 4.6+ keep all by default.
- **Cache note**: clearing invalidates the cached prefix → `clear_at_least`
  exists precisely so you clear enough to make the re-cache worthwhile. Confirms
  eigen rec #4 (fixed prefix) matters.
- **Pairs with the memory tool**: when nearing the threshold the model gets an
  automatic warning to persist anything important to memory before it's cleared.

### Anthropic — Compaction API (beta `compact-2026-01-12`)
A dedicated newer server-side compaction (distinct from context-editing):
- Emits a `compaction` block; on later requests **all blocks before it are
  dropped** automatically.
- `trigger` default **150k** input tokens (min 50k).
- `pause_after_compaction` → returns `stop_reason: "compaction"` so you can
  splice verbatim-preserved recent turns *after* the summary (their example
  keeps the last 3 messages). ← this is the clean way to do eigen rec #3+#4.
- `instructions` replaces the summary prompt entirely.
- **Gotcha**: with tools defined, the model sometimes *calls a tool* instead of
  summarizing → `compaction` block with `content: null`; fix by instructing
  "respond with text only, do not call tools." (eigen's summarizer renders to
  plain text and doesn't expose tools, so we already dodge this. ✓)
- **Token-budget enforcement** pattern: count compactions × trigger to estimate
  cumulative spend and inject a "wrap up now" message past a budget. ← maps to
  eigen's Codex-style per-goal budget idea.
- The **real default compaction prompt** (shorter than eigen's): *"You have
  written a partial transcript for the initial task above. Please write a
  summary … to provide continuity so you can continue … Write down anything that
  would be helpful, including the state, next steps, learnings etc. Wrap your
  summary in `<summary></summary>`."* eigen's 6-section schema is *richer* — keep
  it (the SDK's client-side default uses the same 5-section structure eigen has).

### Anthropic — Client-side SDK compaction
Anthropic **explicitly recommends server-side over client-side**, but the
client-side design mirrors eigen's exactly and validates two choices:
- Threshold = `input + cache_creation + cache_read + output` tokens.
- Summary replaces the whole history, wrapped in `<summary>` tags, 5-section
  structure (Task Overview / Current State / Important Discoveries incl. *failed
  approaches* / Next Steps / Context to Preserve). eigen's prompt already has
  all of this.
- **A cheaper model can generate summaries** (`model: claude-haiku-4-5`). ←
  eigen should point `Compactor` at `smallProvider()` (haiku), not the main
  model. New, concrete, cheap win.
- Documented **footgun**: server-side tools inflate `cache_read_input_tokens`,
  triggering compaction prematurely → use token-counting, not the naive sum.

### Net effect on the plan
Nothing in the web sources contradicts §5; they *strengthen* it and add specifics:
- Rec #1 (tool-result shedding) is Anthropic's own "safest, lightest-touch"
  lever — build it first, with their param shape: `keep=3`, a `trigger`
  threshold, an `exclude_tools`-style pin, and stub text in place of results.
- Rec #4 (fixed prefix): cache the system prompt behind its own breakpoint
  (eigen already sets cache points — verify the summary write doesn't invalidate
  the system prefix).
- **New cheap win**: summarize with the small/haiku provider, not the main model.
- **New**: keep "the N most-recently-accessed files" verbatim through compaction
  (Claude Code keeps 5) — a coding-specific recall booster.
- Keep eigen's richer 6-section summary schema; optionally borrow the explicit
  "do not call tools / text only" guard if we ever let the summarizer see tools.

## 5c. Codex deep-dive (binary + open source `codex-rs`)

Codex is open source and the binary leaks its source paths (`core/src/compact.rs`),
so the authoritative implementation is readable directly. Far richer than the
strings suggested. Files: `core/src/compact.rs`, `core/src/session/turn.rs`,
`core/src/state/auto_compact_window.rs`, `protocol/src/config_types.rs`,
`prompts/templates/compact/`.

### The summarization prompt (verbatim, `prompts/templates/compact/prompt.md`)
> You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary
> for another LLM that will resume the task.
> Include:
> - Current progress and key decisions made
> - Important context, constraints, or user preferences
> - What remains to be done (clear next steps)
> - Any critical data, examples, or references needed to continue
> Be concise, structured, and focused on helping the next LLM seamlessly continue the work.

Notably **shorter** than eigen's 6-section schema and Anthropic's 5-section one.
Codex leans on a *framing* trick instead of a long schema (see prefix below).

### The summary re-injection prefix (verbatim, `summary_prefix.md`)
When the summary is fed back, it's prefixed with:
> Another language model started to solve this problem and produced a summary of
> its thinking process. You also have access to the state of the tools that were
> used by that language model. Use this to build on the work that has already
> been done and avoid duplicating work. Here is the summary produced by the other
> language model…

This "another model did this, build on it" third-person framing is a deliberate
device — the summary becomes a `user` message with this prefix, not a fake
assistant turn. eigen's injected text ("Original task: … Summary follows") is the
same idea but first-person; Codex's third-person handoff framing is worth borrowing.

### Architecture (the genuinely good parts)
- **Three compaction call sites**, all funnel through `run_compact_task_inner_impl`:
  1. **Pre-turn / pre-sampling** (`run_pre_sampling_compact`): before sending a
     turn, if the budget is exhausted → compact with `DoNotInject` (next turn
     re-adds full initial context).
  2. **Mid-turn** (`turn.rs` loop): after a sampling step, if
     `token_limit_reached && needs_follow_up` → compact with
     `BeforeLastUserMessage` injection, then `continue` the loop.
  3. **Manual** (`/compact`, `run_compact_task`).
- **The compaction "turn" IS a model call**: it appends the compaction prompt as
  a user message and *streams a normal completion* — the model writes the summary
  as its assistant message (`get_last_assistant_message_from_turn`). Same as
  eigen's `Compactor.Summarize`, but Codex reuses the **same turn/stream/retry
  machinery** (one `client_session`, sticky routing survives retries).
- **`build_compacted_history`** — the new history is NOT just "summary + recent":
  it is `[initial_context?] + [most-recent USER messages up to
  COMPACT_USER_MESSAGE_MAX_TOKENS=20_000] + [summary]`. So it preserves a
  **token-bounded tail of recent *user* messages verbatim**, newest-first, then
  the summary last. (eigen keeps recent *rounds*; Codex keeps recent *user
  messages* + a 20k cap. The user-message focus is a cheap recall booster.)
- **ContextWindowExceeded during compaction** is handled by
  `history.remove_first_item()` and retrying — **trim from the front to preserve
  the cache prefix**, never from the recent end. Direct confirmation of eigen
  rec #4 (fixed prefix) and a concrete fallback for "even the summary request
  overflows."
- **Initial-context injection boundary** (`insert_initial_context_before_last_real_user_or_summary`):
  on mid-turn compaction the canonical context (system/env/initial files) is
  re-inserted *just above the last real user message*, so the summary stays last
  (the model is trained to expect the summary as the final item). Precise
  ordering rule worth copying if eigen ever does mid-turn compaction.

### The token-limit / trigger logic (`auto_compact_token_status`, turn.rs)
- Two **scopes** (`model_auto_compact_token_limit_scope`,
  `AutoCompactTokenLimitScope`):
  - **`Total`** (default): charge the *full active context* against the limit.
  - **`BodyAfterPrefix`**: charge only *growth after the carried window prefix* —
    i.e. subtract a baseline (`prefill_input_tokens`, the server-observed input
    tokens of the first request in this compaction window) from current usage.
    This is the clever bit: it measures "how much has this turn *grown*", not
    total size, so a big stable cached prefix doesn't keep re-triggering.
    (`state/auto_compact_window.rs` tracks the baseline; server-observed usage
    replaces the estimate when available; `start_next()` bumps the window ordinal
    after each compaction.)
- **`effective_context_window_percent`**: the usable window =
  `model_context_window * percent / 100` (a built-in safety buffer below the raw
  window — Codex's equivalent of eigen's 200k cap / Claude Code's autocompact
  buffer).
- **Trigger** = `auto_compact_scope_tokens >= auto_compact_scope_limit ||
  active_context_tokens >= full_context_window_limit`. Limit comes from config
  `model_auto_compact_token_limit` else the model catalog's
  `auto_compact_token_limit()`.
- **Model-switch compaction** (`maybe_run_previous_model_inline_compact`): when
  switching to a *smaller-context* model, it compacts *with the previous model*
  first so the history fits the new window. (eigen has live `/model` switching +
  failover → this is directly relevant: switching opus→a smaller model mid-session
  should compact-to-fit, not 400.)
- Loop-safety comment, verbatim: *"as long as compaction works well in getting us
  way below the token limit, we shouldn't worry about being in an infinite loop."*
  → Codex's anti-thrash defense is "compact aggressively enough that you drop far
  below the limit" rather than Claude Code's explicit circuit breaker. eigen
  should do **both**: compact to a *fraction* of budget (already does — budget/2)
  AND keep the circuit breaker (rec #2) as a backstop.

### Hooks & analytics (context, not for eigen now)
- `PreCompact`/`PostCompact` hooks can stop compaction; rich analytics
  (`CodexCompactionEvent`: before/after tokens, retained_image_count, summary
  tokens, trigger/reason/phase, duration). `CompactionStrategy::Memento` is the
  internal name. eigen doesn't need this, but the **before/after token + reason
  telemetry** is a cheap thing to log for tuning the circuit breaker.

### Net additions to the plan from Codex source
- **Borrow the third-person handoff prefix** when re-injecting the summary
  ("another model produced this summary, build on it, avoid duplicating work") —
  cheap wording change, Codex-proven.
- **Trim-from-front-on-overflow**: if the summary request itself overflows, drop
  oldest items and retry (preserves cache prefix) — concrete impl of rec #4 +
  the buffer caution.
- **Keep a token-bounded tail of recent USER messages verbatim** (Codex: 20k)
  alongside the summary — complements "keep N recent files" from Anthropic.
- **Compact-to-fit on model downsize**: tie into eigen's `/model` + failover so a
  switch to a smaller window compacts first instead of erroring.
- **"Growth-after-prefix" trigger scope** is the most novel idea: trigger on how
  much the context has *grown since the last compaction*, not total size — avoids
  a large stable cached prefix constantly re-arming the trigger. Worth considering
  for eigen's threshold once prompt caching dominates the prefix.
- Confirms again: short prompt + strong framing can beat a long schema; both
  vendors keep the *most recent* material verbatim and summarize only the old.

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
