package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Components is what a plugin bundle provides, discovered from its on-disk tree
// (convention dirs + plugin.json overrides). v1 wires Skills, MCPServers, and
// Hooks; Commands/Agents are counted but not yet wired (no slash-prompt
// subsystem in eigen yet).
type Components struct {
	Root       string          // plugin root dir (where files were found)
	Manifest   *PluginManifest // parsed .claude-plugin/plugin.json (may be nil if absent + lenient)
	Skills     []SkillFile     // skills/<name>/SKILL.md
	MCPServers []MCPServer     // .mcp.json (or manifest mcpServers path)
	Hooks      []HookSpec      // hooks/hooks.json (or manifest hooks path)
	Commands   []CommandFile   // commands/*.md (Claude slash commands)
	Agents     []AgentFile     // agents/*.md adapted into Eigen skills at install
	Apps       int             // Codex app integrations (not wired)
}

// CommandFile is one slash-command markdown file from a plugin's commands/ dir.
type CommandFile struct {
	Name    string // file basename sans .md
	Content string // raw markdown (frontmatter + body)
}

// SkillFile is one discovered skill: its directory name and the raw SKILL.md.
type SkillFile struct {
	Name    string // skill dir name
	Dir     string // directory containing SKILL.md
	Content string // raw SKILL.md (frontmatter + body)
}

// AgentFile is one Claude/Codex subagent markdown file. Eigen does not yet have
// arbitrary plugin-defined Task roles, so install adapts these into loadable
// skills that preserve the agent prompt.
type AgentFile struct {
	Name        string
	Path        string
	Description string
	Content     string
}

// MCPServer is one server from a plugin's .mcp.json, normalized to eigen's
// shape. Name is the server key; Command/Env carry ${...} placeholders that the
// installer expands against the plugin root.
type MCPServer struct {
	Name        string
	Command     []string
	Args        []string
	Env         map[string]string
	URL         string // http/sse remote servers
	Type        string // "", "http", "sse"
	Description string
}

// HookSpec is one hook event→command mapping, normalized from the Claude hooks
// shape (event → matcher groups → command actions) into eigen's flat
// (event, command) spec. Matchers are dropped (eigen hooks fire per event).
type HookSpec struct {
	Event   string
	Command []string
}

// Discover reads the plugin tree at root and returns its components. lenient
// (from the marketplace entry's strict=false) tolerates a missing plugin.json.
func Discover(root string, lenient bool) (*Components, error) {
	c := &Components{Root: root}

	// Manifest (Claude first, then Codex). Both ecosystems keep only the manifest
	// inside the hidden metadata directory; components live at plugin root.
	if b, err := readPluginManifest(root); err == nil {
		m, perr := ParsePluginManifest(b)
		if perr != nil && !lenient {
			return nil, perr
		}
		c.Manifest = m
	} else if !lenient {
		// plugin.json missing: not fatal — convention discovery can still find
		// components — but record nothing and continue. (Claude treats the
		// manifest as the only required file; we're more forgiving so a bundle
		// that is just skills/ still installs.)
		_ = err
	}

	skills, err := discoverSkills(root, c.Manifest)
	if err != nil {
		return nil, err
	}
	c.Skills = skills
	mcp, err := discoverMCP(root, c.Manifest)
	if err != nil {
		return nil, err
	}
	c.MCPServers = mcp
	hooks, err := discoverHooks(root, c.Manifest)
	if err != nil {
		return nil, err
	}
	c.Hooks = hooks
	commands, err := discoverCommands(root, c.Manifest)
	if err != nil {
		return nil, err
	}
	c.Commands = commands
	agents, err := discoverAgents(root, c.Manifest)
	if err != nil {
		return nil, err
	}
	c.Agents = agents
	c.Apps = countPathField(root, c.Manifest, "apps")
	return c, nil
}

func readPluginManifest(root string) ([]byte, error) {
	for _, p := range []string{
		filepath.Join(root, ".claude-plugin", "plugin.json"),
		filepath.Join(root, ".codex-plugin", "plugin.json"),
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, os.ErrNotExist
}

// discoverCommands reads commands/*.md (Claude/Codex slash commands). If a
// manifest declares command paths, those replace the default commands/ scan;
// otherwise the convention dir is used.
func discoverCommands(root string, m *PluginManifest) ([]CommandFile, error) {
	paths := []string{filepath.Join(root, "commands")}
	if m != nil && len(strings.TrimSpace(string(m.Commands))) > 0 {
		var err error
		paths, err = resolveComponentPaths(root, m.Commands, "commands")
		if err != nil {
			return nil, err
		}
	}
	var out []CommandFile
	seen := map[string]bool{}
	for _, p := range paths {
		for _, file := range markdownFiles(p) {
			b, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(filepath.Base(file), ".md")
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, CommandFile{Name: name, Content: string(b)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// discoverSkills reads skills/*/SKILL.md. Manifest skills paths are additive to
// the default skills/ directory (Claude's rule); a root SKILL.md is also loaded
// for single-skill plugins that omit a skills/ directory.
func discoverSkills(root string, m *PluginManifest) ([]SkillFile, error) {
	paths := []string{filepath.Join(root, "skills")}
	if m != nil && len(strings.TrimSpace(string(m.Skills))) > 0 {
		extra, err := resolveComponentPaths(root, m.Skills, "skills")
		if err != nil {
			return nil, err
		}
		paths = append(paths, extra...)
	}
	var out []SkillFile
	seen := map[string]bool{}
	add := func(dir string) {
		p := filepath.Join(dir, "SKILL.md")
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		name := filepath.Base(dir)
		if fm := frontmatterValue(string(b), "name"); fm != "" {
			name = fm
		}
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, SkillFile{Name: name, Dir: dir, Content: string(b)})
	}
	for _, dir := range paths {
		// Path can point directly at one skill dir or at a directory of skills.
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
			add(dir)
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}
	// Single-skill plugin layout: root/SKILL.md and no explicit/default skills were
	// found. Do not add a repo-root SKILL.md alongside a real plugin skills/ tree.
	if len(out) == 0 && (m == nil || len(strings.TrimSpace(string(m.Skills))) == 0) {
		add(root)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type rawMCPServer struct {
	Command     json.RawMessage   `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Type        string            `json:"type"`
	URL         string            `json:"url"`
	Description string            `json:"description"`
}

func discoverMCP(root string, m *PluginManifest) ([]MCPServer, error) {
	var data []byte
	var err error
	path := ""
	if m != nil {
		raw := firstRaw(m.MCPServers, m.MCPServersSnake)
		if len(strings.TrimSpace(string(raw))) > 0 {
			paths, err := resolveComponentPaths(root, raw, "mcpServers")
			if err != nil {
				return nil, err
			}
			if len(paths) > 0 {
				path = paths[0]
			} else if isJSONObject(raw) {
				data = raw
			}
		}
	}
	if data == nil {
		if path == "" {
			path, err = safeJoinUnder(root, ".mcp.json", "mcpServers")
			if err != nil {
				return nil, err
			}
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, nil
		}
	}
	servers := parseMCPServers(data)
	var out []MCPServer
	for name, s := range servers {
		cmd, args := parseCommandAndArgs(s.Command, s.Args)
		out = append(out, MCPServer{
			Name: name, Command: cmd, Args: args,
			Env: s.Env, URL: s.URL, Type: s.Type, Description: s.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parseMCPServers(b []byte) map[string]rawMCPServer {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil
	}
	for _, key := range []string{"mcpServers", "mcp_servers"} {
		if raw, ok := top[key]; ok {
			var m map[string]rawMCPServer
			_ = json.Unmarshal(raw, &m)
			return m
		}
	}
	// Codex also accepts a direct server map.
	var direct map[string]rawMCPServer
	_ = json.Unmarshal(b, &direct)
	return direct
}

func parseCommandAndArgs(raw json.RawMessage, args []string) ([]string, []string) {
	var cmd string
	if err := json.Unmarshal(raw, &cmd); err == nil && strings.TrimSpace(cmd) != "" {
		return splitCmd(cmd), args
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[:1], append(arr[1:], args...)
	}
	return nil, args
}

// claudeHooksFile is the Claude hooks shape: { "hooks": { Event: [ {matcher, hooks:[{type,command}]} ] } }.
type claudeHooksFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func discoverHooks(root string, m *PluginManifest) ([]HookSpec, error) {
	var data []byte
	var err error
	path := ""
	if m != nil && len(strings.TrimSpace(string(m.Hooks))) > 0 {
		paths, err := resolveComponentPaths(root, m.Hooks, "hooks")
		if err != nil {
			return nil, err
		}
		if len(paths) > 0 {
			path = paths[0]
		} else if isJSONObject(m.Hooks) {
			data = m.Hooks
		}
	}
	if data == nil {
		if path == "" {
			path, err = safeJoinUnder(root, filepath.Join("hooks", "hooks.json"), "hooks")
			if err != nil {
				return nil, err
			}
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, nil
		}
	}
	var f claudeHooksFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil
	}
	var out []HookSpec
	var events []string
	for ev := range f.Hooks {
		events = append(events, ev)
	}
	sort.Strings(events)
	for _, ev := range events {
		for _, group := range f.Hooks[ev] {
			for _, h := range group.Hooks {
				if h.Type != "" && h.Type != "command" {
					continue // only command hooks map to eigen
				}
				if strings.TrimSpace(h.Command) == "" {
					continue
				}
				out = append(out, HookSpec{Event: mapHookEvent(ev), Command: splitCmd(h.Command)})
			}
		}
	}
	return out, nil
}

func discoverAgents(root string, m *PluginManifest) ([]AgentFile, error) {
	paths := []string{filepath.Join(root, "agents")}
	if m != nil && len(strings.TrimSpace(string(m.Agents))) > 0 {
		var err error
		paths, err = resolveComponentPaths(root, m.Agents, "agents")
		if err != nil {
			return nil, err
		}
	}
	var out []AgentFile
	seen := map[string]bool{}
	for _, p := range paths {
		for _, file := range markdownFiles(p) {
			b, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			body := string(b)
			name := strings.TrimSuffix(filepath.Base(file), ".md")
			if fm := frontmatterValue(body, "name"); fm != "" {
				name = fm
			}
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, AgentFile{Name: name, Path: file, Description: frontmatterValue(body, "description"), Content: body})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func countPathField(root string, m *PluginManifest, field string) int {
	if m == nil || field != "apps" || len(strings.TrimSpace(string(m.Apps))) == 0 {
		return 0
	}
	paths, err := resolveComponentPaths(root, m.Apps, "apps")
	if err != nil {
		return 0
	}
	if len(paths) > 0 {
		n := 0
		for _, p := range paths {
			if info, err := os.Stat(p); err == nil {
				if info.IsDir() {
					files, _ := os.ReadDir(p)
					n += len(files)
				} else {
					n++
				}
			}
		}
		return n
	}
	if isJSONArray(m.Apps) {
		var arr []json.RawMessage
		_ = json.Unmarshal(m.Apps, &arr)
		return len(arr)
	}
	if isJSONObject(m.Apps) {
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(m.Apps, &obj)
		return len(obj)
	}
	return 0
}

func markdownFiles(path string) []string {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	var out []string
	if !info.IsDir() {
		if strings.HasSuffix(strings.ToLower(path), ".md") {
			return []string{path}
		}
		return nil
	}
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func resolveComponentPaths(root string, raw json.RawMessage, what string) ([]string, error) {
	var rels []string
	if s, ok := rawString(raw); ok {
		rels = append(rels, s)
	} else {
		var arr []string
		if err := json.Unmarshal(raw, &arr); err == nil {
			rels = append(rels, arr...)
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, rel := range rels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		p, err := safeJoinUnder(root, rel, what)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

func rawString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	return "", false
}

func firstRaw(raws ...json.RawMessage) json.RawMessage {
	for _, raw := range raws {
		if len(strings.TrimSpace(string(raw))) > 0 {
			return raw
		}
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "{")
}

func isJSONArray(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "[")
}

func frontmatterValue(md, key string) string {
	lines := strings.Split(md, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	prefix := key + ":"
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), `"'`)
		}
	}
	return ""
}

// mapHookEvent maps Claude hook event names to eigen's. Unknown events pass
// through lowercased so an installer can still record them (and the loader will
// simply never fire an unrecognized event).
func mapHookEvent(claude string) string {
	switch claude {
	case "PreToolUse":
		return "tool_start"
	case "PostToolUse":
		return "tool_result"
	case "SessionStart":
		return "session_start"
	case "SessionEnd", "Stop":
		return "session_stop"
	case "Stop_resume", "SessionResume":
		return "session_resume"
	default:
		return strings.ToLower(claude)
	}
}

// splitCmd turns a shell command string into eigen's []string command form. A
// command with shell metacharacters is wrapped in `sh -c` so pipes/redirects
// work; a simple command is split on whitespace.
func splitCmd(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	if strings.ContainsAny(cmd, "|&;<>$`(){}*?") {
		return []string{"sh", "-c", cmd}
	}
	return strings.Fields(cmd)
}
