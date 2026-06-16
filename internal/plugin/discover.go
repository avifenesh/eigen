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
	Commands   []CommandFile   // commands/*.md (Claude slash commands) — wired in Tier 31
	Agents     int             // count only (not wired)
}

// CommandFile is one slash-command markdown file from a plugin's commands/ dir.
type CommandFile struct {
	Name    string // file basename sans .md
	Content string // raw markdown (frontmatter + body)
}

// SkillFile is one discovered skill: its directory name and the raw SKILL.md.
type SkillFile struct {
	Name    string // skill dir name
	Content string // raw SKILL.md (frontmatter + body)
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

	// Manifest (optional under lenient mode).
	if b, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil {
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

	c.Skills = discoverSkills(root)
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
	c.Commands = discoverCommands(root)
	c.Agents = countDir(root, c.Manifest, "agents", ".md")
	return c, nil
}

// discoverCommands reads commands/*.md (Claude slash commands). Subdir
// namespacing (commands/git/commit.md → git:commit) is flattened to the file
// basename for v1.
func discoverCommands(root string) []CommandFile {
	dir := filepath.Join(root, "commands")
	matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(matches)
	var out []CommandFile
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		out = append(out, CommandFile{Name: name, Content: string(b)})
	}
	return out
}

// discoverSkills reads skills/*/SKILL.md (the convention; no manifest override).
func discoverSkills(root string) []SkillFile {
	dir := filepath.Join(root, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []SkillFile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name(), "SKILL.md")
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, SkillFile{Name: e.Name(), Content: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// claudeMCPFile is the Claude `.mcp.json` shape: { "mcpServers": { name: {...} } }.
type claudeMCPFile struct {
	MCPServers map[string]struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Type    string            `json:"type"`
		URL     string            `json:"url"`
	} `json:"mcpServers"`
}

func discoverMCP(root string, m *PluginManifest) ([]MCPServer, error) {
	path, err := safeJoinUnder(root, ".mcp.json", "mcpServers")
	if err != nil {
		return nil, err
	}
	if m != nil && strings.TrimSpace(m.MCPServers) != "" {
		path, err = safeJoinUnder(root, m.MCPServers, "mcpServers")
		if err != nil {
			return nil, err
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var f claudeMCPFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil
	}
	var out []MCPServer
	for name, s := range f.MCPServers {
		out = append(out, MCPServer{
			Name: name, Command: splitCmd(s.Command), Args: s.Args,
			Env: s.Env, URL: s.URL, Type: s.Type,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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
	path, err := safeJoinUnder(root, filepath.Join("hooks", "hooks.json"), "hooks")
	if err != nil {
		return nil, err
	}
	if m != nil && strings.TrimSpace(m.Hooks) != "" {
		path, err = safeJoinUnder(root, m.Hooks, "hooks")
		if err != nil {
			return nil, err
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var f claudeHooksFile
	if err := json.Unmarshal(b, &f); err != nil {
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

func countDir(root string, m *PluginManifest, dir, ext string) int {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			n++
		}
	}
	return n
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
