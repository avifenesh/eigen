# Performance & resource health (Tier 23)

eigen runs as a long-lived daemon hosting many sessions, plus nightly dreaming, a
Telegram bridge, and background subtasks. This doc captures what's bounded, the
baselines, and how to watch for regressions.

## Measure it live

```
eigen daemon stats            # default (production) instance
EIGEN_INSTANCE=dev eigen daemon stats
make stats                    # builds + runs the above
```

Reports: uptime, **goroutines**, **heap alloc/sys**, **RSS** (`/proc/self/statm`),
GC cycles, sessions / views / running-turns, in-memory bg-task records, Go version.

The two numbers to watch over days of uptime are **goroutines** and **RSS**. They
should track current activity (sessions attached, turns running) and return to a
flat baseline when idle — not climb monotonically.

## What is bounded (and where)

| Structure | Location | Bound |
|---|---|---|
| Hosted sessions | `host.sessions` | deleted on `Remove`/prune (`host.go`) |
| Per-session view subscribers | `session.subs` | removed + channel closed on detach (`session.go` attach's detach fn) |
| Per-session replay buffer | `session.events` | capped at `maxReplayEvents=4096`, trimmed in `dispatch`; reset on clear |
| Background-task records (in memory) | `BgRegistry.tasks` | capped at `maxRetainedTasks=200` terminal records via `reapLocked` in `put`; running tasks never reaped; reaped records still readable from their on-disk jsonl |
| Background-task files (on disk) | `~/.eigen/tasks[-instance]/` | reaped >7d old at daemon startup (`adoptStale`) |

If you add a new per-session / per-turn / per-task structure, give it a cap and a
trim path, and add it to the soak test below.

## Regression guard

```
make perf
```

- **`TestSoakSessionChurnNoLeak`** — 300 cycles of create→attach→detach→remove;
  asserts the sessions map drains to 0 and goroutines don't grow with cycles.
- **`TestBgRegistryReapsTerminalTasks` / `…NeverReapsRunning`** — the bg-task map
  stays ≤ cap, drops oldest terminal first, never reaps running tasks, and reaped
  tasks remain readable from disk.
- **`TestReplayBufferBoundedAndClearedAfterTurn`** — the replay buffer cap holds.

### Latency / alloc baselines (indicative, dev box)

| Benchmark | ns/op | allocs/op | Note |
|---|---|---|---|
| `BenchmarkWireEventEncode` | ~500 | 3 | per streamed event → socket JSON; the hot path |
| `BenchmarkHostStats` | ~12µs | 8 | `stats` snapshot (mostly `runtime.ReadMemStats`) — cheap to poll |

Re-run with `make perf-bench`. Treat a >2× regression in allocs/op on the wire
encode, or goroutine growth in the soak, as a real regression to investigate.

## Knobs

- `maxReplayEvents` (`internal/daemon/session.go`) — replay buffer depth.
- `maxRetainedTasks` (`internal/agent/background.go`) — in-memory bg-task records.
- bg-task disk retention: 7d (`adoptStale` in `internal/agent/taskstore.go`).

## Notes / findings

- The daemon **core** (sessions, subs, replay) was already well-bounded before
  Tier 23; the one real unbounded-growth bug was the in-memory `BgRegistry.tasks`
  map (every subtask accreted a record holding its full result text, never
  pruned at runtime — only disk files were reaped, and only at startup). Fixed by
  `reapLocked`.
- A hanging/slow model call does not leak goroutines in the daemon (turns run one
  at a time per session; the goroutine exits when the turn ends), but it does
  hold a turn slot — see the council/adversary timeout work for model-call hangs.

---

# Token efficiency (Tier 30)

Input-token cost compounds for an always-on agent: the static prefix (system
prompt + tool schemas + memory) is re-sent every turn, and subtasks multiply it.
Tier 30 made that cost visible and cut the avoidable parts.

## Measure it live

`eigen daemon stats` reports cumulative token usage across all sessions plus the
headline metric:

```
  tokens:      in 1.2M (cache: 8.4M read, 220.0K write) · out 340.0K
  cache hit:   87.5% of input tokens served from cache
```

**Cache hit rate** = `cache_read / (input + cache_read)`. A healthy always-on
agent with prompt caching should see a high hit rate — the static prefix is read
from cache after the first turn in a window. A low rate means the prefix is being
invalidated (something in system/tools/memory changes every turn) or caching is
off for the active provider.

Per-turn usage (in/out/cacheRead/cacheWrite) rides on `agent.Event` (EventDone)
and is summed in `internal/agent/agent.go`'s drive loop.

## What's cached, and where

| Provider | System prompt | Tool schemas | Cache usage parsed |
|---|---|---|---|
| Converse (default) | cachePoint | cachePoint | cacheRead/Write ✓ |
| Anthropic | ephemeral block | ephemeral on last tool ✓ (Tier 30) | cache_read/creation ✓ |
| OpenAI-compatible (glm/grok/local) | provider-side | provider-side | prompt_tokens_details.cached ✓ |
| Mantle (Responses) | provider-side | provider-side | input_tokens_details.cached ✓ |

Caching is gated by the catalog `Cache` flag per model (`internal/llm/catalog.go`)
and, for Converse, `EIGEN_CONVERSE_CACHE`. Before Tier 30 every provider DISCARDED
the cache token counts its API returned, so hit rates were invisible.

## What's bounded / trimmed

| Concern | Mechanism | Location |
|---|---|---|
| Tool JSON schemas re-sent every turn | compacted to canonical form ONCE at registration; pretty-print is render-only | `tool.NewRegistry` / `compactJSON`; render in `internal/tui/jsonview` |
| Injected memory per scope | capped at `maxInjectedBytes` (8 KiB ≈ 2K tok), keep newest | `Store.Injected` / `clampMemoryTail` |
| Stale/duplicate tool outputs | `DedupeToolResults` stubs older identical results; `ShedToolResults` sheds old payloads at compaction | `internal/llm/shed.go` |
| Transcript growth | auto-compaction fires at `compactTriggerFrac` (85%) of budget, with headroom | `Session.maybeCompact` |
| Subtask reasoning effort | trivial/easy lowered to lowest real effort (only lowers) | `applySubtaskEffort` |

## Principle

The prompt/data plane carries **canonical compact JSON** — no `MarshalIndent`
anywhere in `internal/agent`, `internal/tool`, `internal/llm`. Pretty-printing
is a **render-time** concern (the TUI re-indents + colorizes for humans). Don't
put data-shaping in `Definition.Spec()` (per-step hot path); normalize at the
authoring→runtime boundary instead.

## Knobs

- `EIGEN_CONVERSE_CACHE` — toggle Converse prompt caching.
- `EIGEN_SUBTASK_EFFORT=keep` — disable per-difficulty subtask effort lowering.
- `compactTriggerFrac` (`internal/agent/agent.go`) — compaction trigger (0.85).
- `maxInjectedBytes` (`internal/memory/memory.go`) — injected memory cap per scope.

## Indicative findings (dev box)

- Static tool text ≈ 4.9K tokens (descriptions ~1.8K + schemas ~3.1K); schema
  whitespace was ~15% before compaction.
- Memory injected ≈ 2.4K tok/turn here (global + project SUMMARY); the raw
  MEMORY.md fallback for an un-summarized scope was an unbounded token bomb
  (projects scope MEMORY.md ≈ 127K tokens) — now capped.
- Subtasks previously inherited `effort=max` globally; trivial/easy now run at
  the lowest real effort.
