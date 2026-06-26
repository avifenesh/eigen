# Documentation & project meta

> This slice is Eigen's prose + project-meta layer: the user-facing README and
> roadmap, the contributor/security/conduct policy files, the long-form design
> and research notes under `docs/`, and the three build/module config files
> (`Makefile`, `wails.json`, `go.mod`). It contains **no Go source** — every
> file is Markdown, JSON, or a Makefile. Its job is to explain what Eigen is,
> how to build/verify it, what the visual language and memory/compaction designs
> are, and to pin the toolchain + dependency set. These files are read by humans
> (and RAG retrievers) rather than imported by code; the config files
> (`Makefile`/`wails.json`/`go.mod`) are the only ones consumed by tooling.
> Eigen is a terminal-first Go coding agent (CLI + TUI + daemon + plugin system)
> with an in-progress Wails v3 + Svelte 5 desktop GUI.

## Files

### README.md

- **Role:** The front-door project description: what Eigen is (terminal-first
  Go coding agent: CLI, TUI, daemon, plugins, observability), an explicit
  "built-for-me personal project" framing, requirements, install/quickstart,
  core concepts, config, bundled harness helpers, and a docs index.
- **Key symbols:** Not code. Notable load-bearing sections: *What this is (and
  what it isn't)* (opinionated-personal-project stance); *Quick proof signals*
  (module `github.com/avifenesh/eigen`, `make gate` as the verification command,
  local-first `~/.eigen` runtime, `.env` ignored); *Requirements* (Go pinned in
  `go.mod`, git, ripgrep, bash core; Rust/Node/desktop tooling only for optional
  harness helpers); *Configuration* (`~/.eigen/config.json`, `~/.eigen/providers.json`,
  custom-provider catalog example, `EIGEN_*` env vars, `--skill`); *Built-in
  harness helpers* (`eigen harness install` and per-helper installers for
  orientation / chrome-bridge / computer-use-linux / agent-workspace-linux).
- **Depends on:** Documentation only. Links to `docs/plugins.md`,
  `docs/memory-system.md`, `ROADMAP.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `SECURITY.md`, `LICENSE`, `internal/harness/embedded/README.md`, and screenshots
  in `docs/images/`. All linked targets verified to exist.
- **Used by / entrypoint:** entrypoint: GitHub repo landing page / pkg.go.dev.
  First file a new contributor or user reads. Not referenced by Go code.

### ROADMAP.md

- **Role:** The forward plan + a terse "shipped ledger" of tiers (Tier 1 → Tier
  33). Explicitly *not* a changelog; the source of truth for what's queued
  (Now/Next/Later), deferred-by-decision items, and the verify-gate conventions.
- **Key symbols:** Not code. Key sections: *Roadmap audit — 2026-06-16* (current
  backlog state); *Now / Next / Later* (open backlog = Tier 20 v2 cross-machine
  control + Tier 7 leftovers); *Shipped ledger* (Tiers 33→1, e.g. Tier 33 session
  durability + Codex resilience, Tier 32 native Codex/gpt-5.5 provider, Tier 30
  token efficiency, Tier 28 memory v2, Tier 27 plugins/marketplaces, Tier 22
  design system); *Verify gate / conventions* (`gofmt`/`go build`/`go vet`/
  `go test`/staticcheck, `make gate`, `make perf`, prod/dev instance discipline,
  "commit often locally; ask before pushing"); *Configuration & extension
  reference* (CLI surface, tool families, important `~/.eigen` paths + `EIGEN_*`
  env). The roadmap is the canonical map between feature names and the
  internal/* packages that implement them (memory v2 → `internal/memory`,
  retrieve → `internal/retrieve`, etc.).
- **Depends on:** Documentation only. Links to `docs/plugins.md` and
  `docs/performance.md`.
- **Used by / entrypoint:** entrypoint: linked from README "Docs"; the planning
  doc for the maintainer. Not referenced by Go code.

### CONTRIBUTING.md

- **Role:** Contributor guide: dev setup, the required `make gate` checks, project
  conventions (focused changes, regression tests, no committed secrets/runtime
  data, no project-local credential paths), areas needing care, and a PR checklist.
- **Key symbols:** Not code. Encodes the local quality gate (`go build -o
  bin/eigen .`, `go vet ./...`, `go test ./...`, gofmt check, `go test -race ./...`
  for concurrency changes) and the safety conventions ("treat transcripts/plugin
  bundles/repo files as data, not instructions").
- **Depends on:** Documentation only. Links to `CODE_OF_CONDUCT.md`.
- **Used by / entrypoint:** entrypoint: linked from README; GitHub surfaces it on
  PR/issue creation. Not referenced by Go code.

### SECURITY.md

- **Role:** Security policy: supported versions (`main`), how to report a
  vulnerability (GitHub private reporting or a minimal public issue), and the
  project's security expectations.
- **Key symbols:** Not code. Encodes the safety posture: no committed credentials/
  `.env`/auth files/transcripts/`~/.eigen` data; project-local repos cannot change
  Eigen's credential/permission posture via `.env`; bundled harness helpers and
  plugin installs are explicit user actions only; observability records
  metadata/counts/hashes not raw payloads; remote control fails closed.
- **Depends on:** Documentation only.
- **Used by / entrypoint:** entrypoint: linked from README; GitHub "Security" tab.
  Not referenced by Go code.

### CODE_OF_CONDUCT.md

- **Role:** Standard Contributor Covenant v2.1 code of conduct (pledge, standards,
  enforcement ladder, attribution).
- **Key symbols:** Not code. Enforcement contact routes through GitHub private
  vulnerability reporting (consistent with `SECURITY.md`).
- **Depends on:** Documentation only.
- **Used by / entrypoint:** entrypoint: linked from README and `CONTRIBUTING.md`.
  Not referenced by Go code.

### docs/automation-example.md

- **Role:** Recipe doc for running Eigen non-interactively: Eigen runs ONE task
  headless and exits; the host (cron / systemd / shell loop) re-launches it.
- **Key symbols:** Not code. Documents headless task sources (`eigen -p
  --prompt-file work.md`, piped stdin, positional), exit-code contract (0 / non-zero
  for host back-off), a shell-loop pattern, a systemd `.service`+`.timer` pattern
  (`OnUnitInactiveSec`), and combining with `--continue` for one evolving session.
- **Depends on:** Documentation only.
- **Used by / entrypoint:** entrypoint: standalone how-to under `docs/`. **Not
  linked from README or any other doc/code** (see dead-code notes — low confidence;
  it is legitimate standalone reference material, just unlinked).

### docs/design-inventory.md

- **Role:** A pre-redesign census ("the map") of every visual atom in the TUI/app
  before the Tier-22 luxury redesign: glyphs, colors, layout, components, motion,
  transcript rendering, app shell — descriptive + opinionated, with `file:line`
  refs. Each top-10 design problem is annotated DONE/REMAINING.
- **Key symbols:** Not code. References real source it audits: `internal/tui`
  (brand.go, view.go, blocks.go, rail.go, sidebar.go, header.go, rightpanel.go,
  composer.go, tray.go, palette.go) and `internal/app/app.go`, plus
  `internal/theme/theme.go` and `internal/theme/icons.go`. Records that the whole
  top-10 slate is done as of 2026-06-14.
- **Depends on:** Documentation only (audits `internal/tui` + `internal/app` +
  `internal/theme`).
- **Used by / entrypoint:** entrypoint: cross-referenced from `docs/design-system.md`
  as the redesign "map". Not referenced by Go code.

### docs/design-references.md

- **Role:** The inspiration/principles layer for the from-scratch design system:
  reference products (Charmbracelet/Crush, Catppuccin/Rosé Pine/Nord, Warp,
  lazygit/k9s, atuin/Starship), the "big levers" (elevation surfaces, one icon
  set, markdown-as-document, spacing scale, etc.), a concrete dark palette
  direction, what to preserve, and open questions for the user.
- **Key symbols:** Not code. Captures the design decisions later realized in
  `internal/theme` (named elevation surfaces base→surface→overlay, one monochrome
  icon set, brand-accent restraint).
- **Depends on:** Documentation only.
- **Used by / entrypoint:** entrypoint: cross-referenced from `docs/design-system.md`
  as the references layer. Not referenced by Go code.

### docs/design-system.md

- **Role:** The single durable design-system brief (v2 "deep teal" luxury
  redesign) — the source of truth for color, type weight, glyphs, spacing, and
  the rules keeping `internal/tui` and `internal/app` looking like one product.
- **Key symbols:** Not code. Documents the live theme contract: roles in
  `internal/theme/theme.go` (Text/Dim/Faint/Accent/Title/Focus/Sel/Ok/Warn/Err/
  Tool/Code/Link/Working/OnBright + `BreathRamp`/`WorkingRamp`), THE BRAND RULE
  (blue = brand/structure only; Focus/Sel are non-blue), the fixed glyph
  vocabulary, the icon set in `internal/theme/icons.go`, elevation via
  `fillBG`/`internal/tui/surface.go`, the app-shell aliasing in
  `internal/app/style.go`, the `eigen theme` swatch (`internal/theme/swatch.go`),
  and the drift-guard test `TestNoRawColorLiteralsOutsideTheme`
  (`internal/theme/drift_test.go`). States: "When in doubt, this doc wins; update
  it in the same commit as any visual change."
- **Depends on:** Documentation only. Cross-links `docs/design-inventory.md`
  (the map) and `docs/design-references.md` (the references).
- **Used by / entrypoint:** entrypoint: the design contract for `internal/theme`/
  `internal/tui`/`internal/app` work. Named string "design-system" appears in
  `main.go`, `internal/theme/swatch.go`, `internal/theme/drift_test.go` — but
  those are the *theme/role system*, not links to this `.md` (the doc itself is
  human-read).

### docs/memory-system.md

- **Role:** The plan for "memory v2": a codex-style tiered, structured,
  self-maintaining memory pipeline (raw → curated `MEMORY.md` → injected
  `SUMMARY.md` + `bans.md`), staged build order S1–S7. (Tier 28 in the roadmap;
  largely shipped.)
- **Key symbols:** Not code. Describes the design realized in `internal/memory`
  (tiered `Store` paths, `InjectedContext()`), `internal/dream` (Distill/
  Consolidate/SynthesizeSkill), the `index.sqlite` job queue, `internal/transcript`
  ingestion, the `BgRegistry` background-job infra, the native `ban` capability,
  and the build stages.
- **Depends on:** Documentation only (plans `internal/memory`, `internal/dream`,
  `internal/transcript`, daemon background jobs).
- **Used by / entrypoint:** entrypoint: linked from README "Docs" as the memory
  system plan. Not referenced by Go code.

### docs/performance.md

- **Role:** The performance + resource-health reference (Tier 23) and token
  efficiency reference (Tier 30): what's bounded, baselines, and how to watch for
  regressions.
- **Key symbols:** Not code. Documents live knobs/symbols that DO exist in source
  (verified): `maxReplayEvents` (`internal/daemon/session.go`), `maxRetainedTasks`
  (`internal/agent/background.go`), `compactTriggerFrac`/drive-loop usage summing
  (`internal/agent/agent.go`), `maxInjectedBytes`/`clampMemoryTail`
  (`internal/memory/memory.go`), bg-task disk retention (`adoptStale` in
  `internal/agent/taskstore.go`), the soak/bench tests, and `make perf` /
  `eigen daemon stats`. Also the cache-token accounting table and the
  "canonical compact JSON in the data plane" principle.
- **Depends on:** Documentation only (documents `internal/daemon`, `internal/agent`,
  `internal/memory`, `internal/llm`).
- **Used by / entrypoint:** entrypoint: linked from `ROADMAP.md`; referenced by
  the `make perf*` targets it documents. Not referenced by Go code.

### docs/plugins.md

- **Role:** The plugins & marketplaces reference (Tier 27): how Eigen consumes
  Claude/Codex-format marketplaces, CLI/TUI usage, what each component wires into
  under `~/.eigen`, the safety model, manifest discovery order, supported source
  forms, and custom slash commands (Tier 31).
- **Key symbols:** Not code. Documents the user surface: `eigen marketplace`/
  `eigen plugin` subcommands, the component→`~/.eigen` wiring table (skills/agents/
  commands/MCP/hooks), `${EIGEN_PLUGIN_ROOT}` rewriting, scanner+rollback safety,
  install-is-user-only, and `~/.eigen/commands/*.md` slash-command format
  (`$ARGUMENTS`/`$1..$9`, frontmatter `description`/`argument-hint`).
- **Depends on:** Documentation only (describes the plugin/marketplace + commands
  subsystems).
- **Used by / entrypoint:** entrypoint: linked from README "Docs" and `ROADMAP.md`
  (Tier 27 v1.1). The most-cross-linked doc in the slice. Not referenced by Go code.

### docs/research-codex-memory.md

- **Role:** Research note reverse-engineering how OpenAI Codex implements memory
  (three-layer file/git store, two-phase background pipeline, usage-based
  retention, citations), then a gap table vs Eigen + recommended adoption order.
  The empirical basis for `docs/memory-system.md`.
- **Key symbols:** Not code. Documents the external Codex design (`memory_summary.md`
  / `MEMORY.md` / `rollout_summaries/`, `memories_1.sqlite` job queue,
  `memory_stage1` + `memory_consolidate_global`) and maps each lesson to an Eigen
  build step.
- **Depends on:** Documentation only.
- **Used by / entrypoint:** entrypoint: cross-referenced from
  `docs/research-compaction.md` as a companion. Not linked from README; not
  referenced by Go code.

### docs/research-compaction.md

- **Role:** Research note on compaction / token-saving across coding-agent
  harnesses (Claude Code 2.1.x + Codex `codex-rs` + Anthropic web docs), with a
  ranked gap analysis vs Eigen and a recommended build order; many items annotated
  "shipped".
- **Key symbols:** Not code. Documents external techniques (microcompaction /
  tool-result shedding, circuit breaker, threshold+buffer, fixed prefix,
  error-driven compaction, third-person handoff prefix) and Eigen's baseline
  (`llm.EstimateTokens`, `Agent.MaxContextTokens`, `internal/llm/compact*.go`,
  `Session.drive`). Companion to `docs/research-codex-memory.md`.
- **Depends on:** Documentation only (analyzes `internal/llm`, `internal/agent`).
- **Used by / entrypoint:** entrypoint: standalone research doc. **Not linked from
  README or any other doc, and not referenced by Go code** (see dead-code notes —
  low confidence; legitimate research artifact).

### docs/hooks-example.json

- **Role:** A small example hooks config file (sibling artifact in `docs/`, listed
  by the `git ls-files docs/*` net only as JSON, not in the `docs/*.md` set, but
  it lives in this slice's directory).
- **Key symbols:** Not code. An example `hooks.json` payload illustrating the
  hooks substrate (the format `~/.eigen/hooks.json` uses, per `docs/plugins.md`).
- **Depends on:** None.
- **Used by / entrypoint:** entrypoint: example file a user copies. **Not
  referenced by any `.md` or `.go` file** (grep returned nothing) — see dead-code
  notes (low confidence; example assets are intentionally unreferenced).

### Makefile

- **Role:** The build/verify/run entrypoint for the whole repo. Defines the
  canonical local gate (`make gate`), GUI build/run targets, perf guards, and
  harness install.
- **Key symbols:** Targets — `build` (`go build -o bin/eigen .`); `gui-run`
  (`go run -tags 'wails production webkit2_41' . gui`); `gui-desktop`
  (`go build -tags 'wails production webkit2_41' -o bin/eigen-gui .`); `gui-smoke`
  (`scripts/gui-smoke.sh`, verified to exist); `vet`/`test`/`race`/`fmt`;
  `gate` (build+vet+test+gofmt check); `harness` (`bin/eigen harness install`);
  `perf` → `perf-soak`+`perf-tokens`+`perf-bench` (run specific test selectors in
  `internal/daemon`/`internal/agent`/`internal/llm`/`internal/tool`/`internal/memory`);
  `stats` (`bin/eigen daemon stats`); `clean`.
- **Depends on:** The Go toolchain + the repo's `internal/*` test selectors;
  `scripts/gui-smoke.sh`; the built `bin/eigen` binary (for `harness`/`stats`).
- **Used by / entrypoint:** entrypoint: invoked directly by developers/CI and
  prescribed by README + CONTRIBUTING + ROADMAP. The build wires the GUI via the
  `wails`/`webkit2_41`/`production` build tags (see `main_gui_wails.go`).

### wails.json

- **Role:** Wails v3 desktop-app config (app name/output filename `eigen`, author,
  product info, build tags, frontend dir).
- **Key symbols:** Not code. `name: "Eigen"`, `outputfilename: "eigen"`,
  `build:tags: "wails webkit2_41"`, `frontend:dir: "internal/gui/static"`. Note:
  `frontend:dir` points at `internal/gui/static`, which **does not exist** — the
  built Svelte frontend actually lives at `internal/gui/frontend/dist` and is
  embedded via `//go:embed all:internal/gui/frontend/dist` in `main_gui_wails.go`
  (see dead-code/drift notes).
- **Depends on:** Consumed by the Wails CLI tooling (`wails3`), not by `go build`
  directly — the binary's asset embedding is done in `main_gui_wails.go`, so a
  plain `make gui-desktop` does not need this path.
- **Used by / entrypoint:** entrypoint: Wails CLI (`wails3 build/dev`). The actual
  GUI binary is built through the Makefile `gui-*` targets / `main_gui_wails.go`,
  which do not read `frontend:dir`.

### go.mod

- **Role:** The Go module definition + dependency manifest. Pins the toolchain
  and every direct/indirect dependency.
- **Key symbols:** module `github.com/avifenesh/eigen`; `go 1.26.4`. Direct deps:
  chroma v2 (syntax highlighting), charmbracelet bubbles/bubbletea/lipgloss/x/*
  (the TUI stack), creack/pty (real PTY terminal), mattn/go-isatty,
  muesli/termenv, `wailsapp/wails/v3 v3.0.0-alpha2.105` (the desktop GUI),
  `modernc.org/sqlite` (pure-Go sqlite for the memory/job index). Indirect deps
  cover Wails webview2, dbus, websocket, etc.
- **Depends on:** Nothing internal — it IS the dependency root.
- **Used by / entrypoint:** entrypoint: the Go toolchain (`go build`/`go test`/
  `go mod`). The README's "Quick proof signals" and "Requirements" reference the
  pinned toolchain here.

## Cross-links

- **`internal/theme`** — `docs/design-system.md`/`design-inventory.md`/
  `design-references.md` are the spec/source-of-truth for the theme roles, icon
  set, elevation surfaces, and drift-guard test (theme-system / tui-render slices).
- **`internal/tui` + `internal/app`** — the design docs govern their visual
  language; the design-inventory audits both with `file:line` refs (tui-core,
  tui-panels, tui-render, app-superapp slices).
- **`internal/memory` + `internal/dream` + `internal/transcript`** —
  `docs/memory-system.md` and `docs/research-codex-memory.md` are the plan/research
  behind the memory v2 pipeline (memory/dream slice).
- **`internal/daemon` + `internal/agent` + `internal/llm`** —
  `docs/performance.md` documents their bounded structures/knobs;
  `docs/research-compaction.md` analyzes their compaction code (daemon, agent,
  llm-* slices).
- **Plugins / marketplaces / commands subsystem** — `docs/plugins.md` is its
  user-facing reference (skill-feed-retrieve / plugin slices).
- **GUI build (`internal/gui` + `main_gui_wails.go`)** — `Makefile` `gui-*`
  targets, `wails.json`, and `go.mod`'s `wails/v3` dep are this slice's wiring
  into the Wails v3 + Svelte 5 GUI (gui-bridge slice).
- **Root command + CLI surface (`main.go`, `internal/cmd`)** — `README.md` and
  `ROADMAP.md` document the `eigen ...` subcommand surface and `EIGEN_*` env
  (root-cmd slice).
- **`internal/harness/embedded`** — README's "Built-in harness helpers" section
  links its `README.md` and documents `eigen harness install` (voice-speech-
  clipboard / harness slices).
