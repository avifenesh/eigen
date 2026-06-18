// Package plugin implements eigen's plugin + marketplace layer (Tier 27): a
// MARKETPLACE is a catalog repo listing many plugins; a PLUGIN is a bundle of
// components (skills, agents, slash commands, MCP servers, hooks, and sometimes
// Codex app integrations). eigen reads Claude and Codex on-disk formats directly
// (`.claude-plugin/*`, `.agents/plugins/marketplace.json`, `.codex-plugin/*`)
// so existing marketplaces work without re-authoring — consume + manage,
// mirroring how `eigen skill add` consumes a skill without authoring one.
package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Marketplace is the parsed `.claude-plugin/marketplace.json`: a catalog of
// plugins. Only the fields eigen uses are kept; unknown fields are ignored.
type Marketplace struct {
	Name      string          `json:"name"`
	Owner     Owner           `json:"owner"`
	Metadata  MarketMeta      `json:"metadata"`
	Interface PluginInterface `json:"interface,omitempty"` // Codex install-surface metadata
	Plugins   []PluginEntry   `json:"plugins"`
}

// PluginInterface is the Codex-facing presentation metadata also seen in
// .codex-plugin/plugin.json. Eigen uses the text fields for marketplace cards;
// unknown visual fields are ignored.
type PluginInterface struct {
	DisplayName      string   `json:"displayName,omitempty"`
	ShortDescription string   `json:"shortDescription,omitempty"`
	LongDescription  string   `json:"longDescription,omitempty"`
	DeveloperName    string   `json:"developerName,omitempty"`
	Category         string   `json:"category,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"`
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
	Name        string          `json:"name"`
	Source      Source          `json:"source"`
	Description string          `json:"description,omitempty"`
	Version     string          `json:"version,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Repository  string          `json:"repository,omitempty"`
	License     string          `json:"license,omitempty"`
	Keywords    []string        `json:"keywords,omitempty"`
	Category    string          `json:"category,omitempty"`
	Interface   PluginInterface `json:"interface,omitempty"`
	Strict      *bool           `json:"strict,omitempty"` // nil = default (true); false tolerates missing components
}

// strictMode reports whether the entry enforces strict validation (the default).
func (e PluginEntry) strictMode() bool { return e.Strict == nil || *e.Strict }

// Source tells the client where to fetch a plugin's files. The Claude format is
// polymorphic: a bare string (relative path within the marketplace repo) OR an
// object with {source: local|git|github, ...}. We normalize both into this one
// struct via UnmarshalJSON.
type Source struct {
	// Kind is "" (relative path within the marketplace repo), "local", "git",
	// "github", "url" (git URL at repo root), or "git-subdir" (Codex). A
	// bare-string source yields Kind="" with Path set.
	Kind   string
	Path   string // relative plugin path for local/git-subdir sources
	Repo   string // owner/repo or full Git URL
	Ref    string // branch/tag, optional
	Commit string // pinned commit/sha, optional and preferred over Ref
}

// UnmarshalJSON handles the string-shorthand and object forms of `source`.
func (s *Source) UnmarshalJSON(b []byte) error {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	// String shorthand: usually a relative path within the marketplace repo. Some
	// community catalogs use a bare GitHub URL string; treat that like a url source.
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		if looksLikeGitURL(str) {
			s.Kind = "url"
			s.Repo = str
		} else {
			s.Path = str
		}
		return nil
	}
	// Object form.
	var obj struct {
		Source string `json:"source"`
		Repo   string `json:"repo"`
		URL    string `json:"url"`
		Ref    string `json:"ref"`
		Commit string `json:"commit"`
		SHA    string `json:"sha"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	s.Ref = obj.Ref
	s.Commit = firstNonEmpty(obj.Commit, obj.SHA)
	switch obj.Source {
	case "git", "github", "url", "git-subdir":
		s.Kind = obj.Source
		s.Repo = firstNonEmpty(obj.Repo, obj.URL)
		s.Path = obj.Path
	case "local":
		s.Kind = "local"
		s.Path = obj.Path
	default:
		// {"source":"./path"} local form, or any other string treated as path.
		s.Path = firstNonEmpty(obj.Path, obj.Source)
	}
	return nil
}

// IsLocal reports whether the plugin lives inside the marketplace repo (a
// relative path), vs an external git/github repo.
func (s Source) IsLocal() bool { return s.Kind == "" || s.Kind == "local" }

// EffectiveRef returns the pinned commit/sha when present, else the branch/tag.
func (s Source) EffectiveRef() string { return firstNonEmpty(s.Commit, s.Ref) }

// PluginManifest is the parsed `.claude-plugin/plugin.json`. All component
// fields are optional — components are discovered by convention (directory
// layout); the manifest only adds non-default paths.
type PluginManifest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"displayName,omitempty"` // Claude v2.1.143+
	Version     string          `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Repository  string          `json:"repository,omitempty"`
	License     string          `json:"license,omitempty"`
	Keywords    []string        `json:"keywords,omitempty"`
	Interface   PluginInterface `json:"interface,omitempty"` // Codex presentation metadata

	// Component paths may be a string, an array, or (for MCP/hooks) an inline
	// object in Claude/Codex manifests. We keep the raw JSON and parse the path
	// forms leniently in discovery.
	Skills          json.RawMessage `json:"skills,omitempty"`
	Commands        json.RawMessage `json:"commands,omitempty"`
	Agents          json.RawMessage `json:"agents,omitempty"`
	Hooks           json.RawMessage `json:"hooks,omitempty"`       // default hooks/hooks.json
	MCPServers      json.RawMessage `json:"mcpServers,omitempty"`  // default .mcp.json
	MCPServersSnake json.RawMessage `json:"mcp_servers,omitempty"` // Codex legacy/wrapped spelling
	Apps            json.RawMessage `json:"apps,omitempty"`        // Codex app integrations (wired as MCP servers when parseable)
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
	for i := range m.Plugins {
		p := &m.Plugins[i]
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("marketplace.json: plugin #%d has no name", i)
		}
		if p.Description == "" {
			p.Description = firstNonEmpty(p.Interface.ShortDescription, p.Interface.LongDescription)
		}
		if p.Category == "" {
			p.Category = p.Interface.Category
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

func looksLikeGitURL(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.HasPrefix(s, "https://github.com/") || strings.HasSuffix(s, ".git")
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
