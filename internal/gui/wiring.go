package gui

import (
	"strings"

	"github.com/avifenesh/eigen/internal/mcp"
)

// Wiring bridge layer — the full mcp.json server editor (stdio AND remote),
// so MCP servers can be added/edited/enabled/removed from the GUI instead of
// hand-editing JSON. Connectors (remote + OAuth) get their own richer surface
// (connectors.go); this is the general server list, including local stdio
// servers, used by the "Wiring" management view.

// MCPServerDTO is one mcp.json server for the wiring editor.
type MCPServerDTO struct {
	Name         string   `json:"name"`
	Command      []string `json:"command,omitempty"`
	URL          string   `json:"url,omitempty"`
	Type         string   `json:"type,omitempty"`
	Description  string   `json:"description,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	ExcludeTools []string `json:"excludeTools,omitempty"`
	Disabled     bool     `json:"disabled"`
	Remote       bool     `json:"remote"`
	// Env is rendered as KEY=VALUE lines in the editor (kept simple for the form).
	EnvPairs []string `json:"envPairs,omitempty"`
	// SecretEnvKeys names env vars stored in the OS keychain (values never leave
	// it). Read-back carries only the names; shown as "•••• (secret)" rows.
	SecretEnvKeys []string `json:"secretEnvKeys,omitempty"`
	// SecretEnvPairs is WRITE-ONLY input (KEY=VALUE lines) → stored in the
	// keychain on save. Always empty on read-back.
	SecretEnvPairs []string `json:"secretEnvPairs,omitempty"`
}

// MCPServersDTO is the wiring snapshot.
type MCPServersDTO struct {
	Servers []MCPServerDTO `json:"servers"`
}

func entryToDTO(e mcp.ServerEntry) MCPServerDTO {
	var env []string
	for k, v := range e.Env {
		env = append(env, k+"="+v)
	}
	return MCPServerDTO{
		Name:          e.Name,
		Command:       e.Command,
		URL:           e.URL,
		Type:          e.Type,
		Description:   e.Description,
		Tools:         e.Tools,
		ExcludeTools:  e.ExcludeTools,
		Disabled:      e.Disabled,
		Remote:        e.Remote,
		EnvPairs:      env,
		SecretEnvKeys: e.SecretEnvKeys,
	}
}

func dtoToEntry(d MCPServerDTO) mcp.ServerEntry {
	env := parsePairs(d.EnvPairs)
	secrets := parsePairs(d.SecretEnvPairs)
	return mcp.ServerEntry{
		Name:          strings.TrimSpace(d.Name),
		Command:       d.Command,
		URL:           strings.TrimSpace(d.URL),
		Type:          strings.TrimSpace(d.Type),
		Env:           env,
		Description:   d.Description,
		Tools:         d.Tools,
		ExcludeTools:  d.ExcludeTools,
		Disabled:      d.Disabled,
		SecretEnvKeys: d.SecretEnvKeys,
		SecretEnv:     secrets,
	}
}

// parsePairs turns KEY=VALUE lines into a map (nil when empty).
func parsePairs(lines []string) map[string]string {
	m := map[string]string{}
	for _, p := range lines {
		if k, v, ok := strings.Cut(p, "="); ok {
			if k = strings.TrimSpace(k); k != "" {
				m[k] = v
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// MCPServers lists every configured MCP server (stdio + remote) for the editor.
func (b *Bridge) MCPServers() (*MCPServersDTO, error) {
	servers, err := mcp.ListServers(mcp.UserConfigPath())
	if err != nil {
		return nil, err
	}
	out := make([]MCPServerDTO, 0, len(servers))
	for _, s := range servers {
		out = append(out, entryToDTO(s))
	}
	return &MCPServersDTO{Servers: out}, nil
}

// SaveMCPServer adds or replaces (by name) one server, validating it.
func (b *Bridge) SaveMCPServer(d MCPServerDTO) error {
	return mcp.SaveServer(mcp.UserConfigPath(), dtoToEntry(d))
}

// RemoveMCPServer deletes a server by name.
func (b *Bridge) RemoveMCPServer(name string) (bool, error) {
	return mcp.RemoveServer(mcp.UserConfigPath(), strings.TrimSpace(name))
}

// SetMCPServerDisabled toggles a server's disabled flag.
func (b *Bridge) SetMCPServerDisabled(name string, disabled bool) (bool, error) {
	return mcp.SetServerDisabled(mcp.UserConfigPath(), strings.TrimSpace(name), disabled)
}

// MCPSecretsAvailable reports whether the OS keychain can store MCP env secrets.
// The editor gates its "store as secret" affordance on this — without a keyring
// it keeps env values as plaintext rather than imply a security guarantee.
func (b *Bridge) MCPSecretsAvailable() bool {
	return mcp.SecretsAvailable()
}
