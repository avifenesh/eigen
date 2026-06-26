package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/connector"
	"github.com/avifenesh/eigen/internal/mcp"
)

// Connectors bridge layer — the desktop "superapp" integrations surface. A
// connector is a REMOTE MCP server (Streamable HTTP) the user authorizes via
// OAuth: Google Workspace, Slack, Notion, Linear, etc. This bridge lists them
// with live connection status, adds one (write the mcp.json entry + run the
// OAuth flow), (re)connects, and disconnects. It also exposes the full mcp.json
// server editor (stdio + remote) so the wiring no longer requires hand-editing
// JSON.
//
// The OAuth flow opens the user's browser and can take a while, so Connect runs
// in the background and the result lands on the "eigen:connector" event; the
// frontend refreshes its list when it fires.

// ConnectorDTO is one connector's editor + status row.
type ConnectorDTO struct {
	Name        string `json:"name"`
	Display     string `json:"display"` // friendly label from the directory, else the name
	Glyph       string `json:"glyph"`   // directory tile glyph (empty for custom URLs)
	URL         string `json:"url"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
	// Connected reflects OAuth state: true when a (non-expired-able) token is
	// stored. RequiresAuth is true when the server is remote (so a Connect button
	// makes sense); a remote server with no token shows "not connected".
	Connected    bool   `json:"connected"`
	RequiresAuth bool   `json:"requiresAuth"`
	Expiry       string `json:"expiry,omitempty"` // RFC3339, when the token expires
}

// CatalogEntryDTO is one curated directory connector (the browse-and-connect grid).
type CatalogEntryDTO struct {
	Name        string `json:"name"`
	Display     string `json:"display"`
	Glyph       string `json:"glyph"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Added       bool   `json:"added"` // already in mcp.json (so the grid shows "added")
}

// ConnectorsDTO is the connectors snapshot: configured connectors + the curated
// directory of ones the user could add.
type ConnectorsDTO struct {
	Connectors []ConnectorDTO    `json:"connectors"`
	Directory  []CatalogEntryDTO `json:"directory"`
}

// connectorEventDTO is emitted on "eigen:connector" when a background OAuth flow
// finishes (success or failure), so the UI can refresh + toast.
type connectorEventDTO struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Connectors lists remote MCP servers (connectors) from mcp.json joined with
// their OAuth connection status.
func (b *Bridge) Connectors() (*ConnectorsDTO, error) {
	servers, err := mcp.ListServers(mcp.UserConfigPath())
	if err != nil {
		return nil, err
	}
	mgr := connector.Default()
	configured := map[string]bool{}
	out := make([]ConnectorDTO, 0)
	for _, s := range servers {
		configured[strings.ToLower(s.Name)] = true
		if !s.Remote {
			continue // connectors are the remote servers; stdio lives in the wiring editor
		}
		dto := ConnectorDTO{
			Name:         s.Name,
			Display:      s.Name,
			URL:          s.URL,
			Type:         s.Type,
			Description:  s.Description,
			Disabled:     s.Disabled,
			RequiresAuth: true,
			Connected:    mgr.IsConnected(s.Name),
		}
		// Enrich from the curated directory (display label + tile glyph) when known.
		if cat, ok := connector.CatalogByName(s.Name); ok {
			dto.Display = cat.Display
			dto.Glyph = cat.Glyph
			if dto.Description == "" {
				dto.Description = cat.Description
			}
		}
		out = append(out, dto)
	}
	// Fold in expiry from manager statuses (keyed by name).
	if sts, err := mgr.Statuses(); err == nil {
		byName := map[string]connector.Status{}
		for _, st := range sts {
			byName[st.Name] = st
		}
		for i := range out {
			if st, ok := byName[out[i].Name]; ok && !st.Expiry.IsZero() {
				out[i].Expiry = st.Expiry.Format(time.RFC3339)
			}
		}
	}
	// Curated directory — mark entries already configured so the grid shows "added".
	dir := make([]CatalogEntryDTO, 0)
	for _, e := range connector.Directory() {
		dir = append(dir, CatalogEntryDTO{
			Name:        e.Name,
			Display:     e.Display,
			Glyph:       e.Glyph,
			URL:         e.URL,
			Description: e.Description,
			Category:    e.Category,
			Added:       configured[strings.ToLower(e.Name)],
		})
	}
	return &ConnectorsDTO{Connectors: out, Directory: dir}, nil
}

// AddCatalogConnector adds a curated directory connector by name (one click —
// the URL comes from the catalog), then starts its OAuth flow.
func (b *Bridge) AddCatalogConnector(name string) error {
	cat, ok := connector.CatalogByName(strings.TrimSpace(name))
	if !ok {
		return fmt.Errorf("unknown directory connector %q", name)
	}
	return b.AddConnector(cat.Name, cat.URL, cat.Description)
}

// AddConnector records a remote MCP server in mcp.json and starts the OAuth
// flow for it in the background (opens the browser). The result lands on the
// "eigen:connector" event. Returns once the entry is persisted + the flow is
// kicked off; the UI should show "connecting…" until the event arrives.
func (b *Bridge) AddConnector(name, url, description string) error {
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if name == "" || url == "" {
		return fmt.Errorf("connector needs a name and a remote URL")
	}
	entry := mcp.ServerEntry{
		Name:        name,
		URL:         url,
		Type:        "http",
		Description: strings.TrimSpace(description),
	}
	if err := mcp.SaveServer(mcp.UserConfigPath(), entry); err != nil {
		return err
	}
	b.startConnect(name, url)
	return nil
}

// ConnectConnector (re)runs the OAuth flow for an already-listed connector.
func (b *Bridge) ConnectConnector(name string) error {
	name = strings.TrimSpace(name)
	servers, err := mcp.ListServers(mcp.UserConfigPath())
	if err != nil {
		return err
	}
	for _, s := range servers {
		if strings.EqualFold(s.Name, name) && s.Remote {
			b.startConnect(s.Name, s.URL)
			return nil
		}
	}
	return fmt.Errorf("no remote connector named %q", name)
}

// startConnect runs the (slow, browser-opening) OAuth flow off the bound call so
// the UI isn't blocked; emits the outcome on "eigen:connector".
func (b *Bridge) startConnect(name, url string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		err := connector.Default().Connect(ctx, name, url, "")
		ev := connectorEventDTO{Name: name, OK: err == nil}
		if err != nil {
			ev.Error = err.Error()
		}
		b.emit("eigen:connector", ev)
	}()
}

// DisconnectConnector revokes the stored token for a connector (the mcp.json
// entry stays, so it can be reconnected).
func (b *Bridge) DisconnectConnector(name string) error {
	return connector.Default().Disconnect(strings.TrimSpace(name))
}

// RemoveConnector deletes both the stored token AND the mcp.json entry.
func (b *Bridge) RemoveConnector(name string) (bool, error) {
	name = strings.TrimSpace(name)
	_ = connector.Default().Disconnect(name)
	return mcp.RemoveServer(mcp.UserConfigPath(), name)
}

// SetConnectorDisabled toggles whether a connector is wired (kept in config).
func (b *Bridge) SetConnectorDisabled(name string, disabled bool) (bool, error) {
	return mcp.SetServerDisabled(mcp.UserConfigPath(), strings.TrimSpace(name), disabled)
}
