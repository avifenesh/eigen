# Eigen

[![CI](https://github.com/avifenesh/eigen/actions/workflows/ci.yml/badge.svg)](https://github.com/avifenesh/eigen/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/avifenesh/eigen.svg)](https://pkg.go.dev/github.com/avifenesh/eigen)

Eigen is a terminal-first coding agent for Go/Linux workstations: a CLI, TUI, daemon, plugin system, and observability dashboard built around long-running local sessions.

Use it when you want a local agent loop you can inspect, resume, route across models, extend with skills/plugins, and keep under normal approval gates instead of a stateless one-shot wrapper.

## Quick proof signals

- Go module: `github.com/avifenesh/eigen`
- Main verification command: `make gate` (`go build`, `go vet`, `go test ./...`, gofmt check)
- Local-first runtime: daemon socket, transcripts, memory, plugins, and config live under `~/.eigen`
- Safety posture: project-local `.env` files are ignored; credentials are loaded from trusted user config, not from untrusted repos

## Why this project

Use Eigen when you need:

- a persistent coding-agent daemon with resumable sessions;
- a TUI/app surface for live sessions, projects, models, providers, memory, plugins, crons, and observability;
- model routing for delegated work while preserving the user-selected main model;
- plugin/skill/command compatibility with Claude- and Codex-style ecosystems;
- local telemetry for errors, tools, model/token usage, hooks, route decisions, subagents, and runtime health.

## Installation

Eigen currently builds from source.

```bash
git clone https://github.com/avifenesh/eigen.git
cd eigen
make build
./bin/eigen --help
```

For a user-local install:

```bash
install -Dm755 ./bin/eigen "$HOME/.local/bin/eigen"
eigen --help
```

## Quick start

```bash
# Run one task in the current repo.
eigen "summarize the project layout"

# Open the terminal app dashboard.
eigen app

# Run the full local quality gate before committing.
make gate
```

What happens:

1. `eigen` loads defaults from `~/.eigen/config.json` and credentials from trusted user-level config.
2. Interactive sessions attach to the local daemon unless daemonless mode is requested.
3. Session transcripts, memory, plugin wiring, hooks, and observability data stay local under `~/.eigen`.

## Core concepts

- **Session**: a resumable conversation and tool-use loop.
- **Daemon**: the long-lived local host for sessions (`eigen daemon`), normally reached through a Unix socket.
- **App/TUI**: terminal dashboards for sessions, projects, config, models, providers, observe, memory, plugins, machines, and scheduled jobs.
- **Tools**: file, shell, search, subtask, observe, plugin, and integration capabilities exposed to the model through approval-aware tool calls.
- **Routing**: optional delegated-work routing. The main model remains the explicit user choice; `/route` only affects delegated subtasks.
- **Custom providers**: add OpenAI-compatible chat/responses endpoints or Anthropic-compatible endpoints, each with its own explicit model catalog, from the app Providers page.
- **Memory**: durable project/global notes injected as compact context, with local storage under `~/.eigen/memory`.
- **Plugins**: Claude/Codex-style plugin bundles for skills, commands, MCP servers, hooks, and task roles.

## Feature highlights

- Persistent local daemon with session attach/resume.
- Bundled harness helper sources for Linux Computer Use, isolated agent workspaces, and orientation/provenance history; install them with `eigen harness install` instead of maintaining sibling checkouts or a separate orientation skill package.
- TUI/app pages for live work, projects, sessions, config, models, providers, observability, memory, crons, machines, and plugins.
- Structured observability for tool failures, model/token usage, skills, hooks, subagents, route decisions, and runtime pressure.
- Background subtasks and task groups with route-aware model selection.
- Plugin marketplace support for Claude/Codex-style bundles.
- Custom slash commands from user and project command directories.
- Approval-aware safety model for risky tool actions.

## Configuration

The primary config file is:

```text
~/.eigen/config.json
```

Common fields include provider/model defaults, routing options, permission mode, theme, and provider-specific settings.

Custom provider catalogs live in:

```text
~/.eigen/providers.json
```

The Providers page in `eigen app` can add a provider without hand-editing JSON. Press `a` on the Providers page and define the protocol (`openai` chat/completions, OpenAI `responses`, or `anthropic`), endpoint, API-key environment variable (or leave it blank for an explicit no-auth local endpoint), and the exact model names Eigen should show. Keep credentials in environment variables or trusted user-level config; do not commit `.env`, `.eigen`, token files, custom provider files with inline keys, or generated transcripts.

Example custom provider catalog:

```json
{
  "providers": [
    {
      "name": "localai",
      "type": "openai",
      "api": "chat",
      "base_url": "http://127.0.0.1:11434/v1",
      "no_auth": true,
      "models": [{ "name": "local-qwen", "id": "qwen-wire", "context_window": 128000 }]
    }
  ]
}
```

Useful environment variables:

- `EIGEN_INSTANCE=dev` — use the development daemon/socket namespace.
- `EIGEN_NO_DAEMON=1` — run a foreground daemonless session.
- `EIGEN_THEME=<name>` — select a theme before startup.

## Built-in harness helpers

Eigen's Go binary embeds the source for the optional harness helpers:

- `computer-use-linux` for real desktop computer-use tools (`computer_use_*` MCP group);
- `agent-workspace-linux` for isolated scratch desktop workspaces (`workspace_*` MCP group);
- `orientation`, a native Go provenance/history engine used to answer “why does this code exist?” without a separate skill package or Node runtime.

They are not required for normal CLI use. To install them intentionally, run:

```bash
eigen harness install
# or one at a time:
eigen orientation install
eigen computer-use install
eigen workspace install
```

The install step builds the bundled Rust desktop sources with Cargo, copies helper binaries into `~/.local/bin`, initializes the native Go orientation state under `~/.eigen/orientation`, writes a small `orientation` wrapper, and installs Eigen orientation hooks that call that wrapper. Eigen auto-registers installed desktop helpers as built-in MCP servers on the next run. This removes the previous requirement for separate `~/projects/computer-use-linux`, `~/projects/agent-workspace-linux`, standalone orientation/get-oriented package checkouts, or a Node-based orientation runtime.

Orientation can also be run through Eigen directly:

```bash
eigen orientation provenance "$PWD" internal/app/app.go
eigen orientation related "$PWD" internal/app/app.go
```

## Development

```bash
make build      # compile ./bin/eigen
make test       # go test ./...
make vet        # go vet ./...
make gate       # build + vet + test + gofmt check
make race       # focused race tests for daemon/agent packages
make harness    # optional: install bundled computer-use + workspace helpers
```

Before opening a PR, run:

```bash
make gate
```

## Limitations / tradeoffs

- Eigen is currently optimized for local Linux terminal workflows.
- Provider credentials and model access are user-supplied; the repo does not include hosted model access.
- Some app pages reflect local machine state (`~/.eigen`, systemd user timers, SSH config) and may show less data on a fresh install.
- Remote control is intentionally constrained; raw unauthenticated daemon networking is not a goal.

## Docs

- [Plugins and marketplaces](docs/plugins.md)
- [Memory system plan](docs/memory-system.md)
- [Roadmap](ROADMAP.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Contributing

Bug reports, focused fixes, tests, and documentation improvements are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md), run `make gate`, and keep credentials or local runtime artifacts out of commits.

## License

MIT. See [LICENSE](LICENSE).
