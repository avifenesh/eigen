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
