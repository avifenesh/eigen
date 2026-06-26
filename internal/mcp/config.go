package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config CRUD for mcp.json — the typed editor the GUI/CLI use to add, edit,
// enable/disable, and remove MCP servers (stdio AND remote connectors) without
// hand-editing JSON. The plugin layer manipulates the same file as raw maps for
// bundle wiring; this is the user-facing, schema-aware path.

// ServerEntry is the public view of one mcp.json server, for editor UIs.
type ServerEntry struct {
	Name         string            `json:"name"`
	Command      []string          `json:"command,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	URL          string            `json:"url,omitempty"`
	Type         string            `json:"type,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Description  string            `json:"description,omitempty"`
	Tools        []string          `json:"tools,omitempty"`
	ExcludeTools []string          `json:"excludeTools,omitempty"`
	Disabled     bool              `json:"disabled"`
	// Remote is true when this entry is a remote (HTTP) server rather than stdio.
	Remote bool `json:"remote"`
	// SecretEnvKeys names env vars whose VALUES are kept in the OS keychain, not
	// in mcp.json. Read-back only carries the key NAMES (values never leave the
	// keychain).
	SecretEnvKeys []string `json:"secretEnvKeys,omitempty"`
	// SecretEnv is WRITE-ONLY input on SaveServer: key→value pairs to store in the
	// keychain (their keys are recorded in SecretEnvKeys, values stripped from the
	// file). Always empty on read-back.
	SecretEnv map[string]string `json:"secretEnv,omitempty"`
}

func (sc serverConfig) toEntry() ServerEntry {
	return ServerEntry{
		Name:          sc.Name,
		Command:       sc.Command,
		Env:           sc.Env,
		URL:           sc.URL,
		Type:          sc.Type,
		Headers:       sc.Headers,
		Description:   sc.Description,
		Tools:         sc.Tools,
		ExcludeTools:  sc.ExcludeTools,
		Disabled:      sc.Disabled,
		Remote:        isRemoteServer(sc),
		SecretEnvKeys: sc.SecretEnvKeys,
	}
}

func (e ServerEntry) toConfig() serverConfig {
	return serverConfig{
		Name:          strings.TrimSpace(e.Name),
		Command:       e.Command,
		Env:           e.Env,
		URL:           strings.TrimSpace(e.URL),
		Type:          strings.TrimSpace(e.Type),
		Headers:       e.Headers,
		Description:   e.Description,
		Tools:         e.Tools,
		ExcludeTools:  e.ExcludeTools,
		Disabled:      e.Disabled,
		SecretEnvKeys: e.SecretEnvKeys,
	}
}

// UserConfigPath returns the per-user mcp.json (~/.eigen/mcp.json). The GUI +
// connector layer edit this user-scoped file; project-local .eigen/mcp.json (a
// CLI cwd concern) is not editable from the desktop app.
func UserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "mcp.json"
	}
	return filepath.Join(home, ".eigen", "mcp.json")
}

// readConfig loads mcp.json (missing file → empty config, no error).
func readConfig(path string) (mcpConfig, error) {
	var cfg mcpConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// writeConfig persists mcp.json (0600, atomic via temp+rename), creating the dir.
func writeConfig(path string, cfg mcpConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ListServers returns every configured server (the file's own entries, NOT the
// auto-detected built-ins), in stable name order, as editor views.
func ListServers(path string) ([]ServerEntry, error) {
	cfg, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	out := make([]ServerEntry, 0, len(cfg.Servers))
	for _, sc := range cfg.Servers {
		out = append(out, sc.toEntry())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// validateEntry checks an entry is a well-formed stdio or remote server.
func validateEntry(e ServerEntry) error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("server name is required")
	}
	hasURL := strings.TrimSpace(e.URL) != ""
	hasCmd := len(e.Command) > 0
	if hasURL == hasCmd {
		if hasURL {
			return fmt.Errorf("server %q: set EITHER a remote url OR a stdio command, not both", e.Name)
		}
		return fmt.Errorf("server %q: needs a remote url or a stdio command", e.Name)
	}
	return nil
}

// SaveServer adds or replaces (by name, case-insensitive) one server entry,
// validating it first. Any SecretEnv values are written to the OS keychain (not
// the file) and their key names recorded in secret_env_keys; the merged set of
// secret keys is preserved so re-saving without re-supplying a value keeps it.
func SaveServer(path string, e ServerEntry) error {
	if err := validateEntry(e); err != nil {
		return err
	}
	cfg, err := readConfig(path)
	if err != nil {
		return err
	}
	sc := e.toConfig()

	// Route secret env to the keychain. Merge new values over any already stored
	// for this server (so editing one secret doesn't wipe the others), and union
	// the recorded key names.
	if len(e.SecretEnv) > 0 {
		stored := serverSecrets(sc.Name)
		if stored == nil {
			stored = map[string]string{}
		}
		keyset := map[string]bool{}
		for _, k := range sc.SecretEnvKeys {
			keyset[k] = true
		}
		for k, v := range e.SecretEnv {
			stored[k] = v
			keyset[k] = true
			// A secret key must never also live in the plaintext env.
			delete(sc.Env, k)
		}
		if len(sc.Env) == 0 {
			sc.Env = nil
		}
		if err := setServerSecrets(sc.Name, stored); err != nil {
			return fmt.Errorf("store secret env for %q in keychain: %w", sc.Name, err)
		}
		sc.SecretEnvKeys = sortedKeys(keyset)
	}

	replaced := false
	for i, ex := range cfg.Servers {
		if strings.EqualFold(ex.Name, sc.Name) {
			cfg.Servers[i] = sc
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Servers = append(cfg.Servers, sc)
	}
	return writeConfig(path, cfg)
}

// sortedKeys returns the map keys sorted (stable secret_env_keys ordering).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RemoveServer deletes a server by name. Returns whether one was removed.
func RemoveServer(path, name string) (bool, error) {
	cfg, err := readConfig(path)
	if err != nil {
		return false, err
	}
	kept := cfg.Servers[:0]
	removed := false
	for _, sc := range cfg.Servers {
		if strings.EqualFold(sc.Name, name) {
			removed = true
			continue
		}
		kept = append(kept, sc)
	}
	if !removed {
		return false, nil
	}
	cfg.Servers = kept
	// Drop any keychain-stored secrets for this server (no orphaned credentials).
	deleteServerSecrets(name)
	return true, writeConfig(path, cfg)
}

// SetServerDisabled toggles a server's disabled flag. Returns whether the server
// was found.
func SetServerDisabled(path, name string, disabled bool) (bool, error) {
	cfg, err := readConfig(path)
	if err != nil {
		return false, err
	}
	found := false
	for i := range cfg.Servers {
		if strings.EqualFold(cfg.Servers[i].Name, name) {
			cfg.Servers[i].Disabled = disabled
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	return true, writeConfig(path, cfg)
}
