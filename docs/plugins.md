# Plugins & marketplaces (Tier 27)

eigen consumes **Claude-format** plugin marketplaces: a *marketplace* is a
catalog repo listing *plugins*; a *plugin* bundles components (skills + an MCP
server + hooks). eigen reads the on-disk `.claude-plugin/*.json` format directly,
so an existing Claude marketplace works without re-authoring.

v1 is **consume + manage** (CLI), mirroring `eigen skill add`. The agent cannot
install plugins — it's a user action only (like `/add-dir`).

## Use it

```sh
# Add a marketplace catalog (GitHub owner/repo, optional /subdir and @ref):
eigen marketplace add anthropics/claude-plugins
eigen marketplace list
eigen marketplace update          # re-check catalogs are reachable
eigen marketplace remove <name>

# Install a plugin from any added marketplace (scanned on the small model):
eigen plugin install <name>
eigen plugin install <name> --marketplace <name>   # disambiguate
eigen plugin install <name> --force                # install despite a RISKY scan
eigen plugin install <name> --no-scan              # skip the scan (not recommended)

eigen plugin list
eigen plugin disable <name>       # keep installed, stop loading (new sessions)
eigen plugin enable  <name>
eigen plugin remove  <name>       # reverse all wiring + delete the bundle
```

## What gets wired (v1)

A plugin's components flow into the **global** per-scope configs under `~/.eigen`:

| Component | Source in bundle | Wired into | Notes |
|---|---|---|---|
| Skills | `skills/<n>/SKILL.md` (+ files) | `~/.eigen/skills/<plugin>-<n>/` | namespaced; `${CLAUDE_PLUGIN_ROOT}` expanded to the cached bundle |
| MCP servers | `.mcp.json` | `~/.eigen/mcp.json` | **niche** (gated behind `search_tools`), auto-described, `${ROOT}` expanded |
| Hooks | `hooks/hooks.json` | `~/.eigen/hooks.json` | Claude events mapped (`PostToolUse`→`tool_result`, …) |
| Commands / agents | `commands/`, `agents/` | — | **counted, not wired in v1** (no slash-command-prompt subsystem yet) → v1.1 |

The bundle is cached at `~/.eigen/plugins/<name>/` so `${CLAUDE_PLUGIN_ROOT}`
references in scripts/MCP commands resolve. Installs are recorded in
`~/.eigen/plugins-installed.json` (with the exact files written) so `remove`
reverses cleanly.

## Safety

- **Scanned before install**: each skill body goes through the same LLM security
  scanner as `eigen skill add`. A RISKY verdict blocks the install (rolling back
  any partial wiring) unless `--force`.
- **MCP servers stay niche + gated**: the agent only sees them after unlocking via
  `search_tools` — no per-request schema bloat, no auto-run.
- **CLI-only**: there is no agent tool that installs plugins. Untrusted bundle
  code never runs on install.
- **Fetch guards**: bundles download as a single tarball (codeload, no git
  binary) with path-traversal and size caps.

## Format notes

- Marketplace manifest: `.claude-plugin/marketplace.json` (name, owner, plugins[]
  with a polymorphic `source`: string path / `{source: local|git|github, repo,
  ref}`).
- Plugin manifest: `.claude-plugin/plugin.json` (only `name` required; component
  dirs discovered by convention, manifest paths are additive overrides).
- Codex has no equivalent marketplace format; MCP is the shared interop point, so
  eigen builds on the Claude format.

## v1.1 (follow-up)

App `[plugins]` browse/install page; `/plugin` + `/marketplace` slash commands;
wiring the `commands`/`agents` components (needs a slash-command-prompt subsystem).
