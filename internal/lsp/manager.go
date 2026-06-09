// Package lsp — manager: maps a file to its language server (by extension),
// starting servers lazily on first use and caching them for the session. It
// also tracks which documents have been opened so position requests resolve.
package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ServerConfig is one language-server entry: a command plus the file
// extensions it handles (e.g. ".go").
type ServerConfig struct {
	Name       string            `json:"name"`
	Command    []string          `json:"command"`
	Extensions []string          `json:"extensions"`
	Env        map[string]string `json:"env"`
	LanguageID string            `json:"language_id"` // optional; sent on didOpen
}

// connectTimeout bounds the initialize handshake and each request.
const connectTimeout = 20 * time.Second

// Manager owns the configured servers and their live clients.
type Manager struct {
	root    string
	configs []ServerConfig

	mu      sync.Mutex
	clients map[string]*Client // keyed by server name
	opened  map[string]bool    // document URIs already didOpen'd
	failed  map[string]error   // servers that failed to start (don't retry)
}

// NewManager builds a manager rooted at root with the given server configs.
func NewManager(root string, configs []ServerConfig) *Manager {
	return &Manager{
		root:    root,
		configs: configs,
		clients: map[string]*Client{},
		opened:  map[string]bool{},
		failed:  map[string]error{},
	}
}

// ServerNames returns the configured server names (for listings).
func (m *Manager) ServerNames() []string {
	out := make([]string, 0, len(m.configs))
	for _, c := range m.configs {
		out = append(out, c.Name)
	}
	return out
}

// configFor returns the server config that handles a file's extension.
func (m *Manager) configFor(path string) (ServerConfig, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, c := range m.configs {
		for _, e := range c.Extensions {
			if strings.ToLower(e) == ext {
				return c, true
			}
		}
	}
	return ServerConfig{}, false
}

// clientFor returns a live, initialized client for a file's language, starting
// the server on first use. The returned document URI is opened with the file's
// current contents so position requests resolve.
func (m *Manager) clientFor(ctx context.Context, path string) (*Client, string, error) {
	cfg, ok := m.configFor(path)
	if !ok {
		return nil, "", fmt.Errorf("no language server configured for %q (ext %q)", filepath.Base(path), filepath.Ext(path))
	}

	m.mu.Lock()
	if err := m.failed[cfg.Name]; err != nil {
		m.mu.Unlock()
		return nil, "", fmt.Errorf("language server %q unavailable: %w", cfg.Name, err)
	}
	client := m.clients[cfg.Name]
	m.mu.Unlock()

	if client == nil {
		cctx, cancel := context.WithTimeout(ctx, connectTimeout)
		c, err := Connect(cctx, cfg.Command, serverEnv(cfg.Env), m.root)
		cancel()
		if err != nil {
			m.mu.Lock()
			m.failed[cfg.Name] = err
			m.mu.Unlock()
			return nil, "", fmt.Errorf("start %q: %w", cfg.Name, err)
		}
		m.mu.Lock()
		m.clients[cfg.Name] = c
		m.mu.Unlock()
		client = c
	}

	uri, err := m.ensureOpen(client, cfg, path)
	if err != nil {
		return nil, "", err
	}
	return client, uri, nil
}

// ensureOpen sends textDocument/didOpen for path once per session, returning
// its URI.
func (m *Manager) ensureOpen(client *Client, cfg ServerConfig, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	uri := PathToURI(abs)

	m.mu.Lock()
	already := m.opened[uri]
	m.mu.Unlock()
	if already {
		return uri, nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if err := client.DidOpen(uri, cfg.LanguageID, string(data)); err != nil {
		return "", err
	}
	m.mu.Lock()
	m.opened[uri] = true
	m.mu.Unlock()
	return uri, nil
}

// Close shuts down every started server.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, c := range m.clients {
		_ = c.Close()
		delete(m.clients, name)
	}
}

// serverEnv merges configured env onto the process environment.
func serverEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// waitDiagnostics polls for a server's published diagnostics for uri, returning
// as soon as any arrive or after a short grace period (they are pushed
// asynchronously after didOpen, so a freshly-opened file may have none yet).
func waitDiagnostics(ctx context.Context, c *Client, uri string) []Diagnostic {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(75 * time.Millisecond)
	defer tick.Stop()
	for {
		if d := c.Diagnostics(uri); len(d) > 0 {
			return d
		}
		select {
		case <-ctx.Done():
			return c.Diagnostics(uri)
		case <-deadline.C:
			return c.Diagnostics(uri)
		case <-tick.C:
		}
	}
}
