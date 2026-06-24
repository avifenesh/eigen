# agent/ — multi-agent, background tasks, worktrees

> `internal/agent` is the heart of eigen's autonomy: it drives a provider through the tool-use loop
> (call model → run the tool calls it returns → feed results back → repeat until a final answer),
> and layers on everything that makes the agent more than a single conversation. A `Session` owns the
> growing transcript with live-switchable config (model/perm/budget), proactive auto-compaction with a
> circuit breaker, and mid-turn "steer" injection. On top sits the multi-agent layer: `Subtask`
> (foreground delegation that auto-promotes to background past a front window), `SubtaskBackground`
> (detached, durable, process-survivable tasks under `~/.eigen/tasks`), `TaskGroup` (parallel read-only
> fan-out by role) and `TaskGroupMutating` (parallel implementer children in isolated git worktrees
> whose diffs are merged behind one approval). It also owns the goal/judge mechanism (a persistent
> north-star verified by an independent judge), named sub-agent roles (built-in + plugin), and a stall
> watchdog. The package deliberately builds NO providers or tool policies itself — `main` injects those
> (Router, ModelProvider, WorktreeTools, Bg, Policy) so the loop stays the single seam between
> front-ends (TUI/daemon/GUI) and the LLM. All front-ends (CLI/TUI/daemon/GUI) consume it via `Agent`,
> `Session`, and `BgRegistry`.

## Files

### internal/agent/agent.go
- **Role:** The core tool-use loop, the `Agent`/`Session` types, live-switchable config, compaction policy, foreground subtask delegation, and tool dispatch/permission gating.
- **Key symbols:**
  - `Agent` — the loop driver; holds Provider/Tools/Perm/Compactor plus injected seams (Router, ModelProvider, Bg, Policy, Shells, WorktreeTools, SessionDir). Four fields (Provider/Perm/Compactor/MaxContextTokens) are mutex-guarded for live switching mid-turn.
  - `Session` — a running conversation (msgs + RWMutex); circuit-breaker state for auto-compaction; `steer`/`allowTools` per-turn slots.
  - `Permission` / `PermGated` / `PermAuto` — autonomy posture (gated asks before mutating tools; auto runs everything).
  - `Event` / `EventKind` / `EventSink` — the structured event stream (text/reasoning deltas, tool lifecycle, done, note, approval, bg-done); the single front-end seam.
  - `Approver` — callback deciding whether a gated mutating tool may run.
  - `(*Session).Send` / `SendWith` / `Resend` — append a user message (with optional images) and drive the loop; `Resend` retries existing history (overload failover).
  - `(*Session).drive` — the loop itself: per-step steer drain, goal/shell-status/compaction injection, progressive tool disclosure, stream-or-complete, empty/reasoning-only turn handling, tool dispatch, context-overflow retry.
  - `(*Session).Steer` / `drainSteer` / `hasSteer` / `FlushSteer` — inject a message into a running turn; FlushSteer makes a pending steer durable on shutdown.
  - `(*Session).SetTurnTools` / `turnTools` — restrict the next turn to a slash command's `allowed-tools`.
  - `(*Session).Compact` / `maybeCompact` / `maybeCompactWithNotes` / `forceCompactOnOverflow` — on-demand, proactive (trigger/target/stall fractions), and post-overflow compaction.
  - `(*Agent).Subtask` / `SubtaskWith` — run a delegated task on a fresh sub-agent; foreground with idle-stall + front-window→background promotion.
  - `(*Agent).subAgent` — build the sub-agent for one delegation (role filter → explicit model → router → inherit), applying effort/fast discipline; precedence and the "where it ran" note.
  - `(*Agent).runChild` — run a foreground child with detached context, heartbeat, stall watch, and promotion (shared by Subtask and group fan-out).
  - `(*Agent).dispatch` / `runTool` — enforce allow-list + permission posture, run the tool (panic-recovered), cap output at `maxToolOutput`.
  - `(*Agent).SetLive` / `SetPerm` / `SetGoal` / `SetMaxContextTokens` / `UnlockTools` / `AddDir` — live config mutators (TUI calls these mid-turn). Read side: `CurrentProvider` / `CurrentGoal` / `CurrentPerm` / `CurrentMaxContextTokens` / `Roots` are race-safe accessors the daemon's state snapshot uses while a `/model` swap may be in flight.
  - `(*Agent).Run` / `(*Agent).NewSession` / `(*Agent).Resume` — entrypoints to start a one-shot or stateful session.
  - `applySubtaskEffort` / `applySubtaskFast` / `effortRank` / `subtaskDisciplineApplies` / `subtaskEffortFloor` / `joinWhere` — effort/fast discipline for trivial/easy subtasks: cap reasoning at the `medium` floor + enable the fast path, but only on a freshly-built provider the subtask EXCLUSIVELY owns (never mutate the shared parent/router provider); `subtaskDisciplineApplies` gates the cost of that own-provider build, `joinWhere` stitches the "where it ran" note. Opt out with `EIGEN_SUBTASK_EFFORT=keep`.
  - `BashDetachCh` / `DetachBash` / `Shelled` — backgrounded-shell detach plumbing.
- **Depends on:** `internal/llm` (Provider, Message, Request/Response, Compactor, EstimateTokens, ShedToolImages, DedupeToolResults, IsContextOverflow, Streamer), `internal/tool` (Registry, Definition, Result, Policy, ShellRegistry).
- **Used by / entrypoint:** Reached from every front-end — `main.go`/`build.go` build the `Agent`; `internal/daemon/session.go`, `internal/tui/tui.go`, `internal/chat/*` drive `Session.Send`/`Steer`/`Compact` (the GUI drives sessions through its `internal/chat` client; `internal/gui/agents.go` is the read-only tasks bridge). Subtask/dispatch are invoked by the `task`/`task_group` tools registered in `internal/tool`.

### internal/agent/background.go
- **Role:** Detached background subtasks — the `BgTask` record, the in-memory+durable `BgRegistry`, launch (`SubtaskBackground`), foreground→background promotion (`promoteRunning`), per-attempt run with context-overflow retry and difficulty escalation, and the cross-process cancel watcher.
- **Key symbols:**
  - `BgTask` — one task's durable record (status/result/error, observability fields: Pid/Host/Steps/LastTool/tokens/Canceling); `Format()` renders it for `task_status`.
  - `BgRegistry` — tracks a session's bg tasks; `NewBgRegistry` adopts stale state on start; `next`/`put`/`update`/`Get`/`List`/`History`/`reapLocked` manage records (memory map capped at `maxRetainedTasks`, terminal records pruned; disk is source of truth across processes).
  - `TasksDir` / `tasksInstanceSuffix` — resolve `~/.eigen/tasks[-<EIGEN_INSTANCE>]`.
  - `(*BgRegistry).SeedDone` — register a pre-completed task (test/external injection of a result).
  - `(*BgRegistry).StatePath` / `TranscriptPath` — durable observability file paths.
  - `(*Agent).SubtaskBackground` — launch a detached delegation: returns an id immediately, runs on a `context.Background`-rooted ctx capped at `bgMaxRuntime` (30m), clears any stale `<id>.cancel` before the task is visible as running, persists state+transcript, emits a finish note. The 30m cap is a per-TASK deadline spanning ALL attempts (each attempt derives from the shared `taskCtx`), so a retry/escalation can't make one task run ~2x the cap.
  - `(*Agent).runBackgroundAttempt` — one attempt against the shared task-level `taskCtx`: subagent build, transcript persist hook, sanitized event bridge (`bgEventSink`), heartbeat stall + cancel-marker watchers; its own derived ctx lets stall/cancel stop just this attempt while the task deadline still bounds the total. Returns `bgAttemptOutcome`.
  - `backgroundRetryContext` — derive a retry context that carries the promoted first attempt's ABSOLUTE deadline (not its cancellation), so promote-then-retry honors the same per-task cap; falls back to a fresh `bgMaxRuntime` timeout if the prior ctx has no deadline.
  - `nextBackgroundContextRetry` / `backgroundContextRetryTarget` / `compactedBackgroundTask` / `compactOversizedTaskText` / `compactOversizedText` — on context overflow, deterministically compact the transcript/prompt and retry once (no second model call).
  - `nextBackgroundEscalation` / `reportsUnderpowered` / `backgroundAttemptSummary` — on a hard error / stall / self-reported underpowered result, escalate one difficulty tier for the retry.
  - `watchCancelMarker` — poll `<id>.cancel`; on hit persist Canceling and cancel the task ctx (cross-process stop protocol).
  - `(*Agent).promoteRunning` — adopt an already-running foreground child into the registry, rewire its event sink, install cancel/stall watchers, spawn the collector goroutine.
  - `(*Agent).emitBgFinished` / `BgResult` — emit completion note + `EventBgDone` (wakes an idle orchestrator); `BgResult` returns a done task's result.
  - `bgEventSink` — sanitized bridge: maps tool/note/done events into bounded BgTask progress.
  - `writeTranscript` — atomic temp+rename rewrite of `<id>.transcript.jsonl`.
  - `sanitizeNote` / `truncateForNote` — bound notes for the durable record.
- **Depends on:** `internal/llm` (Message, CompactWith, EstimateTokens, IsContextOverflow, RoleTool).
- **Used by / entrypoint:** `BgRegistry` is built by `main`/daemon and injected as `Agent.Bg`. `SubtaskBackground` runs via the `task(background=true)` tool; `task_status.go` and the daemon/TUI/GUI tasks panels read `List`/`Get`/`History`.

### internal/agent/group.go
- **Role:** Read-only parallel multi-agent fan-out — `TaskGroup` runs several role-scoped sub-agents concurrently (bounded), validates them read-only up front, and joins their outputs into one report (optionally synthesized).
- **Key symbols:**
  - `GroupSubtask` — one child spec (Task/Role/Kind/Difficulty/Model).
  - `(*Agent).TaskGroup` — validate (role known + read-only, toolset `AllReadOnly`), bound workers, fan out via `runChild` (with promotion + one-step escalation on hard error), format the stable report, optional synthesis.
  - `(*Agent).synthesizeReports` — a tool-less sub-agent that merges N child reports into one coherent answer.
  - `formatGroupReport` — render children in stable input order with per-child status/duration/where.
  - `escalateDifficulty` — next difficulty tier up (trivial→easy→medium→hard); shared by group + background escalation.
  - `isCanceled` — context-cancel/deadline check (never retried).
  - `taskGroupRoleNames` — built-in + plugin role names for docs/errors.
  - consts: `maxGroupChildren`, `defaultGroupWorkers`, `maxGroupWorkers`, `groupChildTimeout`, `groupTotalTimeout`, `maxGroupResultBytes`.
- **Depends on:** `internal/llm` (NewCompactor), `internal/tool` (NewRegistry, Registry.Subset/AllReadOnly).
- **Used by / entrypoint:** Invoked by the `task_group` tool (registered in `internal/tool`); `escalateDifficulty`/`isCanceled` reused by background.go.

### internal/agent/groupmut.go
- **Role:** Mutating parallel fan-out — `TaskGroupMutating` runs implementer children in isolated git worktrees, captures each diff, validates the combined patch set in a throwaway worktree, and applies the clean result behind one approval (with rebase-by-redo conflict recovery).
- **Key symbols:**
  - `MutApprover` — the single apply-time approval callback (nil ⇒ deny, fail closed).
  - `(*Agent).TaskGroupMutating` — precheck the repo, spawn per-child worktrees, run implementers concurrently (PermAuto, worktree-confined tools), then `mergeAndApply`.
  - `(*Agent).implementerChild` — build a sandboxed implementer sub-agent rooted at a worktree (WorktreeTools, `implementerSystem`, routing/override honored).
  - `(*Agent).mergeAndApply` — apply patches in input order, rebase conflicts, capture the combined diff, ONE approval, re-check baseline didn't move, apply to the real tree.
  - `(*Agent).rebaseChild` — recover a conflicting child by re-running it on a snapshot of the merged state.
  - `formatMutReport` — render each implementer's apply state + diffstat + answer excerpt.
  - `PatchStat` — "N files, +A −D" summary (exported, used by the apply prompt).
  - `appliedPatch` / `mutResult` / `oneScreen` — internal apply tracking + report helpers.
- **Depends on:** `internal/llm` (NewCompactor). Calls worktree.go's git helpers.
- **Used by / entrypoint:** Invoked by the `task_group_mutating` tool; needs `Agent.WorktreeTools` + `SessionDir` injected by `main`. `PatchStat` is also used by the TUI/daemon apply-approval UI.

### internal/agent/judge.go
- **Role:** Goal-completion judging — an independent judge provider verifies a claimed achievement against the goal/criterion with a strict structured-gap contract; fail closed on any malformed/missing verdict.
- **Key symbols:**
  - `(*Agent).JudgeGoal` — judge the agent's current Goal against evidence; on a confirmed verdict clears the goal and emits a note; returns (achieved, reason).
  - `(*Agent).JudgeClaim` — same machinery for an arbitrary condition (workflow step checks), independent of Goal.
  - `judgeReport` / `judgeGap` — the parsed verdict + concrete gaps; `achieved()` requires ACHIEVED + summary + zero gaps; `format()` renders the gap report back to the model.
  - `parseJudgeReport` / `extractJudgeJSON` / `ensureEOF` / `isJSONObject` / `hasDuplicateTopLevelKey` — strict, fail-closed JSON parsing (rejects fences/extra data/dupe keys/unknown fields).
  - `judgeContractReport` / `sanitizeGaps` / `cleanJudgeText` / `nonEmpty` — normalize and bound judge output; manufacture a contract-violation gap when the judge misbehaves.
  - `judgePrompt` / `judgePromptText` — the strict judge prompt (criterion/evidence as untrusted JSON literals).
- **Depends on:** `internal/llm` (Provider.Complete, Request, Message).
- **Used by / entrypoint:** `JudgeGoal` backs the `goal_achieved` tool (wired in `build.go` + `main.go`); `JudgeClaim` backs workflow step-check logic (`main.go`). `internal/tui/goal.go` and the daemon wire the goal/judge UI.

### internal/agent/lifecycle.go
- **Role:** Subtask lifecycle primitives — idle-based stall detection, the in-flight model-call window, the foreground→background front window, and the settable event/persist `relay` used during promotion.
- **Key symbols:**
  - package vars `stallIdle` (2m), `modelMaxWait` (5m), `frontWindow` (2m), `heartbeatGrace` (30s) — the watchdog budgets.
  - `SetLifecycle` — startup config override of front-window/idle-stall (keeps model cap ≥ idle).
  - `heartbeat` — tracks last-activity + in-flight flag; `newHeartbeat`/`beat`/`modelStart`/`modelEnd`/`idle`.
  - `activitySink` — wraps an EventSink so tool/stream/note events end the in-flight window and beat.
  - `watchStall` — the watchdog goroutine: cancels when idle past the active budget (idle between actions, modelMaxWait while a call is in flight), after a grace period; returns a "did it fire" func.
  - `relay` — mutex-guarded settable indirection for a child's OnEvent/Persist; `emit`/`save`/`setEvent`/`setPersist` let promotion re-point sinks without racing the run goroutine.
- **Depends on:** `internal/llm` (Message, for the persist func signature).
- **Used by / entrypoint:** Internal to the package — `runChild`/`runBackgroundAttempt`/`promoteRunning` install heartbeats, `activitySink`, `watchStall`, and `relay`. `SetLifecycle` is called once at startup from config (`main`).

### internal/agent/roles.go
- **Role:** Named sub-agent roles — built-in read-only roles (researcher/reviewer/summarizer), the implementer system framing, and discovery/rendering of installed plugin-agent roles.
- **Key symbols:**
  - `Role` — a sub-agent specialization (System framing, Tools allowlist, Kind/Difficulty/Model defaults, ReadOnly, InheritTools).
  - `builtinRoles` — the three hardcoded read-only roles (all tools are no-approval read-only by design).
  - `LookupRole` — resolve a built-in or enabled plugin-agent role by name.
  - `implementerSystem` — the mutating fan-out child's system prompt (worktree-confined, no shell/git/network).
  - `RoleNames` / `PluginRoleNames` — list built-in / enabled-plugin role names for docs/errors.
  - `PluginRoleCatalog` — render installed plugin roles (names + routing metadata) for the system prompt, gated on whether task/task_group are available.
  - `lookupPluginAgentRole` / `pluginAgentRoles` / `pluginAgentPrompt` / `pluginAgentSystem` — load plugin-agent roles from `~/.eigen/agents` (legacy skills fallback), build their framing.
  - `singleLineRole` / `firstNonEmptyRole` — formatting helpers.
- **Depends on:** `internal/plugin` (Registry, Installed, InstalledPlugin, InstalledAgentRole, ExpandInstalledRoot, AgentsDir/SkillsDir).
- **Used by / entrypoint:** `subAgent` (agent.go) and `TaskGroup` (group.go) call `LookupRole`; `pluginRoleCatalog` (agent.go) calls `PluginRoleCatalog` to inject the catalog into the system prompt each turn.

### internal/agent/taskstore.go
- **Role:** The read side of the durable background-task disk store — robust last-line JSONL parsing, lost-task detection (dead host process), the cross-process cancel-marker request, and start-time stale adoption/retention pruning.
- **Key symbols:**
  - `LoadBgTasks` — read every `<id>.jsonl`'s current state (last complete line), interpret stale "running" as "lost", mark Canceling when a marker exists; sorted running-first.
  - `readTaskFile` / `readTaskHistory` — parse the newest valid line / all lines (robust to mid-append partial lines; whole-file-JSON fallback).
  - `ReadTaskHistory` (exported, `dir,id`) — a task's full append-only state trail straight from the disk store, no live `BgRegistry` needed; id-validated, never fatal on a missing/malformed file.
  - `ValidTaskID` — exported `bgIDRe` predicate so other packages (the GUI bridge) reject path-traversal ids before any filesystem access instead of re-deriving the pattern.
  - `taskLost` / `sameHost` / `pidAlive` — decide a "running" record is actually gone (pid signal-0 probe for this host, else age beyond `bgMaxRuntime`+`lostGrace`).
  - `RequestCancel` — drop a `<id>.cancel` marker for a running task (errors on bad id / not-running).
  - `(*BgRegistry).adoptStale` — on start: durably append a "lost" line for dead-host running records, prune terminal records past a 7-day retention window (state/transcript/marker).
  - `sortBgTasks` — running-first, then recency.
  - `bgIDRe` — task-id regex guarding path traversal from cross-process inputs.
- **Depends on:** stdlib only (os, syscall, encoding/json, regexp). Shares `BgTask`, `bgMaxRuntime`, `BgRegistry` with background.go.
- **Used by / entrypoint:** `BgRegistry.List`/`Get`/`History` call into the readers; `task_status.go`, the daemon, and TUI/GUI tasks panels call `LoadBgTasks`/`RequestCancel`; `internal/gui/agents.go` calls `TasksDir`/`LoadBgTasks`/`RequestCancel`/`ValidTaskID`/`ReadTaskHistory` to back the fan-out view + transcript reads; `adoptStale` runs from `NewBgRegistry`.

### internal/agent/worktree.go
- **Role:** Git worktree + patch plumbing for mutating fan-out — repo precheck/safety rails, serialized worktree add/remove, binary-safe patch capture, and 3-way apply/check.
- **Key symbols:**
  - `repoState` — verified baseline (repo root == session root, base HEAD SHA).
  - `precheckMutatingFanout` — refuse unless git repo, session==repo root, born HEAD, clean tree.
  - `addWorktree` / `removeWorktree` — create/force-remove detached worktrees (serialized by `repoGitMu`).
  - `capturePatch` — binary-safe `git diff --binary --full-index` against base, intent-to-add untracked files, capped at `maxPatchBytes`.
  - `applyCheck` / `applyPatch` — 3-way `git apply --check` / real apply at a dir.
  - `gitText` / `gitRaw` — run git (no shell, args passed directly), trimmed / raw stdout, 30s timeout.
  - `mkTempWorktreeParent` — 0700 temp parent dir for one fan-out's worktrees.
  - vars/consts: `repoGitMu`, `gitOpTimeout`, `maxPatchBytes`, `maxCombinedBytes`.
- **Depends on:** stdlib only (os/exec, path/filepath). No internal-package deps.
- **Used by / entrypoint:** Internal to the package — consumed entirely by groupmut.go (`TaskGroupMutating`, `mergeAndApply`, `rebaseChild`). Reached only via the `task_group_mutating` tool.

## Cross-links
- **`internal/llm`** — Provider/Streamer/Compactor/EffortSetter/FastModer interfaces, Message/Request/Response, token estimation, image shedding, tool-result dedupe, context-overflow detection. The provider is injected, never built here.
- **`internal/tool`** — Registry (Specs/CoreSpecs/GroupCatalog/Subset/AllReadOnly/HasNiche), Definition, Result, Policy (filesystem sandbox), ShellRegistry (backgrounded shells), TruncateUTF8. Tool execution and the `task`/`task_group`/`task_group_mutating`/`goal_achieved`/`search_tools` tools live there and call back into this package.
- **`internal/plugin`** — installed plugin-agent role discovery (roles.go).
- **`internal/daemon`** — primary host: builds the `Agent`, `BgRegistry`, injects Router/ModelProvider/WorktreeTools/Policy, drives `Session`, serves state snapshots; uses `SeedDone` in tests.
- **`internal/tui` / `internal/chat` / `internal/gui` / `internal/app`** — front-ends that drive sessions, render the event stream, surface goal/judge UI and the background-tasks panel.
- **`internal/hook` / `internal/observe`** — wrap the EventSink (via `Agent.EventWrap`) for hooks and observability logging.
- **`main.go` / `build.go` / `task_status.go`** — top-level wiring: construct the agent and its injected seams; `task_status` reads the durable store via `LoadBgTasks`/`RequestCancel`/`History`.
- **git CLI** — worktree.go shells out to `git` (worktree/diff/apply/status/rev-parse) for the mutating fan-out.
- **filesystem `~/.eigen/tasks[-<instance>]`** — the durable, cross-process background-task store (JSONL state + transcripts + cancel markers) is the observability seam between processes.
