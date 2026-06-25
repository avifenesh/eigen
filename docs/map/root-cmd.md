# Root commands & entrypoints

> The repo-root `package main`: the `eigen` binary's entrypoint, CLI argument
> routing, and per-session wiring. `main.go` parses flags and dispatches every
> subcommand (`daemon`, `attach`, `gui`, `remote`, `skill`, `plugin`,
> `marketplace`, `dream`, `memory`, `observe`, `telegram`, `plan`, `run`,
> `orientation`, `harness`/`computer-use`/`workspace`/`chrome`, `models`, `dev`,
> `theme`, `version`), and decides whether to open the paged **app** shell,
> attach a TUI view to a daemon session, run headless print mode, or drive an
> in-process agent. The other files specialize a concern: `daemon.go` is the
> long-lived session host + its control surface + nightly dreaming + the Telegram
> supervisor; `build.go` is the reusable per-session agent constructor
> (`buildSession`) the daemon uses; `router.go` is the opt-in cross-vendor model
> router + review/council runners; `remote.go`/`remote_session.go` bootstrap and
> attach to eigen over SSH; `gui.go`/`main_gui_wails.go` launch the Wails v3
> desktop GUI; `plugincmd.go` is the plugin/marketplace CLI; `agent_sessions.go`
> enumerates recent transcripts across every agent (eigen/Claude/Codex/OpenCode)
> for dreaming + the feed; `retrieve_run.go` and `imagegen_run.go` build two
> injected tool runners; `agents.go` loads AGENTS.md guidance; `task_status.go`
> formats background-task status + promotes task transcripts; the `smoke_hooks*`
> trio gates test-only PTY smoke entrypoints behind the `smoke` build tag.

## Files

### main.go

- **Role:** The CLI entrypoint: flag parsing, subcommand dispatch, and the three
  run modes (app shell / daemon-backed TUI / in-process agent / headless print).
- **Key symbols:**
  - `main()` — loads `~/.eigen/.env` + config, may re-exec to honor
    `config.theme`/`nerd_font` (and exports `tts_cmd`/`daemon_timeout`/`effort`/
    `skills_dirs` to env), resolves the daemon instance, parses flags, prints
    `llm.FullVersion()` for `--version`/`version`, dispatches every subcommand,
    then runs the app shell, attaches a TUI to a daemon session, or builds an
    in-process agent (with all tools/MCP/LSP/plugins) for interactive or headless
    use. The DEFAULT interactive path is a daemon session via `ensureDaemon` +
    `chat.NewRemote`; `EIGEN_NO_DAEMON=1` keeps the in-process agent.
  - `cliApprove(ctx, name, args)` — gated-tool approval prompt read from
    `/dev/tty` (fails closed with no terminal); wired as `a.Approve` in headless.
  - `contextBudget` / `resolveUserMaxTokens` / `providerContextDefault` —
    compute the pre-compaction token budget = min(user ceiling, model window).
  - `smallProvider` / `smallProviderFor` / `localBackgroundEnabled` /
    `localReady` / `titleProvider` — pick the small/fast model for background
    chores (titling, dreaming, compaction); optional local llama with a
    `/health` readiness probe.
  - `runObserveCmd`, `runDream`, `runMemoryCmd`, `runModelsCmd`, `runSkillCmd`,
    `runHarnessCmd`, `runComputerUseCmd`, `runWorkspaceCmd`, `runChromeCmd`,
    `runOrientationCmd`, `runTelegram`, `runPlan`, `runWorkflowHeadless` — the
    subcommand handlers dispatched from `main`. `runDream` also auto-maintains the
    GLOBAL profile: distill cross-project notes, refresh `memory_summary.md`, and
    push the learned block into USER.md via `gmem.SetLearnedProfile` (user's own
    additions below the marker preserved), then `memory.CommitMemory`.
  - `newMemoryPipeline` / `refreshMemorySummary` / `memoryScopeKey` — wire the
    `dream` package's model steps into a `memory.Pipeline` (avoids a
    memory→dream import cycle); shared with `daemon.go`'s nightly dreamer.
  - `dreamSessionID` / `dreamWatermark` — derive a stable per-session id +
    mtime/size watermark from a cross-agent ref (skip-unchanged for dreaming).
  - `importResume` / `latestEigenSession` / `newSessionPath` — resume-by-id/
    keyword/path and session-file bookkeeping (the cross-agent scan that backs
    `--list`/`dream` lives in `agent_sessions.go`).
  - `mergeTaskStdin` — fold piped stdin into a positional task so neither is
    dropped (both sources imply headless print mode).
  - `execResume` — `syscall.Exec` onto a freshly built binary for `/rebuild` of
    an in-process session.
  - `installSkill`, `splitSource`, `acquireTelegramLock`, `signalContext`,
    `workflowStepRunner`, `multiFlag`, `parseVars`, `firstNonEmpty`,
    `splitNonEmpty`, `expandTilde`, `wdOrDot`, `effectiveModel`, `fail`,
    `printSessions`/`printSkills`/`printTools`, `*ConfigPath` helpers,
    `notifyCmdline` — utility + path/print helpers.
- **Depends on:** `internal/agent`, `internal/app`, `internal/chat`,
  `internal/config`, `internal/daemon`, `internal/dream`, `internal/harness`,
  `internal/hook`, `internal/llm`, `internal/lsp`, `internal/mcp`,
  `internal/memory`, `internal/observe`, `internal/session`, `internal/skill`,
  `internal/telegram`, `internal/theme`, `internal/tool`, `internal/transcript`,
  `internal/tui`, `internal/workflow`, `github.com/mattn/go-isatty`.
- **Used by / entrypoint:** entrypoint: the `eigen` binary's `main()`. Calls
  into every other file in this slice.

### daemon.go

- **Role:** The long-lived session host (`eigen daemon`), its control surface
  (`status`/`stop`/`stats`/`prune`/`install`/`uninstall`/`stdio`), the
  attach/navigation view loop, daemon auto-spawn, nightly dreaming, and the
  Telegram supervisor.
- **Key symbols:**
  - `runDaemon(cfg)` — starts the persistent host: loads `daemon.env`, sweeps
    stale agent-workspace sandboxes, builds the per-session `build` closure (via
    `buildSession`), sets the bg-task counter (`host.SetBgCount`, feeds `stats` +
    the GUI agents badge), model-switcher, titler, and notifier, restores
    persisted sessions, starts the nightly dreamer + optional Telegram supervisor,
    listens on the Unix socket, handles graceful shutdown.
  - `daemonControl(sub)` — handles the `eigen daemon <sub>` control subcommands;
    returns true when handled.
  - `daemonStdio()` — relays this process's stdin/stdout to the local daemon
    socket (the SSH transport primitive for remote views).
  - `runAttach(id, cfg)` / `attachTUI(c, id, cfg, task)` / `continueNav` /
    `appNav` / `sessionDir` / `mustList` — the one-window view loop: attach the
    rich chat TUI to a daemon session, hop sessions (alt+s), open the app shell
    (h home), and translate the app's choice into the next chat leg.
  - `ensureDaemon()` — returns a client, spawning a detached `eigen daemon` if
    none is running (waits up to 10s for the socket).
  - `daemonRebuildResume(bin, id)` — `/rebuild` for daemon sessions: stop the old
    daemon, exec `bin attach <id>` (auto-starts the new binary's daemon).
  - `loadDaemonEnv`, `daemonInstall`, `daemonUninstall`, `daemonInstanceSuffix`,
    `credentialEnvKeys` (= `remote.CredentialKeys`) — systemd unit + credential
    snapshot lifecycle.
  - `runDevCmd` / `devFindGo` — `eigen dev`: build the source tree and re-exec on
    the isolated `dev` instance.
  - `sweepStaleWorkspaces`, `nightlyDreamer`, `runNightlyDream`,
    `telegramConfigured`, `telegramSupervisor`, `minDur`, `exitCode` — daemon
    background chores + supervision.
  - `humanBytes` / `humanCount` / `shortHash` — `daemon stats` formatting.
  - `isClosedErr` — treat a closed-listener error as clean shutdown.
- **Depends on:** `internal/agent`, `internal/app`, `internal/chat`,
  `internal/config`, `internal/daemon`, `internal/dream`, `internal/hook`,
  `internal/llm`, `internal/mcp`, `internal/memory`, `internal/remote`,
  `internal/session`, `internal/skill`, `internal/transcript`, `internal/tui`.
- **Used by / entrypoint:** `main.go` (`runDaemon`, `daemonControl`,
  `ensureDaemon`, `runAttach`, `runDevCmd`, `continueNav`); `daemonRebuildResume`
  called from `main.go` + `daemon.go`'s own `attachTUI`. `main_gui_wails.go`
  passes `ensureDaemon` into the GUI bridge.

### build.go

- **Role:** The reusable per-session agent constructor: `buildSession` mirrors
  main's inline wiring so the daemon can host many concurrent sessions, each
  rooted at its own dir.
- **Key symbols:**
  - `buildSession(p buildParams) (*sessionDeps, error)` — constructs a complete
    `*agent.Agent` rooted at `p.Dir`: tools, per-dir memory, MCP/LSP/plugins, the
    router, cross-vendor review/judge/council closures, observability + hooks,
    and the small-model compactor.
  - `sessionDeps` (struct) + `(*sessionDeps).Close()` + `(*sessionDeps).registryRef()`
    — the agent (`Agent`/`Provider`/`Router`/`Mem`/`GlobalMem`) plus resources the
    caller keeps alive (`mcpClients`, `lspMgr`, `obsLog`) and the
    observability+hooks composition (`eventWrap` is wired onto `a.EventWrap`;
    `hooks` is the lifecycle runner); `Close` tears the external resources down;
    `registryRef` lazily resolves the tool registry for `search_tools`.
  - `buildParams` (struct) — dir/provider/model/perm/maxTokens/goal/cfg/skills/
    global-mem inputs.
  - `worktreeTools(dir)` — the read/write/edit-only implementer toolset (no
    bash/git/network) for a mutating fan-out child, rooted at its git worktree.
- **Depends on:** `internal/agent`, `internal/config`, `internal/hook`,
  `internal/llm`, `internal/lsp`, `internal/mcp`, `internal/memory`,
  `internal/observe`, `internal/skill`, `internal/tool`.
- **Used by / entrypoint:** `daemon.go`'s `runDaemon` build closure calls
  `buildSession`; `worktreeTools` is passed as `Agent.WorktreeTools` in both
  `main.go` and `build.go`.

### router.go

- **Role:** The opt-in per-task model router (`autoRouter`) plus the cross-vendor
  reviewer and adversarial planning-council runners.
- **Key symbols:**
  - `autoRouter` (struct) + `newAutoRouter` — caches constructed providers;
    holds the cross-provider allowlist + the user's base provider.
  - `(*autoRouter).Route(ctx, prompt, kind, difficulty, hasImage)` —
    orchestrator-driven model selection for a delegated subtask (explicit
    kind/difficulty or a vision need always routes; otherwise a small model
    assesses only when `/route` is enabled).
  - `(*autoRouter).providerFor(model)` — build + cache a provider for a model id.
  - `(*autoRouter).crossReviewer(authorModel)` — returns the review closure (GPT
    reviews Claude, Claude reviews GPT — never self-review).
  - `(*autoRouter).councilRunner(authorModel)` — returns the adversarial planning
    closure (author = active model, adversary = other vendor; honors
    `EIGEN_PLAN_ADVERSARY`).
  - `(*autoRouter).SetEnabled` / `Enabled` / `Providers` — live toggle + allowlist
    accessors (used by the TUI's `/route`).
  - `assessRoute`, `routeCandidates`, `routeAssessmentPrompt`,
    `parseRouteAssessment`, `routeAssessment`, `routeAssessor`, `kindName`,
    `diffName` — the small-model routing-assessment internals.
- **Depends on:** `internal/llm`.
- **Used by / entrypoint:** `main.go` and `build.go` construct an `autoRouter`
  and wire `Route`/`providerFor` into the agent, and `crossReviewer`/
  `councilRunner` into the review/plan tools; `runPlan` (main.go) uses
  `councilRunner` directly.

### remote.go

- **Role:** The `eigen remote <install|list|add|remove>` CLI: bootstrap eigen
  onto an SSH host and manage saved hosts (`~/.eigen/hosts.json`).
- **Key symbols:**
  - `runRemoteCmd(args)` — dispatch the `remote` subcommands.
  - `remoteInstall(spec, pushCreds)` — detect remote OS/arch, obtain a matching
    binary (copy-running or cross-compile), scp + verify, optionally push creds.
  - `remoteBinaryFor(target)` — plan + produce the binary for the target arch.
  - `crossCompile(srcDir, t)` — `GOOS/GOARCH` cross-build into a temp file.
  - `remoteList` / `remoteAdd` / `remoteRemove` — saved-host CRUD.
  - `credentialSnapshot` (= `remote.CredentialSnapshot()`), `eigenSourceDir` —
    creds + source-tree resolution.
- **Depends on:** `internal/remote`.
- **Used by / entrypoint:** `main.go` (`runRemoteCmd` for `eigen remote`).

### remote_session.go

- **Role:** Attaching a local TUI view to a session on a REMOTE eigen daemon over
  SSH (or via a forwarded socket), with the agent loop running remotely.
- **Key symbols:**
  - `runRemote(spec, cfg)` — `eigen --remote user@host[:dir]` thin wrapper over
    `runRemoteSession`.
  - `runRemoteSession(spec, sessionID, cfg)` — the single remote entry: resolve
    the host, dial over SSH, pick/create a session, run the view loop. Called by
    `main.go` and `daemon.go`'s `appNav` (the app's Machines drill-in).
  - `remoteAttachLoop(c, h, id, cfg)` — the remote view loop (alt+s hops; no local
    chdir/rebuild).
  - `runAttachSock(sock, id, cfg)` — attach to a daemon at an explicit (forwarded)
    socket path; never auto-spawns a daemon.
  - `extractSockFlag(args)` — scan `attach` sub-args for `--sock` in any position.
- **Depends on:** `internal/chat`, `internal/config`, `internal/daemon`,
  `internal/hook`, `internal/memory`, `internal/remote`, `internal/session`,
  `internal/skill`, `internal/tui`.
- **Used by / entrypoint:** `main.go` (`runRemote`, `extractSockFlag`,
  `runAttachSock`, `runRemoteSession`); `daemon.go` (`runRemoteSession`).

### gui.go

- **Role:** Launch the Eigen desktop GUI (`eigen gui`); `app.Run` blocks until
  the window closes, with bridge shutdown on both exit paths.
- **Key symbols:**
  - `runGUICmd(_ []string)` — build the Wails app + bridge, run, then
    `bridge.Shutdown()` (orphan-free teardown).
- **Depends on:** (stdlib only; calls `buildGUIApp` in `main_gui_wails.go`).
- **Used by / entrypoint:** `main.go` (`runGUICmd` for `eigen gui`).

### main_gui_wails.go

- **Role:** Construct the Wails v3 application + the `gui.Bridge`, embedding the
  built Svelte frontend; supply the feed suggester + project-dir universe.
- **Key symbols:**
  - `buildGUIApp() (*application.App, *gui.Bridge)` — embeds
    `internal/gui/frontend/dist`, wires `ensureDaemon` + suggester + project dirs
    into `gui.NewBridge`, opens the main webview window.
  - `guiSuggester()` / `guiSuggestProvider()` — adapt a suggestion model
    (`EIGEN_SUGGEST_MODEL`, else glm-5.2 (web_search "auto"), else the small
    model) into a `feed.Suggester`.
  - `guiProjectDirs()` — distinct working dirs across saved sessions (the feed's
    scan universe).
  - `guiAssets` (`//go:embed`) — the built Svelte frontend.
- **Depends on:** `internal/feed`, `internal/gui`, `internal/llm`,
  `internal/session`, `github.com/wailsapp/wails/v3/pkg/application`.
- **Used by / entrypoint:** `gui.go`'s `runGUICmd` calls `buildGUIApp`.

### plugincmd.go

- **Role:** The user-only plugin/marketplace CLI (Tier 27): the agent cannot
  install plugins; bundles are vulnerability-scanned before install.
- **Key symbols:**
  - `runMarketplaceCmd(args)` — `eigen marketplace <add|list|remove|enable|
    disable|update>`.
  - `runPluginCmd(args, provider, model)` — `eigen plugin <install|list|remove|
    enable|disable>`; the small model scans on install unless `--no-scan`.
  - `printInstallResult(res, scanned)` — render the install outcome + scan flags.
- **Depends on:** `internal/llm`, `internal/plugin`, `internal/skill`.
- **Used by / entrypoint:** `main.go` (`runMarketplaceCmd`, `runPluginCmd`).

### agent_sessions.go

- **Role:** Enumerate recent transcripts across EVERY agent (eigen + Claude +
  Codex + OpenCode), newest-first, so dreaming and the feed reflect over all of
  the user's recent work, not just eigen's own last session.
- **Key symbols:**
  - `sessionRef` (struct) — one discovered transcript: `Path` (file path, or the
    session id for OpenCode), `Source` (which agent wrote it), `ModTime`.
  - `agentSessionGlobs` — per-source `$HOME`-relative transcript globs (mirrors
    `internal/session.sourceGlobs`); OpenCode's SQLite DB is handled separately.
  - `recentAgentSessions(n)` — up to n refs across all sources, newest-first;
    missing agent dirs/unreadable files are skipped, never fatal. The wide-span
    counterpart used by `--list`'s store fallback, `runDream`, and `runModelsCmd`.
  - `firstGlobSegment(glob)` — the fixed (non-wildcard) leading path of a glob,
    so a missing agent root is `os.Stat`-skipped before globbing.
- **Depends on:** `internal/transcript`.
- **Used by / entrypoint:** `main.go` (`printSessions` fallback, `runDream`,
  `dreamSessionID`/`dreamWatermark`).

### retrieve_run.go

- **Role:** Build the injected `retrieve` tool runner (per-project lexical (BM25)
  index, fused with embeddings when an embedder is explicitly configured; opened
  lazily, synced incrementally).
- **Key symbols:**
  - `retrieveRunner(dir) tool.RetrieveRun` — mutex-guarded closure: lazily open a
    BM25/embedder-fused index rooted at `dir`, sync, search top-k.
  - `configuredEmbedder()` — return the embedder ONLY when `EIGEN_EMBED_BASE_URL`
    is explicitly set (else nil → lexical-only), so retrieval never hammers a
    usually-dead default localhost.
  - `formatRetrieval(query, res)` — render hits as `path:lines (score)` + snippet.
- **Depends on:** `internal/llm`, `internal/retrieve`, `internal/tool`.
- **Used by / entrypoint:** `main.go` and `build.go` pass `retrieveRunner(dir)`
  into `tool.Retrieve(...)`.

### imagegen_run.go

- **Role:** Build the injected `generate_image` tool runner (render via the
  configured image model, save PNGs under `<dir>/eigen-images/`).
- **Key symbols:**
  - `imageGenRunner(dir) tool.ImageGenRun` — closure: generate images, save to
    the project, return paths + inline images for the tool-result plumbing.
- **Depends on:** `internal/llm`, `internal/tool`.
- **Used by / entrypoint:** `main.go` and `build.go` pass `imageGenRunner(dir)`
  into `tool.GenerateImage(...)`.

### agents.go

- **Role:** Load a repo's agent-instructions file (AGENTS.md / .eigen/AGENTS.md /
  CLAUDE.md) for the system prompt's `ExtraSystem`.
- **Key symbols:**
  - `agentsGuidance(cwd)` — walk up from cwd to the repo root, collect the nearest
    instruction files (each capped at 12 KiB), render them nearest-first.
  - `isRepoRoot(dir)` — stop the upward walk at a `.git` entry.
- **Depends on:** (stdlib only).
- **Used by / entrypoint:** `main.go` (`agentsGuidance` for in-process
  `ExtraSystem`) and `build.go` (per-session `ExtraSystem`).

### task_status.go

- **Role:** The textual surface behind the `task_status` tool + promoting a
  background task's transcript into a resumable session.
- **Key symbols:**
  - `formatTaskStatus(bg, id, all, verbose, tail)` — render one task or the list,
    with optional verbose detail + transcript tail.
  - `promoteTaskTranscript(bg, id)` — copy a background task's transcript into
    `~/.eigen/sessions` as a resumable eigen session (+ meta).
  - `formatTaskStatusVerbose`, `summarizeAttempts`, `formatTranscriptTail`,
    `readTranscriptTail`, `formatTranscriptMessage`, `uniquePromotedSessionPath`,
    `pathExistLabel`, `oneLine`/`oneLineLimit`, `attemptSpan`, `maxTranscriptTail`
    — formatting + transcript-reading internals.
- **Depends on:** `internal/agent`, `internal/llm`, `internal/transcript`.
- **Used by / entrypoint:** `main.go` and `build.go` wire `formatTaskStatus` +
  `promoteTaskTranscript` into the `task_status` / `task_promote` tools.

### smoke_hooks.go

- **Role:** Dispatch to the build-tag-selected smoke-command implementation.
- **Key symbols:**
  - `runSmokeCommand(arg)` (build tag `smoke`) — delegates to
    `runTestSmokeCommand`.
- **Depends on:** (none).
- **Used by / entrypoint:** `main.go` (`runSmokeCommand`). Compiled only under
  `-tags smoke`.

### smoke_hooks_prod.go

- **Role:** Production (`!smoke`) build: refuse the hidden `app-smoke`/`tui-smoke`
  hooks explicitly instead of falling through to a real agent task.
- **Key symbols:**
  - `runSmokeCommand(arg)` (build tag `!smoke`) — returns false for normal args;
    exits 2 for `app-smoke`/`tui-smoke`.
  - `runTestSmokeCommand(string) bool` — no-op stub (returns false).
- **Depends on:** (stdlib only).
- **Used by / entrypoint:** `main.go` (`runSmokeCommand`). Compiled into every
  release binary (default, no `smoke` tag).

### smoke_hooks_smoke.go

- **Role:** Test-only (`smoke`) build: drive the real `app`/`tui` Program paths
  from subprocess-based smoke tests via env-gated entrypoints.
- **Key symbols:**
  - `runTestSmokeCommand(arg)` (build tag `smoke`) — runs `app-smoke`
    (`EIGEN_APP_SMOKE=1`) or `tui-smoke` (`EIGEN_TUI_SMOKE=1`) against the real
    app/tui.
  - `smokeProvider` — a stub `llm.Provider` returning a canned answer.
- **Depends on:** `internal/agent`, `internal/app`, `internal/chat`,
  `internal/llm`, `internal/tool`, `internal/tui`.
- **Used by / entrypoint:** `smoke_hooks.go`'s `runSmokeCommand` →
  `runTestSmokeCommand`. Compiled only under `-tags smoke` (subprocess smoke
  tests).

## Cross-links

- **internal/daemon** — the persistent session host, client, instances,
  sockets/PID; the core of `daemon.go` + every attach path.
- **internal/agent** — the agent loop, subtasks, background registry, roles,
  permissions; constructed in `main.go` + `build.go`.
- **internal/tool** — the tool registry + definitions; assembled in `main.go` +
  `build.go`; `retrieve_run.go`/`imagegen_run.go`/`task_status.go` build injected
  runners.
- **internal/llm** — providers, the model catalog, routing primitives, council +
  review, compactors, embedders, image generation; used pervasively.
- **internal/tui** — the rich chat TUI launched by every interactive/attach path.
- **internal/app** — the paged app shell (home/projects/sessions/config) opened
  by bare `eigen`, `eigen app`, and the `h` home navigation.
- **internal/chat** — the `chat.NewLocal`/`chat.NewRemote` backend seam between
  the TUI and either an in-process agent or a daemon session.
- **internal/gui** + **internal/feed** + **wails/v3** — the desktop GUI bridge,
  proactive feed/suggester, and the Wails application (`gui.go`,
  `main_gui_wails.go`).
- **internal/remote** — SSH host specs, bootstrap planning, credential keys;
  `remote.go`/`remote_session.go`/`daemon.go`.
- **internal/memory** + **internal/dream** — project/global memory stores +
  the reflection/consolidation pipeline (`eigen dream`, nightly dreamer).
- **internal/skill** + **internal/plugin** — skills discovery/install +
  plugin/marketplace registry (`runSkillCmd`, `plugincmd.go`).
- **internal/config** — `~/.eigen/config.json` defaults consumed in `main`.
- **internal/harness** + **internal/mcp** — bundled helpers (orientation, Chrome
  bridge, computer-use, workspace) + MCP/LSP tool loading.
- **internal/hook** + **internal/observe** — lifecycle hooks + metadata-only
  observability log wrapped around the event sink.
- **internal/session** + **internal/transcript** — the session store +
  transcript load/save/import and session meta.
- **internal/telegram** + **internal/workflow** + **internal/lsp** + **internal/theme**
  — Telegram phone bridge, authored multi-step workflows, language-server tools,
  themed swatch/output.

## Dead-code notes

Investigated and **NOT dead** (verified callers): all `run*Cmd` handlers
(Cobra-less manual dispatch from `main`), `worktreeTools`, `continueNav`,
`runRemoteSession`, `runAttachSock`/`extractSockFlag`, `recentAgentSessions`
(`--list` fallback + `dream`), `humanCount`/`humanBytes`/`shortHash` (daemon
stats), `credentialEnvKeys`, `attemptSpan`, `(*sessionDeps).registryRef`/`Close`,
the Wails-bound bridge wiring, and the build-tagged
`runTestSmokeCommand`/`smokeProvider` (smoke tests). The only genuine
(low-confidence) suspects: the `sessionDeps` fields `Provider`/`Router`/`Mem`/
`GlobalMem`/`hooks` are written by `buildSession` (the daemon's build closure
reads only `Agent`+`Close`), and `eventWrap` is consumed locally (assigned to
`a.EventWrap`) — kept as the documented per-session resource set.
