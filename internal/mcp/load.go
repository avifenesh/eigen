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

	// Description is the one-line "what is this server" shown to the model at
	// the top level of progressive disclosure (e.g. "drive the user's real
	// Chrome", "isolated Linux sandbox"). REQUIRED in practice: it's how eigen
	// presents the server before the model drills in. When empty, eigen falls
	// back to the server's own MCP `instructions`, then warns — an undescribed
	// server is opaque to the model.
	Description string `json:"description"`

	// Tools, when non-empty, is an allowlist: only the named server tools are
	// exposed to the model (names as the server declares them, without the
	// "<server>_" prefix; "*" suffix allowed for prefix matches). Tool schemas
	// ride along on EVERY model request, so a server with dozens of tools can
	// quietly cost thousands of tokens per call — allowlist what you use.
	Tools []string `json:"tools"`

	// ExcludeTools removes specific server tools (same name syntax). Applied
	// after Tools.
	ExcludeTools []string `json:"exclude_tools"`

	// Disabled skips this server entirely (kept in config, not connected) —
	// toggled from the app's plugins page.
	Disabled bool `json:"disabled,omitempty"`
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
	var cfg mcpConfig
	if data, err := os.ReadFile(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, []error{err}
		}
		// Missing config is fine — built-ins may still apply.
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, []error{fmt.Errorf("%s: %w", path, err)}
	}

	// Built-in servers (e.g. the agent workspace) are auto-registered as
	// first-class capabilities when their binary is present, UNLESS the user
	// already configured a server with the same name (their config wins).
	cfg.Servers = withBuiltinServers(cfg.Servers)

	for _, sc := range cfg.Servers {
		if sc.Disabled {
			continue
		}
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
		// Group description (Level-0 frontmatter): config description wins, else
		// the server's own MCP instructions, else a warning + generic gist.
		gist := strings.TrimSpace(sc.Description)
		if gist == "" {
			gist = firstSentence(client.Instructions())
		}
		if gist == "" {
			gist = sc.Name + " MCP server"
			fmt.Fprintf(os.Stderr, "eigen: mcp %q has no description — add \"description\" in mcp.json so the model knows what it's for\n", sc.Name)
		}
		kept := 0
		for _, sp := range specs {
			if !toolAllowed(sc, sp.Name) {
				continue
			}
			defs = append(defs, wrap(client, sc.Name, gist, sp))
			kept++
		}
		if len(sc.Tools) > 0 || len(sc.ExcludeTools) > 0 {
			fmt.Fprintf(os.Stderr, "eigen: mcp %q: %d/%d tools exposed (filtered by mcp.json)\n", sc.Name, kept, len(specs))
		}
	}
	return defs, clients, errs
}

// toolAllowed applies the per-server allowlist/excludelist to a server-declared
// tool name. Patterns match exactly, or as a prefix when ending in "*".
func toolAllowed(sc serverConfig, name string) bool {
	match := func(pat string) bool {
		if strings.HasSuffix(pat, "*") {
			return strings.HasPrefix(name, strings.TrimSuffix(pat, "*"))
		}
		return name == pat
	}
	if len(sc.Tools) > 0 {
		ok := false
		for _, pat := range sc.Tools {
			if match(pat) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, pat := range sc.ExcludeTools {
		if match(pat) {
			return false
		}
	}
	return true
}

// wrap turns an MCP ToolSpec into an eigen tool.Definition backed by the client.
func wrap(client *Client, server, gist string, sp ToolSpec) tool.Definition {
	params := slimSchema(sp.InputSchema)
	if len(params) == 0 {
		params = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	name := sanitize(server) + "_" + sanitize(sp.Name)
	desc := sp.Description
	if desc == "" {
		desc = "MCP tool " + sp.Name + " from server " + server
	}
	toolName := sp.Name
	// Honor the MCP readOnlyHint: a tool the server declares read-only and
	// non-destructive has no side effects, so it can auto-run in gated mode
	// instead of prompting for approval on every call. Anything without an
	// explicit safe hint stays mutating (fail safe).
	readOnly := sp.Annotations != nil && sp.Annotations.ReadOnlyHint && !sp.Annotations.DestructiveHint
	// Screenshot-producing tools (e.g. the sandbox's workspace_screenshot /
	// workspace_observe) return a PNG file PATH rather than an inline image —
	// attach the file so the model can see it. Gated by tool name so an
	// ordinary tool that happens to return a "path" field isn't slurped.
	attachShot := strings.Contains(toolName, "screenshot") || strings.Contains(toolName, "observe")
	return tool.Definition{
		Name:        name,
		Description: desc,
		Parameters:  params,
		ReadOnly:    readOnly,
		// Progressive disclosure: MCP tools are niche (schema withheld from each
		// request) and grouped by their server, so the model browses the server
		// then opens a tool via search_tools instead of paying for every schema.
		Niche:     true,
		Group:     sanitize(server),
		GroupDesc: gist,
		RunRich: func(ctx context.Context, args json.RawMessage) (tool.Result, error) {
			res, err := client.CallToolRich(ctx, toolName, args)
			if err == nil && attachShot {
				res = attachScreenshotPath(res)
			}
			return tool.Result{Text: res.Text, Images: res.Images}, err
		},
	}
}

// slimSchema strips JSON-Schema metadata that costs tokens on every request
// without helping the model pick arguments: "$schema" and "title" keys (at any
// nesting level). The schema's structure, types, descriptions, enums, and
// defaults are preserved. Returns the input unchanged if it isn't a JSON object.
func slimSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	stripped := stripSchemaNoise(v)
	out, err := json.Marshal(stripped)
	if err != nil {
		return raw
	}
	return out
}

// stripSchemaNoise removes "$schema" and "title" keys recursively.
func stripSchemaNoise(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"$schema", "title"} {
			delete(t, k)
		}
		for k, val := range t {
			t[k] = stripSchemaNoise(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = stripSchemaNoise(val)
		}
		return t
	default:
		return v
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

// firstSentence returns the first sentence/line of s (for a one-line server
// gist derived from a server's MCP instructions), truncated.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexAny(s, ".!"); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		s = s[:117] + "…"
	}
	return s
}
