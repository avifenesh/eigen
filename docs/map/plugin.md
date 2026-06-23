# plugin/ — plugin & marketplace layer

> Eigen's plugin + marketplace subsystem (the package's own doc-comment calls it "Tier 27"). A **marketplace** is a catalog repo (`marketplace.json`) that lists many plugins; a **plugin** is a bundle of components — skills, agents, slash commands, MCP servers, hooks, and (Codex) app integrations. The package reads Claude/Codex on-disk formats directly (`.claude-plugin/*`, `.agents/plugins/marketplace.json`, `.codex-plugin/*`) so existing third-party marketplaces install without re-authoring. The flow: **record a marketplace** (`AddMarketplace`) → **fetch a plugin's repo tarball** (`TreeFetcher` via `codeload.github.com`, no git binary) → **discover components by directory convention + manifest overrides** (`Discover`) → **security-scan each skill/command/agent body** (via `skill.Scanner`) → **wire components into the shared `~/.eigen` config** (skills dir, agents dir, `mcp.json`, `hooks.json`, `commands` dir) with full installed-file bookkeeping so `Uninstall` can cleanly reverse everything. Installs are CLI/TUI/GUI-user-triggered only — the agent never auto-installs untrusted bundle code. The registry state lives in two JSON files under `~/.eigen`: `marketplaces.json` and `plugins-installed.json`.

## Files

### internal/plugin/manifest.go
- **Role:** Package doc-comment + the JSON data model and parsers for `marketplace.json` / `plugin.json`, including the polymorphic `source` field.
- **Key symbols:**
  - `Marketplace` — parsed `marketplace.json` catalog (Name, Owner, Metadata, Interface, `[]PluginEntry`).
  - `PluginEntry` — one listed plugin (Name, `Source`, Description, Version, `Strict *bool`, etc.); `(PluginEntry).strictMode()` reports whether missing components are fatal (default true).
  - `Source` — normalized fetch location; `UnmarshalJSON` collapses the string-shorthand (relative path or bare GitHub URL) and object forms (`local|git|github|url|git-subdir`) into one struct; `(Source).IsLocal()` (path lives inside marketplace repo) and `(Source).EffectiveRef()` (pinned commit/sha preferred over branch/tag).
  - `PluginManifest` — parsed `plugin.json`; component fields kept as `json.RawMessage` (string|array|inline-object) for lenient path parsing in discovery; `MCPServersSnake`/`Apps` cover Codex spellings.
  - `PluginInterface` / `Owner` / `MarketMeta` — presentation/metadata sub-structs.
  - `ParseMarketplace` — unmarshals + validates a catalog (requires `name`, each plugin needs a name), backfilling Description/Category from `Interface`.
  - `ParsePluginManifest` — unmarshals a `plugin.json`, requiring a name.
  - `(Marketplace).Find` — case-insensitive plugin lookup by name (used by install).
  - `looksLikeGitURL` / `firstNonEmpty` — helpers.
- **Depends on:** stdlib only (`encoding/json`, `strings`, `fmt`).
- **Used by / entrypoint:** `ParseMarketplace`/`ParsePluginManifest`/`Find` called from `install.go`/`discover.go`; types are the wire format surfaced to `internal/app`, `internal/tui`, `internal/gui`, and root `plugincmd.go`.

### internal/plugin/discover.go
- **Role:** Reads a plugin's on-disk tree and produces a `*Components` — finds skills, MCP servers, hooks, slash commands, agents, and Codex apps by convention dir + manifest path overrides.
- **Key symbols:**
  - `Components` — what a bundle provides (Root, Manifest, Skills, MCPServers, AppServers, Hooks, Commands, Agents, Apps count).
  - `SkillFile` / `CommandFile` / `AgentFile` / `MCPServer` / `HookSpec` — per-component value types; `AgentFile` carries parsed frontmatter (Kind, Difficulty, Model, Tools, ReadOnly + ReadOnlySet).
  - `Discover(root, lenient)` — top-level: parse manifest (Claude then Codex), then run each `discover*` collector; `lenient` (from a marketplace entry's `strict=false`) tolerates a missing `plugin.json`.
  - `discoverSkills` (skills/*/SKILL.md, manifest paths additive, root SKILL.md fallback for single-skill bundles), `discoverCommands` (commands/*.md), `discoverAgents` (agents/*.md with frontmatter parse), `discoverMCP` (.mcp.json or manifest path/inline), `discoverHooks` (hooks/hooks.json, Claude event→matcher→command shape), `discoverApps` (Codex apps normalized as MCP servers).
  - `parseMCPServers` / `parseCommandAndArgs` — tolerant JSON shape handling (mcpServers/mcp_servers/direct map; string-or-array command).
  - `mapHookEvent` — Claude hook event name → eigen event (PreToolUse→tool_start, etc.; unknown passes through lowercased).
  - `splitCmd` — shell string → `[]string`, wrapping shell-metachar commands in `sh -c`.
  - Frontmatter/path helpers: `frontmatterValue`, `frontmatterListAny`, `frontmatterBoolAny`, `splitFrontmatterList`, `resolveComponentPaths`, `markdownFiles`, `countPathField`, `firstRaw`, `rawString`, `isJSONObject`, `isJSONArray`, `readPluginManifest`.
- **Depends on:** `fetch.go` (`safeJoinUnder` for untrusted manifest paths); stdlib (`encoding/json`, `os`, `path/filepath`, `sort`, `strings`).
- **Used by / entrypoint:** `Discover` called from `install.go` (`InstallPlugin`, `PreviewPlugin`) and from `internal/app/plugins.go:installedPluginPreview` (recompute counts for an already-installed plugin).

### internal/plugin/fetch.go
- **Role:** Network fetch of a repo as a tarball + safe extraction, and the path-safety primitives used everywhere untrusted plugin/marketplace paths are joined.
- **Key symbols:**
  - `TreeFetcher` — injectable `func(ctx, owner, repo, ref, destDir) (root, err)` (overridable in tests).
  - `DefaultTreeFetcher` — downloads `codeload.github.com/{owner}/{repo}/tar.gz/{ref}` over one HTTPS request (no git binary), 60s timeout, then extracts.
  - `extractTarGz` — gzip+tar extraction under destDir; skips PAX metadata, rejects `..`/absolute entries, enforces `maxArchiveBytes` (64 MiB) and `maxFileBytes` (8 MiB) tar-bomb guards, skips symlinks/devices, returns the nested `<repo>-<ref>/` top dir.
  - `safeJoinUnder(root, rel, what)` — joins an untrusted relative path under root, rejecting absolute/upward paths and verifying via `ensureResolvedUnder` (EvalSymlinks) that it stays inside root.
  - `ensureResolvedUnder` / `withinDir` / `firstComponent` — path-containment + first-path-element helpers.
  - consts `maxArchiveBytes`, `maxFileBytes`.
- **Depends on:** stdlib only (`archive/tar`, `compress/gzip`, `net/http`, `os`, `path/filepath`, `context`).
- **Used by / entrypoint:** `DefaultTreeFetcher` is the default `TreeFetcher` plugged into `AddMarketplace`/`InstallPlugin`/`PreviewPlugin`; `safeJoinUnder` is used across `discover.go` and `install.go` for every untrusted path.

### internal/plugin/install.go
- **Role:** The install/preview engine — resolves a plugin from a marketplace, fetches its tree, scans, and wires every component into `~/.eigen`, plus marketplace add/fetch and the plugin-root placeholder rewriting.
- **Key symbols:**
  - `InstallOptions` (Scanner, Force, Overwrite, Tree) / `InstallResult` (Plugin, Scans, Warnings) / `ScanFinding` / `PluginPreview` — install I/O types.
  - `(*Registry).AddMarketplace` — fetch a catalog repo, parse `marketplace.json`, record it; returns the parsed `Marketplace`.
  - `(*Registry).InstallPlugin` — the main flow: dedupe, resolve, fetch root, `Discover`, cache bundle (`copyTree`), then scan+wire skills → MCP servers → app servers → hooks → commands → agent roles, set `ScanStatus`, record install; rolls back wired files on any error via a deferred `cleanupPluginFiles`.
  - `(*Registry).PreviewPlugin` — read-only manifest/component summary for marketplace UI (no scan, no wiring); `buildPluginPreview` builds the DTO.
  - `(*Registry).fetchMarketplace` / `resolvePlugin` / `resolvePluginRoot` — locate the entry across recorded marketplaces and return its on-disk root (local subdir vs externally fetched repo).
  - `addDirectMarketplace` / `fetchDirectMarketplace` / `readMarketplaceManifest` / `fetchURL` — handle direct (https-json, local path/dir, `file://`) marketplace sources without a GitHub fetch.
  - Wiring helpers: `installSkillDir` (copy dir, rewrite placeholder, write `.eigen-root` sidecar), `installAgentFile`, `installCommand`, `installedAgentRole` + agent normalizers (`normalizeAgentKind/Difficulty/Tools/ToolName`, `defaultPluginAgentReadOnlyTools` — the task_group read-only trust boundary), `safeComponentName`, `copyTree`.
  - Placeholder rewriting: `pluginRootVar`/`codexPluginRootVar`→`eigenRootVar`; `toEigenRoot` (store form), `expandRoot` (literal expand at write), `ExpandInstalledRoot` (runtime expand of stored `${EIGEN_PLUGIN_ROOT}`).
  - JSON edit helpers `jsonObj`, `readObj`, `writeObj` (atomic tmp+rename, preserves unknown fields); `isHTTP`/`isLocalPath`.
- **Depends on:** `internal/skill` (`skill.Scanner`, `skill.ParseGitHubRef`, `skill.RiskyError`); `registry.go`/`wire.go` (same package); `fetch.go`, `discover.go`, `manifest.go`.
- **Used by / entrypoint:** `AddMarketplace`/`InstallPlugin`/`PreviewPlugin` reached from `plugincmd.go` (CLI `eigen plugin`/`eigen marketplace`), `internal/tui/plugin_commands.go`, `internal/app/install.go`; `ExpandInstalledRoot` is called from `internal/agent/roles.go` to expand role prompts.

### internal/plugin/registry.go
- **Role:** The on-disk registry — path layout under `~/.eigen` and CRUD over `marketplaces.json` + `plugins-installed.json`; name validation.
- **Key symbols:**
  - `Registry` (just a `dir` field) + `NewRegistry` (rooted at `~/.eigen`, shared across instances) / `NewRegistryAt` (tests).
  - Path accessors: `PluginsDir`, `SkillsDir`, `AgentsDir`, `MCPPath`, `HooksPath`, `CommandsDir` (+ unexported `marketsPath`/`pluginsPath`).
  - `MarketRecord` / `InstalledPlugin` / `InstalledAgentRole` — persisted records; `ScanStatusClean/Forced/Skipped` consts.
  - Marketplace CRUD: `Markets`, `AddMarket` (upsert, preserves Added/Disabled), `MarketByName`, `SetMarketEnabled`, `RemoveMarket`.
  - Installed-plugin CRUD: `Installed`, `InstalledByName`, `RecordInstall` (upsert), `RemoveInstall`.
  - `SafeName` + `validName` regex — filesystem-safe name guard.
  - `readJSON` / `writeJSON` — missing-file-tolerant read; atomic pretty-JSON write.
- **Depends on:** stdlib only (`encoding/json`, `os`, `path/filepath`, `regexp`, `sort`, `strings`, `time`).
- **Used by / entrypoint:** `NewRegistry` is the entry into the whole package, called from `internal/gui/plugins.go`, `internal/agent/roles.go`, `internal/app/install.go`+`plugins.go`, `internal/tui/plugin_commands.go`, root `plugincmd.go`. Most methods are the public API consumed by those layers.

### internal/plugin/wire.go
- **Role:** The component-wiring + teardown mechanics for `mcp.json`/`hooks.json` and the enable/disable/uninstall lifecycle (skills/agents/commands handled by file moves).
- **Key symbols:**
  - `(*Registry).addMCPServer` — append/replace a plugin's MCP server in `mcp.json`; rewrites root placeholders to `${EIGEN_PLUGIN_ROOT}` and injects `EIGEN_PLUGIN_ROOT=<bundle>` into env (the one place the path lives); idempotent by name.
  - `(*Registry).addHooks` — append hooks to `hooks.json` (`{"hooks":[...]}` wrapper), expanding the root placeholder; returns count.
  - `(*Registry).Uninstall` — public: reverse wiring + remove record; returns false if not installed.
  - `uninstallFiles` / `cleanupPluginFiles` — best-effort teardown (skill dirs, agent role files incl. legacy generated-skill shape, command files, MCP-by-prefix, hooks-by-bundle-root, cached bundle).
  - `(*Registry).SetEnabled` — flips all of a plugin's components on/off without deleting: `disabled:true` marker on MCP/hooks, `.disabled` park-aside rename for skills/agents/commands; affects new sessions only.
  - Helpers: `removeMCPByPrefix`, `removeHooksByRoot`, `setMCPDisabled`, `setHooksDisabled`, `cmdReferences`, `toAnySlice`, `pluginOf`.
- **Depends on:** `install.go` (`readObj`/`writeObj`/`toEigenRoot`/`expandRoot`/`jsonObj`); stdlib (`os`, `path/filepath`, `strings`).
- **Used by / entrypoint:** `addMCPServer`/`addHooks`/`uninstallFiles`/`cleanupPluginFiles` called internally by `InstallPlugin`; `Uninstall` + `SetEnabled` are public, reached from `internal/gui/plugins.go` (`RemovePlugin`), `internal/app/plugins.go`, `internal/tui/plugin_commands.go`, and `plugincmd.go`.

## Cross-links
- **internal/skill** — install consumes `skill.Scanner` to vet each skill/command/agent body (RISKY → `skill.RiskyError` unless Force), `skill.ParseGitHubRef` to parse `owner/repo@ref`/URL sources, and the skill loader resolves the `.eigen-root` sidecar that `installSkillDir` writes. The package's design intentionally mirrors `eigen skill add` (consume, not author).
- **internal/agent (roles.go)** — turns each plugin's `InstalledAgentRole` + `agents/*.md` prompt into a native Eigen task role (`pluginAgentRoles`, `PluginRoleCatalog`), expanding `${EIGEN_PLUGIN_ROOT}` via `ExpandInstalledRoot`; the read-only tool whitelist (`normalizeAgentTools`) is the task_group trust boundary.
- **internal/gui (plugins.go)** — `*Bridge` methods `Plugins`, `SetMarketEnabled`, `RemoveMarketplace`, `RemovePlugin` expose read + safe-management ops to the Svelte frontend via generated Wails bindings (`frontend/src/lib/bridge.ts`); installing is deliberately NOT exposed to the GUI/agent.
- **internal/app (install.go, plugins.go)** — the TUI/Bubble Tea plugins page drives `AddMarketplace`/`InstallPlugin`/`PreviewPlugin`/`Uninstall`/`SetEnabled` and re-runs `Discover` for installed-plugin previews.
- **internal/tui (plugin_commands.go)** — slash-command surface for `/plugin` and `/marketplace` management.
- **MCP/hooks/commands loaders** — wiring writes into shared `~/.eigen/mcp.json`, `hooks.json`, `commands/`, `skills/`, `agents/` that eigen's runtime loaders read; MCP servers are marked niche (progressive disclosure behind `search_tools`).
- **root plugincmd.go / main.go** — CLI entrypoints `eigen plugin <install|list|remove|enable|disable>` and `eigen marketplace <add|list|remove|enable|disable|update>` (registered at `main.go:314`).
