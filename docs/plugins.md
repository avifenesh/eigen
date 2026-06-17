# Plugins & marketplaces (Tier 27)

eigen consumes **Claude- and Codex-format** plugin marketplaces. A marketplace is
a catalog repo (or local directory) listing plugins; a plugin bundles skills,
agents, slash commands, MCP servers, hooks, and sometimes Codex app integrations.

Installs are a **user action only**: CLI, TUI slash command, or app-page action.
The agent does not get a tool that can install/remove plugins.

## Use it

```sh
# Add a marketplace catalog (GitHub owner/repo, full GitHub URL, local dir, or a
# direct marketplace.json URL):
eigen marketplace add https://github.com/agent-sh/agentsys
eigen marketplace add /path/to/codex/plugins/openai-bundled
eigen marketplace list
eigen marketplace update          # re-check enabled catalogs are reachable
eigen marketplace disable <name>  # keep it recorded, don't search/update it
eigen marketplace enable  <name>
eigen marketplace remove <name>   # alias: delete

# Install a plugin from any added marketplace (scanned on the small model):
eigen plugin install <name>
eigen plugin install <name> --marketplace <name>   # disambiguate
eigen plugin install <name> --force                # install despite a RISKY scan
eigen plugin install <name> --no-scan              # skip the scan (not recommended)

eigen plugin list
eigen plugin disable <name>       # keep installed, stop loading (new sessions)
eigen plugin enable  <name>
eigen plugin remove  <name>       # alias: delete; reverse all wiring + delete bundle
```

In the TUI:

- bare `/plugins`, `/plugin`, or `/marketplace` opens the plugins page;
- in Marketplace, `enter` opens a catalog; already-installed plugins are hidden because they live in the Plugins tab; `j/k` moves, `v` previews manifest/components, `space` marks/unmarks plugins, and `i` installs all marked plugins (or the current one if none are marked); `Shift+U` pulls marketplace updates and overwrites installed plugins from that marketplace while adding newly available plugins to the catalog;
- `/plugin list|install|remove|delete|enable|disable` and
  `/marketplace list|add|update|remove|delete|enable|disable` call the same registry paths as the CLI.

## What gets wired

A plugin's components flow into the **global** per-scope configs under `~/.eigen`:

| Component | Claude/Codex source | Wired into | Notes |
|---|---|---|---|
| Skills | `skills/<n>/SKILL.md`, manifest `skills` path, or root `SKILL.md` | `~/.eigen/skills/<plugin>-<n>/` | namespaced; `${CLAUDE_PLUGIN_ROOT}` / `${CODEX_PLUGIN_ROOT}` rewritten to `${EIGEN_PLUGIN_ROOT}` |
| Agents | `agents/*.md` or manifest `agents` path | `~/.eigen/skills/<plugin>-agent-<n>/` + task role | adapted into loadable Eigen skills and exposed as foreground/background `task` roles |
| Commands | `commands/*.md` or manifest `commands` path | `~/.eigen/commands/<plugin>-<n>.md` | appears as `/<plugin>-<n>` in the TUI |
| MCP servers | `.mcp.json`, manifest `mcpServers`, or Codex `mcp_servers` | `~/.eigen/mcp.json` | **niche** (gated behind `search_tools`), auto-described, root vars rewritten |
| Hooks | `hooks/hooks.json` or manifest `hooks` | `~/.eigen/hooks.json` | Claude events mapped (`PostToolUse`→`tool_result`, …) |
| Codex app integrations | manifest `apps` | not wired yet | counted and warned; app/runtime integration is deferred |

Installed plugin agents can be used with the `task` tool by setting `role` to the generated agent name (for example `next-task-agent-task-discoverer`). The app and chat command palettes surface installed agent roles: the app jumps to the owning plugin detail, while the chat palette pre-fills a task-role prompt. Agent frontmatter may provide routing metadata (`kind`, `difficulty`, `model`) and read-only tool metadata (`tools` / `allowed-tools`, `read_only`). Agents without read-only metadata inherit the normal task toolset and approval gates. Agents with a verified read-only allowlist (`read`, `grep`, `glob`, `list`/`ls`, `tree`, `symbols`, `diff`) can also be used in `task_group`; mutating/unknown plugin agents stay blocked from parallel fan-out.

The bundle is cached at `~/.eigen/plugins/<name>/` so root placeholders resolve.
Installs are recorded in `~/.eigen/plugins-installed.json` (with the exact files
written) so `remove` reverses cleanly.

## Safety

- **Scanned before install**: each skill, command, and adapted agent body goes
  through the same LLM security scanner as `eigen skill add`. A RISKY verdict
  blocks the install (rolling back partial wiring) unless `--force`.
- **MCP servers stay niche + gated**: the agent only sees them after unlocking via
  `search_tools` — no per-request schema bloat, no auto-run.
- **User-only installs**: there is no agent tool that installs plugins. Untrusted
  bundle code never runs on install.
- **Fetch guards**: GitHub bundles download as a single tarball (codeload, no git
  binary) with path-traversal, symlink-escape, and size caps.

## Format notes

Marketplace manifests are discovered in this order:

1. `.claude-plugin/marketplace.json`
2. `.agents/plugins/marketplace.json` (Codex bundled/local marketplace layout)
3. `marketplace.json`

Plugin manifests are discovered in this order:

1. `.claude-plugin/plugin.json`
2. `.codex-plugin/plugin.json`

Supported marketplace `source` forms:

- string relative path: `"./plugins/foo"`
- Claude local object: `{ "source": "local", "path": "./plugins/foo" }`
- Claude/Codex GitHub object: `{ "source": "github", "repo": "owner/repo", "ref": "v1" }`
- agentsys-style URL object: `{ "source": "url", "url": "https://github.com/owner/repo.git", "commit": "..." }`
- Codex subdir object: `{ "source": "git-subdir", "url": "https://github.com/owner/repo.git", "path": "plugins/foo", "sha": "..." }`

Non-GitHub git hosts are not fetched yet; direct HTTPS `marketplace.json` URLs
are supported for catalogs whose plugin entries use external GitHub-style source
objects (relative local plugin paths require a marketplace repo/directory base).

## Custom slash commands (Tier 31)

eigen reads **Claude-format** slash commands, so a plugin's `commands/*.md` — and
your own hand-authored ones — work unchanged.

- **Locations:** `~/.eigen/commands/*.md` (global) and `./.eigen/commands/*.md`
  (project; shadows global). Plugin-installed commands land in the global dir as
  `<plugin>-<name>.md`.
- **Format:** optional `--- … ---` frontmatter (`description`, `argument-hint`;
  `allowed-tools`/`model` are tolerated and ignored) + a markdown body that
  becomes the prompt.
- **Arguments:** `$ARGUMENTS` → everything you typed after the command; `$1`..`$9`
  → positional tokens (quoted groups respected). A command with no placeholder
  gets your args appended.
- **Use:** type `/<name> [args]` in the TUI (it appears in the `/` menu with its
  description + arg-hint). The body is expanded and submitted as a normal turn —
  the model then does the work with the regular toolset, composing with approvals
  + steering (like `/workflow`).

Example: `~/.eigen/commands/pr.md`

```markdown
---
description: open a PR for the current branch
argument-hint: "[base-branch]"
---
Review the staged changes, write a tight PR description, and open a PR against $ARGUMENTS.
```

→ `/pr main`
