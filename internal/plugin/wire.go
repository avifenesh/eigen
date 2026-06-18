package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// addMCPServer appends a plugin's MCP server to ~/.eigen/mcp.json as a niche,
// auto-described server entry. The plugin-root placeholder is handled via OUR
// namespaced env param: ${CLAUDE_PLUGIN_ROOT}/${CODEX_PLUGIN_ROOT} in
// command/args/env values are rewritten to ${EIGEN_PLUGIN_ROOT}, and
// EIGEN_PLUGIN_ROOT=<bundle> is injected
// into the server's env. The MCP loader expands ${EIGEN_PLUGIN_ROOT} at launch
// — so the path lives in one env var, not smeared as a literal. Idempotent:
// replaces an entry of the same name. eigen marks MCP tools niche at load time
// (progressive disclosure), so the server stays gated behind search_tools.
func (r *Registry) addMCPServer(name string, s MCPServer, bundleRoot string, entry PluginEntry) error {
	root, err := readObj(r.MCPPath())
	if err != nil {
		return err
	}
	servers, _ := root["servers"].([]any)

	// Build the eigen server entry and rewrite plugin-root placeholders to OUR env ref.
	cmd := append([]string{}, s.Command...)
	cmd = append(cmd, s.Args...)
	for i := range cmd {
		cmd[i] = toEigenRoot(cmd[i])
	}
	env := map[string]any{eigenRootEnv: bundleRoot} // the one place the path lives
	for k, v := range s.Env {
		env[k] = toEigenRoot(v)
	}
	desc := s.Description
	if strings.TrimSpace(desc) == "" {
		desc = entry.Description
	}
	if strings.TrimSpace(desc) == "" {
		desc = name + " (from plugin " + pluginOf(name) + ")"
	}
	srv := jsonObj{
		"name":        name,
		"description": desc,
		"env":         env,
	}
	if len(cmd) > 0 {
		srv["command"] = toAnySlice(cmd)
	}
	if strings.TrimSpace(s.URL) != "" {
		srv["url"] = toEigenRoot(s.URL)
	}
	if strings.TrimSpace(s.Type) != "" {
		srv["type"] = s.Type
	}

	// Replace any existing server of this name; else append.
	replaced := false
	for i, e := range servers {
		if m, ok := e.(jsonObj); ok {
			if nm, _ := m["name"].(string); strings.EqualFold(nm, name) {
				servers[i] = srv
				replaced = true
				break
			}
		}
	}
	if !replaced {
		servers = append(servers, srv)
	}
	root["servers"] = servers
	return writeObj(r.MCPPath(), root)
}

// addHooks appends a plugin's hooks to ~/.eigen/hooks.json (the {"hooks":[...]}
// wrapper form), expanding ${CLAUDE_PLUGIN_ROOT}. Returns the count appended.
func (r *Registry) addHooks(hooks []HookSpec, bundleRoot string) (int, error) {
	if len(hooks) == 0 {
		return 0, nil
	}
	root, err := readObj(r.HooksPath())
	if err != nil {
		return 0, err
	}
	list, _ := root["hooks"].([]any)
	for _, h := range hooks {
		cmd := append([]string{}, h.Command...)
		for i := range cmd {
			cmd[i] = expandRoot(cmd[i], bundleRoot)
		}
		entry := jsonObj{
			"event":   h.Event,
			"command": toAnySlice(cmd),
		}
		if h.Matcher != "" {
			entry["matcher"] = h.Matcher
		}
		list = append(list, entry)
	}
	root["hooks"] = list
	return len(hooks), writeObj(r.HooksPath(), root)
}

// uninstallFiles reverses a plugin's wiring: removes its installed skill dirs,
// its mcp.json server entries, its hooks, and its cached bundle. Best-effort —
// each step is independent so a partial install still cleans up.
func (r *Registry) uninstallFiles(pluginName string) {
	rec, ok := r.InstalledByName(pluginName)
	if !ok {
		// No record (e.g. mid-install rollback): clean by name prefix anyway.
		rec = InstalledPlugin{Name: pluginName, Root: filepath.Join(r.PluginsDir(), pluginName)}
	}
	r.cleanupPluginFiles(rec)
}

func (r *Registry) cleanupPluginFiles(rec InstalledPlugin) {
	if rec.Name == "" {
		return
	}
	root := rec.Root
	if root == "" {
		root = filepath.Join(r.PluginsDir(), rec.Name)
	}
	// Skills.
	for _, sd := range rec.Skills {
		_ = os.RemoveAll(filepath.Join(r.SkillsDir(), sd))
	}
	// Native plugin agents.
	for _, an := range rec.Agents {
		_ = os.Remove(filepath.Join(r.AgentsDir(), an+".md"))
		_ = os.Remove(filepath.Join(r.AgentsDir(), an+".md.disabled"))
		// Legacy installs adapted agents as generated skills; clean that shape
		// too even if the old record did not list the agent under Skills.
		_ = os.RemoveAll(filepath.Join(r.SkillsDir(), an))
	}
	// Commands.
	for _, cn := range rec.Commands {
		_ = os.Remove(filepath.Join(r.CommandsDir(), cn+".md"))
		_ = os.Remove(filepath.Join(r.CommandsDir(), cn+".md.disabled"))
	}
	// MCP servers + hooks: drop entries whose name starts with "<plugin>-" (mcp)
	// or whose command references the plugin's bundle dir (hooks).
	r.removeMCPByPrefix(rec.Name + "-")
	r.removeHooksByRoot(root)
	// Cached bundle.
	_ = os.RemoveAll(root)
}

func (r *Registry) removeMCPByPrefix(prefix string) {
	root, err := readObj(r.MCPPath())
	if err != nil {
		return
	}
	servers, _ := root["servers"].([]any)
	if servers == nil {
		return
	}
	out := servers[:0]
	for _, e := range servers {
		if m, ok := e.(jsonObj); ok {
			if nm, _ := m["name"].(string); strings.HasPrefix(nm, prefix) {
				continue
			}
		}
		out = append(out, e)
	}
	root["servers"] = out
	_ = writeObj(r.MCPPath(), root)
}

func (r *Registry) removeHooksByRoot(bundleRoot string) {
	root, err := readObj(r.HooksPath())
	if err != nil {
		return
	}
	list, _ := root["hooks"].([]any)
	if list == nil {
		return
	}
	out := list[:0]
	for _, e := range list {
		if m, ok := e.(jsonObj); ok {
			if cmdReferences(m["command"], bundleRoot) {
				continue
			}
		}
		out = append(out, e)
	}
	root["hooks"] = out
	_ = writeObj(r.HooksPath(), root)
}

// cmdReferences reports whether a command array contains bundleRoot (so a hook
// belonging to this plugin can be identified for removal).
func cmdReferences(cmd any, bundleRoot string) bool {
	arr, ok := cmd.([]any)
	if !ok {
		return false
	}
	for _, a := range arr {
		if s, ok := a.(string); ok && strings.Contains(s, bundleRoot) {
			return true
		}
	}
	return false
}

// SetEnabled enables or disables ALL of a plugin's wired components at once,
// using the same `"disabled": true` marker convention as the rest of eigen's
// extension config (enabled = absence of the marker). Disabling keeps every
// entry in place (and the cached bundle + installed-skill dirs) so re-enabling
// is instant. Returns false if the plugin isn't installed. Affects NEW sessions
// (a running session keeps its already-connected servers).
func (r *Registry) SetEnabled(pluginName string, enabled bool) (bool, error) {
	rec, ok := r.InstalledByName(pluginName)
	if !ok {
		return false, nil
	}
	// MCP servers: flip `disabled` on entries named "<plugin>-*".
	r.setMCPDisabled(pluginName+"-", !enabled)
	// Hooks: flip `disabled` on entries whose command references the bundle.
	r.setHooksDisabled(rec.Root, !enabled)
	// Skills: a plugin skill is "disabled" by renaming its SKILL.md aside so the
	// skill scanner's `*/SKILL.md` glob no longer finds it (eigen's loader has
	// no in-file disable marker for skills). Renamed back to re-enable.
	for _, sd := range rec.Skills {
		active := filepath.Join(r.SkillsDir(), sd, "SKILL.md")
		parked := active + ".disabled"
		if enabled {
			_ = os.Rename(parked, active)
		} else {
			_ = os.Rename(active, parked)
		}
	}
	// Native agents: park the markdown role file so role discovery skips it.
	for _, an := range rec.Agents {
		active := filepath.Join(r.AgentsDir(), an+".md")
		parked := active + ".disabled"
		if enabled {
			_ = os.Rename(parked, active)
		} else {
			_ = os.Rename(active, parked)
		}
		// Legacy generated-agent-skill fallback.
		activeSkill := filepath.Join(r.SkillsDir(), an, "SKILL.md")
		parkedSkill := activeSkill + ".disabled"
		if enabled {
			_ = os.Rename(parkedSkill, activeSkill)
		} else {
			_ = os.Rename(activeSkill, parkedSkill)
		}
	}
	// Commands: same park-aside trick — the command loader globs commands/*.md,
	// so a .md.disabled suffix hides it.
	for _, cn := range rec.Commands {
		active := filepath.Join(r.CommandsDir(), cn+".md")
		parked := active + ".disabled"
		if enabled {
			_ = os.Rename(parked, active)
		} else {
			_ = os.Rename(active, parked)
		}
	}
	return true, nil
}

func (r *Registry) setMCPDisabled(prefix string, disabled bool) {
	root, err := readObj(r.MCPPath())
	if err != nil {
		return
	}
	servers, _ := root["servers"].([]any)
	for _, e := range servers {
		if m, ok := e.(jsonObj); ok {
			if nm, _ := m["name"].(string); strings.HasPrefix(nm, prefix) {
				if disabled {
					m["disabled"] = true
				} else {
					delete(m, "disabled")
				}
			}
		}
	}
	root["servers"] = servers
	_ = writeObj(r.MCPPath(), root)
}

func (r *Registry) setHooksDisabled(bundleRoot string, disabled bool) {
	if bundleRoot == "" {
		return
	}
	root, err := readObj(r.HooksPath())
	if err != nil {
		return
	}
	list, _ := root["hooks"].([]any)
	for _, e := range list {
		if m, ok := e.(jsonObj); ok && cmdReferences(m["command"], bundleRoot) {
			if disabled {
				m["disabled"] = true
			} else {
				delete(m, "disabled")
			}
		}
	}
	root["hooks"] = list
	_ = writeObj(r.HooksPath(), root)
}

// Uninstall removes an installed plugin (files + record). Returns false if it
// wasn't installed.
func (r *Registry) Uninstall(pluginName string) (bool, error) {
	if _, ok := r.InstalledByName(pluginName); !ok {
		return false, nil
	}
	r.uninstallFiles(pluginName)
	return r.RemoveInstall(pluginName)
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// pluginOf extracts the plugin name from a namespaced "<plugin>-<server>" id
// (best-effort: returns the whole string if no dash).
func pluginOf(name string) string {
	if i := strings.IndexByte(name, '-'); i > 0 {
		return name[:i]
	}
	return name
}
