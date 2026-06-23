# Tools — filesystem & editing

> The agent's hands on the local repo. This slice is the set of `tool.Definition`
> constructors in `internal/tool` that read, search, edit, and run commands
> against the working tree: read/list/glob/tree (read-only exploration),
> write/edit/multiedit/patch/move (mutating file changes), diff (git working-tree
> view), and bash + the backgrounded-shell trio (bash/bash_output/kill_shell).
> Every path-taking tool routes through `Policy.Resolve` for the path fence, and
> mutating tools rely on the approval gate (gated mode) rather than the fence to
> permit destructive actions. Each constructor returns a `tool.Definition` (see
> `tool.go`); they are wired into the registry in the repo-root `build.go` and
> `main.go`. The shared write/search helpers live in `fsutil.go`; the
> backgrounded-shell state machine lives in `shells.go` (support, not owned here).

## Files

### internal/tool/bash.go
- **Role:** The `bash` command-execution tool, with optional backgrounding (detach a long-running command into a registered shell).
- **Key symbols:**
  - `Bash(policy) Definition` — plain bash tool (no backgrounding); thin wrapper over `bashWith(policy, bashDeps{})`.
  - `BashWithShells(policy, shells, detach) Definition` — bash plus backgrounding: `background=true` arg or a runtime detach signal hands the live process to the shell registry.
  - `bashWith(policy, deps) Definition` — builds the actual Definition; description/schema gain the `background` arg only when a `ShellRegistry` is wired.
  - `bashDeps` (struct) — optional plumbing: a `*ShellRegistry` and a `detach func() <-chan struct{}` (the ctrl+b mid-run detach channel).
  - `runBash(ctx, command, dir, timeout, shells, detachCh)` — foreground run: own process group (`Setpgid`), pumps combined stdout+stderr through a pipe into a `safeBuffer`, then selects on done / ctx timeout / detach.
  - `startBackgroundShell(shells, command, dir)` — `background=true` path: spawn detached, register a `Shell`, return its handle line immediately.
  - `adoptIntoBackground(...)` — detach/ctrl+b path: convert a running foreground command into a registered background shell via `safeBuffer.redirect`.
  - `pumpShell`, `finishShell` — drain a pipe into a `Shell`; map exit/signal status onto `setStatus("exited"/"killed", code)`.
  - `truncShellCmd(s)` — one-line command preview (≤80 chars); also used by `bashoutput.go`/`shells.go`.
  - `safeBuffer` (struct) + `WriteString`/`String`/`redirect` — goroutine-safe buffer that can re-route future writes to a `Shell` (the live-handoff trick).
- **Depends on:** `ShellRegistry`/`Shell` (`shells.go`); `Policy.Dir()` (`policy.go`). Stdlib `os/exec`, `syscall`, `io`, `bufio`.
- **Used by / entrypoint:** registered as the `bash` tool in `build.go:198` and `main.go:735` (always via `BashWithShells`; the bare `Bash` constructor has no production caller). `detach` is `Agent.BashDetachCh()`.

### internal/tool/bashoutput.go
- **Role:** The `bash_output` (poll a backgrounded shell) and `kill_shell` (stop one) tools.
- **Key symbols:**
  - `BashOutput(shells) Definition` — read-only; returns output appended since the last poll (`Shell.readNew`) or the whole buffer (`full=true` → `Shell.snapshot`), plus a `[shell-N status exit=N]` header.
  - `KillShell(shells) Definition` — mutating; signals the shell's whole process group via `Shell.kill`, or reports it already finished.
- **Depends on:** `ShellRegistry.Get`, `Shell.readNew/snapshot/running/kill` (`shells.go`); `truncShellCmd` (`bash.go`).
- **Used by / entrypoint:** registered as `bash_output`/`kill_shell` in `build.go:199` and `main.go:736-737`.

### internal/tool/diff.go
- **Role:** The `diff` tool — show the git working-tree diff (optionally one path, optionally `--staged`).
- **Key symbols:**
  - `Diff(policy) Definition` — public constructor; delegates to `diff(policy, true)`.
  - `diff(policy, advertise) Definition` — builds the read-only Definition; runs `git -C <dir> diff [--staged] [-- <resolved path>]`. When `advertise` is true and the root is not a git worktree, sets `Disabled=true` so the tool is omitted from the registry.
  - `isGitRepo(ctx, dir)` — `git rev-parse --is-inside-work-tree` probe; also used at registry-disable time.
- **Depends on:** `Policy.Dir()`, `Policy.Resolve` (`policy.go`). Stdlib `os/exec`.
- **Used by / entrypoint:** registered as `diff` in `build.go:196,318` and `main.go:729`. `isGitRepo` also gates the `diff` Definition's `Disabled` flag.

### internal/tool/edit.go
- **Role:** The `edit` tool — exact string replacement in one file, with a uniqueness guard.
- **Key symbols:**
  - `Edit(policy) Definition` — requires `old_string` to be present and unique (unless `replace_all`), so an edit never silently hits the wrong location; writes via `atomicWrite`.
- **Depends on:** `Policy.Resolve` (`policy.go`); `atomicWrite` (`fsutil.go`). Stdlib `os`, `strings`.
- **Used by / entrypoint:** registered as `edit` in `build.go:197,319` and `main.go:731`.

### internal/tool/fsutil.go
- **Role:** Shared low-level helpers for the slice: atomic file write and a ripgrep runner.
- **Key symbols:**
  - `atomicWrite(path, data)` — write to a temp file in the same dir, then `Rename`, so a reader never sees a partial file; `MkdirAll` parents first.
  - `runRipgrep(ctx, args...)` — run `rg` with a 30s timeout, returning combined output + exit code; treats rg's exit 1 (no matches) as a non-error, surfaces a missing-binary error.
- **Depends on:** stdlib only (`os`, `os/exec`, `path/filepath`).
- **Used by / entrypoint:** `atomicWrite` is called by `write.go`, `edit.go`, `multiedit.go`, `patch.go`. `runRipgrep` is called by `glob.go` (and by `grep.go`, outside this slice). Library helpers, not tools themselves.

### internal/tool/glob.go
- **Role:** The `glob` tool — find files matching a glob pattern, via ripgrep `--files -g`.
- **Key symbols:**
  - `Glob(policy) Definition` — read-only; resolves the search dir through the policy, appends `DenyGlobs()` exclusions, runs ripgrep, and post-filters with `FilterDeniedLines` (defense-in-depth against listing denied paths).
- **Depends on:** `Policy.Resolve`, `DenyGlobs`, `FilterDeniedLines` (`policy.go`); `runRipgrep` (`fsutil.go`).
- **Used by / entrypoint:** registered as `glob` in `build.go:195,317` and `main.go:725`.

### internal/tool/list.go
- **Role:** The `list` tool — list one directory's entries (dirs suffixed `/`).
- **Key symbols:**
  - `List(policy) Definition` — read-only; `os.ReadDir`, skips entries that `IsDenied`, sorts, caps at `maxListEntries` (1000) with a `[truncated]` marker.
  - `maxListEntries` (const, 1000) — listing cap.
- **Depends on:** `Policy.Resolve`, `IsDenied` (`policy.go`). Stdlib `os`, `sort`.
- **Used by / entrypoint:** registered as `list` in `build.go:195,317` and `main.go:724`.

### internal/tool/move.go
- **Role:** The `move` tool — move/rename a file or directory; both endpoints fenced by the policy.
- **Key symbols:**
  - `Move(policy) Definition` — mutating; resolves `from`+`to`, verifies the source exists, `MkdirAll`s the destination parent, then `os.Rename`.
- **Depends on:** `Policy.Resolve` (`policy.go`). Stdlib `os`, `path/filepath`.
- **Used by / entrypoint:** registered as `move` in `build.go:197,320` and `main.go:734`.

### internal/tool/multiedit.go
- **Role:** The `multiedit` tool — apply an ordered sequence of string replacements to one file atomically.
- **Key symbols:**
  - `MultiEdit(policy) Definition` — each edit applies against the prior result (same uniqueness/`replace_all` rules as `edit`); the file is written once at the end via `atomicWrite`, so all edits land or none do.
- **Depends on:** `Policy.Resolve` (`policy.go`); `atomicWrite` (`fsutil.go`). Stdlib `os`, `strings`.
- **Used by / entrypoint:** registered as `multiedit` in `build.go:197,319` and `main.go:732`.

### internal/tool/patch.go
- **Role:** The `apply_patch` tool — apply a unified diff or `*** Begin Patch` agent patch across one or more files, all-or-nothing, locating hunks by context match (drift-tolerant).
- **Key symbols:**
  - `Patch(policy) Definition` — parses the patch (`parsePatch`) and applies it (`applyPatch`); supports create/delete/rename.
  - `filePatch` / `patchHunk` (structs) — parsed per-file diff and one `@@` block (anchors, expected old lines, produced new lines, old-start hint).
  - `filePatch.creating()` / `.deleting()` / `.renaming()` — classify a file section by `/dev/null` sentinels / differing paths. (`.renaming()` is unreferenced — see dead code.)
  - `parsePatch(patch)` — dispatches to `parseAgentPatch` for `*** Begin Patch`, else parses standard unified-diff `---/+++/@@/ +-` lines.
  - `parseAgentPatch(lines)` — parses the Codex/OpenAI envelope (`*** Update/Add/Delete File`, `*** Move/Rename to`), lowering it into the same `filePatch` shape.
  - `patchPath`, `parseHunkStart` — strip `a/`,`b/` + tab-timestamp from header paths; read the old start line from `@@ -l,s +l,s @@`.
  - `applyPatch(policy, files)` — computes every file's new content first, writes only if all hunks apply (atomicWrite / os.Remove), then performs rename-deletes for moved files.
  - `applyHunks(content, hunks)` — locate each hunk via `findHunk`, splice new lines in.
  - `findHunk`, `findBlockMatches`, `anchorScore`, `closerToHint`, `clampIndex` — context-match machinery: find all candidate offsets, score by anchors, break ties by proximity to the line-number hint.
  - `findBlock(lines, old, hint)` — older single-match locator; **unreferenced** (see dead code).
- **Depends on:** `Policy.Resolve` (`policy.go`); `atomicWrite` (`fsutil.go`). Stdlib `os`, `strings`, `fmt`.
- **Used by / entrypoint:** registered as `apply_patch` in `build.go:197,320` and `main.go:733`.

### internal/tool/read.go
- **Role:** The `read` tool — return a UTF-8 text file's contents, size-capped.
- **Key symbols:**
  - `Read(policy) Definition` — read-only; rejects non-UTF-8 (binary) files, truncates at `maxReadBytes` (256 KiB) via `TruncateUTF8` with a `[truncated]` marker.
  - `maxReadBytes` (const, 256 KiB) — read cap.
- **Depends on:** `Policy.Resolve`, `TruncateUTF8` (`policy.go`). Stdlib `os`, `unicode/utf8`.
- **Used by / entrypoint:** registered as `read` in `build.go:195,317` and `main.go:723`.

### internal/tool/tree.go
- **Role:** The `tree` tool — a bounded, indented directory tree (depth-limited, VCS/build dirs and hidden entries skipped).
- **Key symbols:**
  - `Tree(policy) Definition` — read-only; default depth 3, renders via `renderTree`.
  - `renderTree(root, maxDepth)` — recursive walk, caps at `maxTreeEntries` (500) with a truncation note, skips `treeSkip` dirs and dotfiles.
  - `readDirSorted(dir)` — list a dir, directories first then files, each alphabetical.
  - `treeSkip` (var), `defaultTreeDepth`/`maxTreeEntries` (consts) — skip set + bounds.
- **Depends on:** `Policy.Resolve` (`policy.go`). Stdlib `os`, `io/fs`, `path/filepath`, `sort`.
- **Used by / entrypoint:** registered as `tree` in `build.go:196,318` and `main.go:728`.

### internal/tool/write.go
- **Role:** The `write` tool — create or overwrite a file with given content (parents created as needed).
- **Key symbols:**
  - `Write(policy) Definition` — mutating; resolves the path and writes via `atomicWrite`, reporting bytes written.
- **Depends on:** `Policy.Resolve` (`policy.go`); `atomicWrite` (`fsutil.go`). Stdlib `fmt`.
- **Used by / entrypoint:** registered as `write` in `build.go:196,319` and `main.go:730`.

## Cross-links
- **`internal/tool` (tool.go — slice core):** every file returns a `tool.Definition`; `NewRegistry` validates + compacts schemas, and `Registry` exposes them to the agent. `ReadOnly` is set on read/list/glob/tree/diff/bash_output.
- **`internal/tool/policy.go`:** the path fence + deny machinery. `Policy.Resolve`/`Dir`, `IsDenied`, `DenyGlobs`, `FilterDeniedLines`, `TruncateUTF8` are consumed throughout this slice.
- **`internal/tool/shells.go`:** the backgrounded-shell registry/state machine (`ShellRegistry`, `Shell`, `ShellInfo`) that `bash.go`/`bashoutput.go` drive; the support layer for this slice (not owned here).
- **Repo-root `build.go` / `main.go`:** the registration call sites — wire all these constructors (plus `grep`, `symbols`, `todo` from sibling files) into the tool registry, passing a per-session `Policy` and a `ShellRegistry`.
- **`internal/agent` (agent.go, background.go):** supplies `BashDetachCh()` (the ctrl+b detach channel) and surfaces `Shells.StatusBlock()` into the system prompt.
- **`internal/chat`, `internal/daemon`, `internal/gui`, `internal/tui`:** consume `ShellRegistry.Infos()`/`KillByID`/`RunningCount` to render and control background shells in the panel / over the daemon protocol.
- **External binaries:** `bash` (bash tool), `git` (diff tool), `rg`/ripgrep (glob tool, via `runRipgrep`).
