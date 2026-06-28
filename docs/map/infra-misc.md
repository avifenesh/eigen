# theme/, harness/, config/, workflow/, command/, hook/, fuzzy/

> Cross-cutting infrastructure packages that the rest of eigen leans on but which
> own no agent loop of their own. `theme` is the single source of truth for color,
> styling, and the icon vocabulary shared by the chat TUI and the app shell.
> `config` is the optional `~/.eigen/config.json` settings model plus `.env`
> loading. `command`, `workflow`, and `hook` are three authored-prose / lifecycle
> extension seams: custom slash commands, replayable multi-step workflows, and
> fire-and-forget lifecycle hooks. `harness` installs the bundled Rust/JS helper
> servers (computer-use, workspace, chrome-bridge) and the native orientation CLI
> from embedded sources. `fuzzy` is a one-function ranking helper shared by every
> "type to find" surface. Almost all of these are reached from the repo-root
> `main.go` / `build.go` / `daemon.go` CLI wiring, with the TUI and app shell as
> the other big consumers.

## Files

### internal/theme/theme.go
- **Role:** The palette/role system — the data a theme IS, plus the package-level role vars and ready-made styles every UI call site references.
- **Key symbols:**
  - `Palette` (struct) — full set of role colors (Base/Surface/Overlay elevation, Text/Dim/Faint/Ghost text tiers, Accent/Title/Ok/Warn/Err/Tool/…, diff backgrounds, 7-color syntax palette, loader ramp stops, and the `Spectrum` brand sweep).
  - `deepTealPalette`, `nordPalette`, `gruvboxPalette` (vars) — the three registered themes (deepteal is the default).
  - `palettes` (map), `PaletteNames() []string` — the theme registry and its name list (used by `config` for validation/option lists).
  - `Active` (var) + `selectPalette(name)` — the palette chosen at init from `EIGEN_THEME` (defaults to deepteal); read-only after init.
  - Role vars (`Text`, `Dim`, `Accent`, `Working`, `Focus`, `Sel`, `SynKeyword`, … `Spectrum`) — assigned from `Active`; call sites reference these, never raw colors.
  - `BreathRamp`, `WorkingRamp` (vars) — brightness cycles for the breathing/pulsing loaders.
  - `S*` style vars (`SText`, `SDim`, `SAccent`, `SSurface`, `SWorking`, …) — pre-built `lipgloss.Style`s per role.
- **Depends on:** `github.com/charmbracelet/lipgloss` (stdlib `os`/`strings`). No internal deps.
- **Used by / entrypoint:** imported broadly across `internal/tui/*` (rail, view, brand, art, blocks, diffview, codetint, …), `internal/app/*` (style, app, surface), `internal/config`, and `main.go`.

### internal/theme/icons.go
- **Role:** The single icon vocabulary — Nerd Font glyphs vs a pure-Unicode fallback, chosen once at init, plus the tool→glyph map and structural/status glyphs.
- **Key symbols:**
  - `nerdFont` (var) + `detectNerdFont()` — pick the tier from `EIGEN_NERD_FONT` (or a font-name hint), defaulting to the safe Unicode fallback.
  - `NerdFontMode() string` — active tier as "on"/"off" (used by `main.go` to decide whether the startup re-exec must fire).
  - `icon(nf, fallback)` — internal picker returning the right glyph for the active tier.
  - `IconRead`/`IconWrite`/`IconEdit`/`IconSearch`/`IconBash`/… (vars) — one glyph per tool family.
  - `ToolIcon(name) string` — maps a tool name (incl. aliases like multiedit/apply_patch, grep/glob/find) to its icon; used by the transcript.
  - `Caret`, `Expanded`/`Collapsed`, `StatusWorking`/`StatusIdle`/`StatusApproval`/`StatusError`, `Ellipsis`, `CollapseAll`/`ExpandAll`, `Back` — the rest of the width-safe glyph vocabulary (status dots are width-1 `◉`/`◌`/`◊`/`✗` chosen to avoid East-Asian-ambiguous double-width).
- **Depends on:** stdlib only (`os`, `strings`).
- **Used by / entrypoint:** `internal/tui` (art, view, blocks, sidebar, shellspanel) and `internal/theme/swatch.go`; `NerdFontMode` from `main.go`.

### internal/theme/swatch.go
- **Role:** Renders the whole design system (roles, elevation, icons, ramps, weights, glyphs) as one styled block for `eigen theme`.
- **Key symbols:**
  - `Swatch() string` — builds the "living swatch" string (roles, elevation, icons, ramps, weights, glyphs; pure lipgloss styling, no terminal control).
  - `rampChips(ramp)` — renders an animation ramp as a row of background chips.
  - `roleColor(name)` — explicit name→color switch for the swatch chips (compile-checked, no reflection).
- **Depends on:** `github.com/charmbracelet/lipgloss`; the role vars/styles in `theme.go` and glyphs in `icons.go`.
- **Used by / entrypoint:** entrypoint: `main.go` prints `theme.Swatch()` for the `eigen theme` command.

### internal/fuzzy/fuzzy.go
- **Role:** Shared fuzzy ranking so every "type to find" surface ranks identically.
- **Key symbols:**
  - `Score(s, q) int` — substring matches beat subsequence matches, earlier starts win; `-1` = no match, `0` = best.
- **Depends on:** stdlib only (`strings`).
- **Used by / entrypoint:** `internal/tui/nav.go`, `internal/tui/palette.go`, `internal/skill/skill.go`, `internal/app/filter.go`.

### internal/config/config.go
- **Role:** The `~/.eigen/config.json` settings model: the `Config` struct plus load/save and the get/set/view machinery used by `/config` and the config panel.
- **Key symbols:**
  - `Config` (struct) — every optional setting: provider/model (one user-facing model ref; provider is shadow metadata), perm, input_mode (steer|queue), effort, theme, nerd_font, max_tokens, tts_cmd, notify_cmd, judge_model (pins the goal/claim judge — read into Agent.JudgeModel, beats the cheap-judge type ladder), dream_model (pins background dreaming — empty = sonnet-first dream ladder, off the cheap GLM quota), dream_batch (route nightly dream Stage1 through the provider async batch API, ~50% off — Anthropic only, default off), telegram_token/telegram_allow, skills_dirs, route/route_providers/route_model, rule_chains (`map[string][]string` — per-ROLE model fallback chains; see below), observe (`*bool`, default-on), dream_on_idle/idle_minutes, front_window_min/stall_idle_min, local_background, daemon_timeout.
  - **Per-role fallback chains:** `RuleChains map[string][]string` (json `rule_chains`) holds an ordered model list per role (the GUI's per-rule chain editor + CLI key `rule_chains.<role>`). `ChainFor(role)` resolves: env `EIGEN_CHAIN_<ROLE>` → `RuleChains[role]` → `RuleChains["default"]` → `DefaultRoleChains[role]` (the capability-aware built-ins mirroring the SubagentModel ladders — explore=fast/cheap, research=strong, code=strong+fast, judge=cheap-but-valid, dreamer=sonnet-first OFF the GLM quota) → `DefaultRuleChain` (the global opus-first chain: `opus,gpt-5.5,glm,sonnet,gpt-5.4,opus-4.7,glm-5.1,composer,glm-5,grok`). `RuleRoles` lists the editable roles. `SetRuleChain(c, role, chain)` sets/clears one role (empty chain → revert to default + drop from map). `Set`/`Get` handle the dotted `rule_chains.<role>` key; `View` renders only customized roles. Fed to `llm.NewChain` at every model-selection site.
  - `Load()` / `LoadFrom(path)` — read the JSON (missing/malformed → zero Config, never an error); splits a tagged model ref into provider+model via `llm.ParseRef`.
  - `Path()`, `Save(c)`, `SaveTo(path, c)` — canonical path and persistence.
  - `Set(c, key, value)` / `Get(c, key)` — typed key writes/reads matching the JSON field names; `Set` validates closed-set keys (theme via `theme.PaletteNames()`, effort via `FieldFor("effort").Options`, perm/input_mode/nerd_font/the bool flags inline); `Get` masks `telegram_token` (returns "set") and renders model as a `llm.Ref`.
  - `Keys()` — settable keys derived from `Fields()` (one source of truth).
  - `View(c)` — aligned "key = value" render for the `/config` view.
  - `(Config) ObserveEnabled()` — activity log default-on helper (true when `Observe` is nil).
  - `knownTheme(name)`, `splitFields(s)` — internal validation/parse helpers.
- **Depends on:** `internal/llm` (`ParseRef`/`Ref`/`Lookup`), `internal/theme` (`PaletteNames`).
- **Used by / entrypoint:** `main.go`, `daemon.go`, `build.go`, `remote_session.go`, `internal/tui` (configpanel, commands), `internal/gui/config.go`, `internal/app` (data, inspector, pages).

### internal/config/dotenv.go
- **Role:** `.env` credential loading into the process environment (package doc comment lives here).
- **Key symbols:**
  - `LoadEnvFiles(paths...)` — loads KEY=VALUE pairs without overriding already-set vars; earlier files win, real env wins over all; missing file is not an error; honors `export ` prefix and quoted values.
  - `loadOne(path)` — single-file scanner (1 MB line buffer).
  - `stripInlineComment(val)` — drops a trailing unquoted `" #..."` comment from a value, preserving non-whitespace `#` (e.g. `color=#fff`) and backslash-escaped hashes.
- **Depends on:** stdlib only (`bufio`, `os`, `strings`).
- **Used by / entrypoint:** entrypoint: `main.go` loads `~/.eigen/.env` at startup.

### internal/config/fields.go
- **Role:** Field metadata describing each settable key for UIs (descriptions, closed option sets, dynamic option sources, multi-select + secret flags).
- **Key symbols:**
  - `Field` (struct) — Key/Desc/Options/Dynamic("providers"|"models")/Multi/Secret. `Secret` marks a credential free-text field: `Get` masks it and secret-shy surfaces (the GUI config form) skip it, while it stays settable.
  - `Fields() []Field` — the settable keys in display order (the source of truth `Keys()` derives from); only `telegram_token` is currently `Secret`.
  - `FieldFor(key) Field` — lookup metadata for one key (zero Field when unknown).
- **Depends on:** none (pure data).
- **Used by / entrypoint:** `internal/config/config.go` (`Keys()`, plus `Set` reads `FieldFor("effort").Options`), `internal/tui/commands.go` (`FieldFor`), `internal/gui/config.go` (`Fields`, `Secret`), and the config picker UIs.

### internal/command/command.go
- **Role:** Custom slash commands (Tier 31) — Claude-Code-compatible markdown commands with frontmatter and `$ARGUMENTS`/`$1..$9` substitution, surfaced as `/<name>`.
- **Key symbols:**
  - `Command` (struct) — Name/Description/ArgHint/Model/AllowedTools/Body/Path/Scope parsed from a `.md` file.
  - `Dirs()` — command dirs in precedence order (project `./.eigen/commands` then user `~/.eigen/commands`).
  - `Set` (struct) + `Load(dirs...)` — discover/parse commands (earlier scope wins); `Get`/`Names`/`All`/`Len` accessors.
  - `parse(name, content)` — split optional `--- … ---` frontmatter (description/argument-hint/model/allowed-tools), tolerating unknown keys.
  - `splitToolList(val)`, `argTokens(args)` — frontmatter tool-list parse and quote-aware arg tokenizer.
  - `Expand(body, args)` — fill `$ARGUMENTS`/`$1..$9`; appends bare args when the body has no placeholder (Claude parity).
- **Depends on:** stdlib only (`os`, `path/filepath`, `regexp`, `sort`, `strconv`, `strings`).
- **Used by / entrypoint:** `internal/tui/completion.go` and `internal/tui/commands.go` (load + expand custom commands), and `internal/gui/bridge.go` (`Load`/`Dirs`/`Expand` to run a custom command on the daemon session).

### internal/hook/hook.go
- **Role:** Fire-and-forget lifecycle hooks (Tier extension seam): run user programs on session/tool/turn/note events, feeding each a small JSON payload on stdin.
- **Key symbols:**
  - Event consts: `OnSessionStart`/`OnSessionStop`/`OnSessionResume`/`OnToolStart`/`OnToolResult`/`OnTurnDone`/`OnNote`.
  - `Spec` (struct) — one configured hook (event, argv command, matcher, disabled).
  - `Payload` (struct) — JSON handed to a hook on stdin (event/session/tool/is_error/step/text).
  - `Observation` (struct) + `Observer` (func) — metadata-only execution telemetry (command hash + argc + status, no raw command/output).
  - `Runner` (struct) + `New(specs)` — dispatcher built from specs (skips malformed/disabled; nil Runner is a valid no-op); `SetObserver`.
  - `(*Runner) Fire(p)` — run every matching hook for an event, fire-and-forget; `specMatches` applies the tool matcher; `runOne` runs argv with a 30s timeout (`hookTimeout`).
  - `(*Runner) Wrap(next, session)` — compose hook firing onto an `agent.EventSink`, then forward.
  - `Load(path)` — parse a hooks config (JSON array or `{"hooks":[...]}`); missing file → nil Runner.
  - `commandHash`, `readFile` — internal helpers (readFile indirected for tests).
- **Depends on:** `internal/agent` (`EventSink`/`Event`/event-kind consts).
- **Used by / entrypoint:** `main.go`, `daemon.go`, `build.go`, `remote_session.go` (load + wire the runner); `internal/observe` (observer); `internal/tui` (commands, tui).

### internal/workflow/workflow.go
- **Role:** Parse and represent authored, replayable multi-step workflows (Tier 17) — markdown with frontmatter and `## <step-id>` sections, hand-rolled parser, `{{var.NAME}}` placeholders.
- **Key symbols:**
  - `OnFailure` + consts `FailStop`/`FailContinue`/`FailRetry` — per-step failure policy.
  - `Step` (struct) — ID/Prompt/Model/Check/OnFailure/Retries.
  - `Workflow` (struct) — Name/Description/Steps.
  - `Dir()` — `~/.eigen/workflows`; `Load(name)` — read+parse a named workflow; `List()` — available workflow names.
  - `Parse(content)` — split frontmatter + `## id` sections into steps, then `validate()`.
  - `parseStep(id, section)` — read leading directive lines (model/check/on_failure/retries) then the prompt body.
  - `splitFrontmatter(content)` — extract name/description from a leading `--- … ---` block.
  - `Interpolate(s, vars)` — replace `{{var.NAME}}`, reporting unset names.
- **Depends on:** stdlib only (`os`, `path/filepath`, `regexp`, `strconv`, `strings`).
- **Used by / entrypoint:** `main.go` (`eigen run`), `internal/tui/workflow.go`, `internal/gui/bridge.go` (`List`/`Load` for the GUI workflow runner).

### internal/workflow/run.go
- **Role:** Execute a parsed workflow on one carried session, with judged checks and per-step failure policies, streaming progress events.
- **Key symbols:**
  - `StepRunner` (func) — runs one step's prompt on a model; implemented by main (keeps workflow free of agent/llm deps).
  - `Judge` (func) — verifies a step's output against a condition; `Reporter` (func) + `Event` (struct) — progress streaming.
  - `RunOpts` (struct) — Vars/Run/Judge/Report.
  - `Result` (struct) — Completed/FailedAt/Outputs.
  - `(*Workflow) Run(ctx, opts)` — run steps in order on one session; judge optional checks; apply on_failure (stop/continue/retry up to Retries); returns a non-nil error only when a stop-on-failure step failed.
  - `excerpt(s)` — 200-char result preview for progress events.
- **Depends on:** stdlib only (`context`, `fmt`, `strings`); collaborates with main via the func types.
- **Used by / entrypoint:** entrypoint: `main.go` calls `wf.Run(...)` for `eigen run`; `internal/gui/bridge.go` drives it with a daemon-backed `StepRunner`.

### internal/harness/harness.go
- **Role:** Install the bundled Rust helper MCP servers (computer-use, workspace) from embedded source by shelling out to cargo only on explicit install.
- **Key symbols:**
  - `SourceFS` (embed.FS) — embedded sources for computer-use-linux, agent-workspace-linux, and chrome-bridge (so install needs no sibling checkouts).
  - `Component` (struct) + `Components` (map) + `ComponentNames()` — the two installable Rust components and their release binaries.
  - `Install(ctx, name, dstDir)` — materialize the source, `cargo build --release --locked`, copy binaries to dst (`~/.local/bin` default).
  - `Materialize(c)` — write a component's embedded tree to a temp dir, returning root + cleanup.
  - `copyExecutable(src, dst)` — atomic, 0755, temp-then-rename install of one binary.
- **Depends on:** stdlib only (`embed`, `io/fs`, `os/exec`, …).
- **Used by / entrypoint:** entrypoint: `main.go` `eigen harness install` (`Components`, `Install`); `Materialize`/`SourceFS`/`ComponentNames` also exercised by `harness_test.go`.

### internal/harness/chrome.go
- **Role:** Install the Chrome connector bridge (MCP server + native-messaging host + extension) from embedded source, and report install status.
- **Key symbols:**
  - `chromeBridgeSourceDir` (const), `ChromeBridgeHostName` (const) — embedded dir and native-host name (`dev.agent_chrome_bridge`).
  - `ChromeBridgeHome()`/`ChromeBridgeMCPScript()`/`ChromeBridgeExtensionDir()` — canonical install paths under `~/.eigen/chrome-bridge`.
  - `ChromeBridgeInstalled()` — true when the MCP script, native host, and extension manifest all exist.
  - `InstallChromeBridge()` — copy the embedded tree, chmod scripts, derive the extension id, write the two native-host manifests; returns (extensionDir, manifests, extensionID, err).
  - `copyEmbeddedFile`/`copyEmbeddedTree`/`walkEmbedded` — embedded-FS copy helpers.
  - `chromeExtensionID(manifestPath)` — derive the Chrome extension id from the manifest `key` (sha256 of the DER, mapped to a-p alphabet).
  - `writeChromeNativeHostManifests` / `writeJSONAtomicHarness` / `writeFileAtomic` — write the per-browser native-messaging-host manifests atomically.
- **Depends on:** stdlib only (`crypto/sha256`, `encoding/base64`, `encoding/json`, …); shares `SourceFS` from `harness.go`.
- **Used by / entrypoint:** entrypoint: `main.go` (`eigen harness` status + chrome-bridge install: `ChromeBridgeInstalled`, `InstallChromeBridge`, `ChromeBridgeHome`, `ChromeBridgeMCPScript`, `ChromeBridgeExtensionDir`).

### internal/harness/orientation.go
- **Role:** Install eigen's native-Go orientation harness integration — state dir, allowlist stub, an `orientation` shell wrapper, and turn/session hooks — and run the orientation CLI.
- **Key symbols:**
  - `OrientationHome()` — persistent orientation state home (delegates to `orientation.DefaultPaths()`).
  - `OrientationInstalled()` — true when the `orientation` wrapper and allowlist both exist.
  - `InstallOrientation(eigenBin, dstDir)` — ensure home, remove legacy JS engine files, install the wrapper.
  - `removeLegacyOrientationEngineFiles()` — delete the old embedded-JS engine files (preserving data/projects).
  - `installOrientationWrapper(eigenBin, dstDir)` — write a small `sh` wrapper that execs `eigen orientation`; `writeExecutable` / `shellQuote` helpers.
  - `InstallOrientationHooks(ctx)` — install turn/session hooks pointing at the wrapper.
  - `RunOrientation(ctx, args)` — `install` subcommand installs wrapper+hooks; otherwise runs `orientation.RunCLI`.
- **Depends on:** `internal/orientation` (`DefaultPaths`, `EnsureHome`, `InstallHooks`, `RunCLI`).
- **Used by / entrypoint:** entrypoint: `main.go` `eigen harness`/`eigen orientation` (`OrientationInstalled`, `OrientationHome`, `InstallOrientation`, `InstallOrientationHooks`, `RunOrientation`).

### internal/syshealth/syshealth.go
- **Role:** Machine health (CPU load, memory, **swap**, disk, **CPU temp**, **GPU**, uptime) for the working-station dashboard. Linux-first via `/proc` + `statfs` + `/sys/class/thermal` + `nvidia-smi`; no required deps/auth; unreadable metrics stay zero. The user trains models here, so swap pressure + GPU util/VRAM/temp/power are first-class.
- **Key symbols:** `Health` (+ `GPU`), `Read()`; readers `readLoadAvg`/`readMemInfo` (mem+swap)/`readDisk`/`readUptime`/`readCPUTemp` (thermal zones, CPU-ish max)/`readGPUs` (`nvidia-smi --query-gpu`, 3s timeout, silent no-op without it); `parseKB`/`pct`.
- **Used by / entrypoint:** `internal/gui/dashboard.go` (`Dashboard()` → Home Machine panel + GPU cards); `main.go:stationDigest` flags high swap/CPU-temp/GPU-temp for the dream.

## Cross-links
- **internal/llm** — `config` parses/renders model refs and looks up catalog providers (`ParseRef`/`Ref`/`Lookup`).
- **internal/theme** ← **internal/config** — config validates the `theme` key against `theme.PaletteNames()`.
- **internal/agent** — `hook` wraps an `agent.EventSink` and keys off `agent.Event` kinds.
- **internal/orientation** — `harness` is a thin installer/launcher in front of the native-Go orientation engine.
- **internal/tui** & **internal/app** — the biggest consumers of `theme` (styling/icons), and the callers of `config`, `command`, `workflow`, and `fuzzy`.
- **internal/gui** — the Wails bridge also drives `config` (the config form), `command` (run a custom command on the daemon session), and `workflow` (the GUI workflow runner, with a daemon-backed `StepRunner`).
- **internal/skill** — shares `fuzzy.Score` for ranking.
- **internal/observe** — registers a `hook.Observer` to log hook executions (metadata only).
- **root CLI (`main.go`/`daemon.go`/`build.go`/`remote_session.go`)** — the primary wiring point: loads config + hooks, runs workflows, installs harness components, prints the theme swatch.

## Dead-code notes
- `theme.Back` glyph — referenced only inside `Swatch()` (the design-system display); no production UI uses the "back" glyph. Lower confidence: it is part of the documented glyph vocabulary and is technically referenced.
