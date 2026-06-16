package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// addMCPServer appends a plugin's MCP server to ~/.eigen/mcp.json as a niche,
// auto-described server entry, expanding ${CLAUDE_PLUGIN_ROOT} in command/args/
// env against the cached bundle root. Idempotent: replaces an entry of the same
// name. eigen marks MCP tools niche at load time (progressive disclosure), so
// the server stays gated behind search_tools — no per-request schema bloat.
func (r *Registry) addMCPServer(name string, s MCPServer, bundleRoot string, entry PluginEntry) error {
	root, err := readObj(r.MCPPath())
	if err != nil {
		return err
	}
	servers, _ := root["servers"].([]any)

	// Build the eigen server entry. eigen's serverConfig uses a single
	// `command` array (command + args), `env`, and a `description`.
	cmd := append([]string{}, s.Command...)
	cmd = append(cmd, s.Args...)
	for i := range cmd {
		cmd[i] = expandRoot(cmd[i], bundleRoot)
	}
	env := map[string]any{}
	for k, v := range s.Env {
		env[k] = expandRoot(v, bundleRoot)
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
	}
	if len(cmd) > 0 {
		srv["command"] = toAnySlice(cmd)
	}
	if len(env) > 0 {
		srv["env"] = env
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
		list = append(list, jsonObj{
			"event":   h.Event,
			"command": toAnySlice(cmd),
		})
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
	// Skills.
	for _, sd := range rec.Skills {
		_ = os.RemoveAll(filepath.Join(r.SkillsDir(), sd))
	}
	// MCP servers + hooks: drop entries whose name starts with "<plugin>-" (mcp)
	// or whose command references the plugin's bundle dir (hooks).
	r.removeMCPByPrefix(pluginName + "-")
	r.removeHooksByRoot(filepath.Join(r.PluginsDir(), pluginName))
	// Cached bundle.
	if rec.Root != "" {
		_ = os.RemoveAll(rec.Root)
	} else {
		_ = os.RemoveAll(filepath.Join(r.PluginsDir(), pluginName))
	}
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
