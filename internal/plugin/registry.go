package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Registry tracks added marketplaces and installed plugins under ~/.eigen
// (instance-aware: ~/.eigen-<instance> is NOT used — marketplaces/plugins are
// shared, like skills + config). Two JSON files:
//
//	marketplaces.json     — catalogs the user added
//	plugins-installed.json — what's installed + which files it wrote (for clean removal)
type Registry struct {
	dir string // ~/.eigen
}

// MarketRecord is one added marketplace.
type MarketRecord struct {
	Name    string    `json:"name"`   // marketplace name (from its manifest)
	Source  string    `json:"source"` // owner/repo[@ref] the user added
	Owner   string    `json:"owner,omitempty"`
	Added   time.Time `json:"added"`
	Updated time.Time `json:"updated,omitempty"`
}

// InstalledPlugin records one installed plugin and the files it wrote, so
// `plugin remove` can cleanly reverse it.
type InstalledPlugin struct {
	Name        string    `json:"name"`
	Marketplace string    `json:"marketplace,omitempty"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Installed   time.Time `json:"installed"`
	Root        string    `json:"root"`             // ~/.eigen/plugins/<name> (bundled files)
	Skills      []string  `json:"skills,omitempty"` // installed skill dir names (~/.eigen/skills/<n>)
	MCPServers  []string  `json:"mcp_servers,omitempty"`
	Hooks       int       `json:"hooks,omitempty"` // count appended to hooks.json
}

// NewRegistry opens the registry rooted at ~/.eigen.
func NewRegistry() (*Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Registry{dir: filepath.Join(home, ".eigen")}, nil
}

// NewRegistryAt opens a registry rooted at dir (tests).
func NewRegistryAt(dir string) *Registry { return &Registry{dir: dir} }

func (r *Registry) marketsPath() string { return filepath.Join(r.dir, "marketplaces.json") }
func (r *Registry) pluginsPath() string { return filepath.Join(r.dir, "plugins-installed.json") }

// PluginsDir is where plugin bundles are cached: ~/.eigen/plugins/<name>.
func (r *Registry) PluginsDir() string { return filepath.Join(r.dir, "plugins") }

// SkillsDir / MCPPath / HooksPath are where components are wired (global scope).
func (r *Registry) SkillsDir() string { return filepath.Join(r.dir, "skills") }
func (r *Registry) MCPPath() string   { return filepath.Join(r.dir, "mcp.json") }
func (r *Registry) HooksPath() string { return filepath.Join(r.dir, "hooks.json") }

// --- marketplaces ---

// Markets returns the added marketplaces (sorted by name).
func (r *Registry) Markets() ([]MarketRecord, error) {
	var list []MarketRecord
	if err := readJSON(r.marketsPath(), &list); err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// AddMarket records (or updates) a marketplace.
func (r *Registry) AddMarket(rec MarketRecord) error {
	list, err := r.Markets()
	if err != nil {
		return err
	}
	for i, m := range list {
		if strings.EqualFold(m.Name, rec.Name) {
			rec.Added = m.Added
			rec.Updated = time.Now()
			list[i] = rec
			return writeJSON(r.marketsPath(), list)
		}
	}
	if rec.Added.IsZero() {
		rec.Added = time.Now()
	}
	list = append(list, rec)
	return writeJSON(r.marketsPath(), list)
}

// MarketByName returns an added marketplace by name (case-insensitive).
func (r *Registry) MarketByName(name string) (MarketRecord, bool) {
	list, _ := r.Markets()
	for _, m := range list {
		if strings.EqualFold(m.Name, name) {
			return m, true
		}
	}
	return MarketRecord{}, false
}

// RemoveMarket drops a marketplace by name. Returns false if it wasn't present.
func (r *Registry) RemoveMarket(name string) (bool, error) {
	list, err := r.Markets()
	if err != nil {
		return false, err
	}
	out := list[:0]
	found := false
	for _, m := range list {
		if strings.EqualFold(m.Name, name) {
			found = true
			continue
		}
		out = append(out, m)
	}
	if !found {
		return false, nil
	}
	return true, writeJSON(r.marketsPath(), out)
}

// --- installed plugins ---

// Installed returns installed plugins (sorted by name).
func (r *Registry) Installed() ([]InstalledPlugin, error) {
	var list []InstalledPlugin
	if err := readJSON(r.pluginsPath(), &list); err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// InstalledByName returns an installed plugin record by name.
func (r *Registry) InstalledByName(name string) (InstalledPlugin, bool) {
	list, _ := r.Installed()
	for _, p := range list {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return InstalledPlugin{}, false
}

// RecordInstall adds (or replaces) an installed-plugin record.
func (r *Registry) RecordInstall(p InstalledPlugin) error {
	list, err := r.Installed()
	if err != nil {
		return err
	}
	out := make([]InstalledPlugin, 0, len(list)+1)
	for _, e := range list {
		if !strings.EqualFold(e.Name, p.Name) {
			out = append(out, e)
		}
	}
	if p.Installed.IsZero() {
		p.Installed = time.Now()
	}
	out = append(out, p)
	return writeJSON(r.pluginsPath(), out)
}

// RemoveInstall drops an installed-plugin record by name.
func (r *Registry) RemoveInstall(name string) (bool, error) {
	list, err := r.Installed()
	if err != nil {
		return false, err
	}
	out := list[:0]
	found := false
	for _, e := range list {
		if strings.EqualFold(e.Name, name) {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return false, nil
	}
	return true, writeJSON(r.pluginsPath(), out)
}

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// SafeName validates a plugin/marketplace name is filesystem-safe.
func SafeName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid name %q (letters/digits/._- , ≤64 chars)", name)
	}
	return nil
}

// readJSON reads a JSON file into v; a missing file is not an error (v is left
// as its zero value).
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes v as pretty JSON atomically (tmp + rename).
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
