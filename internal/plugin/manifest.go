// Package plugin implements eigen's plugin + marketplace layer (Tier 27): a
// MARKETPLACE is a catalog repo listing many plugins; a PLUGIN is a bundle of
// components (skills + an MCP server + hooks). eigen reads the Claude Code
// on-disk format directly (`.claude-plugin/marketplace.json`,
// `.claude-plugin/plugin.json` + convention dirs) so a user's existing
// marketplaces work without re-authoring — consume + manage, mirroring how
// `eigen skill add` consumes a skill without authoring one.
//
// v1 wires the three component types eigen already runs natively — skills, MCP
// servers, hooks. Claude "commands" and "agents" (markdown slash-prompts /
// subagents) are parsed-but-not-wired yet (eigen has no slash-command-prompt
// subsystem); they're reported so a later version can add them.
package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Marketplace is the parsed `.claude-plugin/marketplace.json`: a catalog of
// plugins. Only the fields eigen uses are kept; unknown fields are ignored.
type Marketplace struct {
	Name     string        `json:"name"`
	Owner    Owner         `json:"owner"`
	Metadata MarketMeta    `json:"metadata"`
	Plugins  []PluginEntry `json:"plugins"`
}

// Owner identifies who maintains a marketplace (name is the field eigen shows).
type Owner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MarketMeta is the marketplace's own description/version.
type MarketMeta struct {
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
}

// PluginEntry is one plugin listed in a marketplace catalog.
type PluginEntry struct {
	Name        string   `json:"name"`
	Source      Source   `json:"source"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Category    string   `json:"category,omitempty"`
	Strict      *bool    `json:"strict,omitempty"` // nil = default (true); false tolerates missing components
}

// strictMode reports whether the entry enforces strict validation (the default).
func (e PluginEntry) strictMode() bool { return e.Strict == nil || *e.Strict }

// Source tells the client where to fetch a plugin's files. The Claude format is
// polymorphic: a bare string (relative path within the marketplace repo) OR an
// object with {source: local|git|github, ...}. We normalize both into this one
// struct via UnmarshalJSON.
type Source struct {
	// Kind is "" (relative path within the marketplace repo), "git", or
	// "github". A bare-string source yields Kind="" with Path set.
	Kind string
	Path string // relative path (Kind=="") or object "source" path for local
	Repo string // owner/repo (github) or full URL (git)
	Ref  string // branch/tag/commit (git/github), optional
}

// UnmarshalJSON handles the string-shorthand and object forms of `source`.
func (s *Source) UnmarshalJSON(b []byte) error {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	// String shorthand: a relative path within the marketplace repo.
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		s.Path = str
		return nil
	}
	// Object form.
	var obj struct {
		Source string `json:"source"`
		Repo   string `json:"repo"`
		Ref    string `json:"ref"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	switch obj.Source {
	case "git":
		s.Kind, s.Repo, s.Ref = "git", obj.Repo, obj.Ref
	case "github":
		s.Kind, s.Repo, s.Ref = "github", obj.Repo, obj.Ref
	default:
		// {"source": "./path"} local form, or any other string treated as path.
		s.Path = firstNonEmpty(obj.Path, obj.Source)
	}
	return nil
}

// IsLocal reports whether the plugin lives inside the marketplace repo (a
// relative path), vs an external git/github repo.
func (s Source) IsLocal() bool { return s.Kind == "" || s.Kind == "local" }

// PluginManifest is the parsed `.claude-plugin/plugin.json`. All component
// fields are optional — components are discovered by convention (directory
// layout); the manifest only adds non-default paths.
type PluginManifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`

	// Component-path overrides (additive to the convention dirs). commands/
	// agents may be a string or an array — kept raw and parsed leniently.
	Commands   json.RawMessage `json:"commands,omitempty"`
	Agents     json.RawMessage `json:"agents,omitempty"`
	Hooks      string          `json:"hooks,omitempty"`      // default hooks/hooks.json
	MCPServers string          `json:"mcpServers,omitempty"` // default .mcp.json
}

// ParseMarketplace parses a marketplace.json byte slice, validating the minimum
// required shape.
func ParseMarketplace(b []byte) (*Marketplace, error) {
	var m Marketplace
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("marketplace.json: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("marketplace.json: missing \"name\"")
	}
	for i, p := range m.Plugins {
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("marketplace.json: plugin #%d has no name", i)
		}
	}
	return &m, nil
}

// Find returns the plugin entry with the given name (case-insensitive), or
// false. Used by `plugin install <name>`.
func (m *Marketplace) Find(name string) (PluginEntry, bool) {
	for _, p := range m.Plugins {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return PluginEntry{}, false
}

// ParsePluginManifest parses a plugin.json byte slice. A missing/empty name is
// an error (the install reference depends on it).
func ParsePluginManifest(b []byte) (*PluginManifest, error) {
	var p PluginManifest
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, fmt.Errorf("plugin.json: missing \"name\"")
	}
	return &p, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
