package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/tool"
)

// serverConfig is one MCP server entry in mcp.json.
type serverConfig struct {
	Name    string            `json:"name"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
}

type mcpConfig struct {
	Servers []serverConfig `json:"servers"`
}

const connectTimeout = 15 * time.Second

// LoadTools reads an mcp.json config, connects to each server, and returns its
// tools wrapped as eigen tool Definitions (named "<server>_<tool>"), plus the
// live clients (which the caller must keep alive and Close on exit). A missing
// config file yields no tools and no error; a server that fails to connect is
// reported in errs but does not abort the others.
func LoadTools(ctx context.Context, path string) (defs []tool.Definition, clients []*Client, errs []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, []error{err}
	}
	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, []error{fmt.Errorf("%s: %w", path, err)}
	}

	for _, sc := range cfg.Servers {
		if sc.Name == "" || len(sc.Command) == 0 {
			errs = append(errs, fmt.Errorf("mcp server with empty name or command"))
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, connectTimeout)
		client, err := Connect(cctx, sc.Command, serverEnv(sc.Env))
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %q: %w", sc.Name, err))
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, connectTimeout)
		specs, err := client.ListTools(lctx)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %q: list tools: %w", sc.Name, err))
			client.Close()
			continue
		}
		clients = append(clients, client)
		for _, sp := range specs {
			defs = append(defs, wrap(client, sc.Name, sp))
		}
	}
	return defs, clients, errs
}

// wrap turns an MCP ToolSpec into an eigen tool.Definition backed by the client.
func wrap(client *Client, server string, sp ToolSpec) tool.Definition {
	params := sp.InputSchema
	if len(params) == 0 {
		params = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	name := sanitize(server) + "_" + sanitize(sp.Name)
	desc := sp.Description
	if desc == "" {
		desc = "MCP tool " + sp.Name + " from server " + server
	}
	toolName := sp.Name
	return tool.Definition{
		Name:        name,
		Description: desc,
		Parameters:  params,
		// MCP tools may have side effects; treat as mutating so gated mode asks.
		ReadOnly: false,
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			return client.CallTool(ctx, toolName, args)
		},
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

// sanitize keeps tool names to a provider-safe character set.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
