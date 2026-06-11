package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// grokDefaultBaseURL is xAI's OpenAI-compatible API root. The grok-cli uses an
// internal OIDC proxy (cli-chat-proxy.grok.com); eigen targets the public API
// with an XAI_API_KEY for a stable, supportable path.
const grokDefaultBaseURL = "https://api.x.ai/v1"

// grokCLIProxyBaseURL is the grok-cli's internal chat proxy, used when falling
// back to the grok-cli OIDC login (~/.grok/auth.json) instead of an API key.
const grokCLIProxyBaseURL = "https://cli-chat-proxy.grok.com/v1"

// Grok drives xAI's Grok models over the OpenAI-compatible chat-completions API.
// Beyond plain chat it supports xAI Live Search (server-side web + X/Twitter
// search) via the search_parameters request field, toggled per model/capability
// and overridable with env.
type Grok struct {
	c *chatClient

	// search controls Live Search: "off" disables it; "auto" lets the model
	// decide; "on" forces it. sources are the Live Search sources to allow
	// (e.g. "web", "x", "news").
	search  string
	sources []string
}

// NewGrok builds a Grok provider from the environment.
//
//	XAI_API_KEY (or EIGEN_GROK_API_KEY)   xAI key from console.x.ai
//	EIGEN_GROK_BASE_URL                   override the API root (default api.x.ai/v1)
//	EIGEN_GROK_SEARCH=off|auto|on         Live Search mode (default: per-model)
//	EIGEN_GROK_SEARCH_SOURCES=web,x,news  comma-separated Live Search sources
//
// If no API key is set, it falls back to the grok-cli OIDC login: an unexpired
// bearer token from ~/.grok/auth.json against the cli-chat-proxy endpoint. This
// lets eigen reuse an existing `grok` CLI session without a separate key.
func NewGrok(model string) (*Grok, error) {
	key := firstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("EIGEN_GROK_API_KEY"))
	base := firstNonEmpty(os.Getenv("EIGEN_GROK_BASE_URL"), grokDefaultBaseURL)

	if key == "" {
		// Fall back to a grok-cli OIDC session token.
		if tok, err := grokCLIToken(); err == nil && tok != "" {
			key = tok
			// The CLI token is only valid against the CLI proxy unless the user
			// pointed EIGEN_GROK_BASE_URL elsewhere.
			if os.Getenv("EIGEN_GROK_BASE_URL") == "" {
				base = grokCLIProxyBaseURL
			}
		}
	}
	if key == "" {
		return nil, fmt.Errorf("no xAI credentials: set XAI_API_KEY or log in with the grok CLI (~/.grok/auth.json)")
	}
	if model == "" {
		model = "grok-build"
	}
	g := &Grok{
		c:       newChatClient(base, model, key, "grok"),
		search:  "off",
		sources: []string{"web", "x"},
	}
	// When using the grok-cli proxy, send the headers the proxy requires (it
	// rejects requests without a client version, and routes by model override).
	if base == grokCLIProxyBaseURL {
		g.c.extraHeaders = map[string]string{
			"X-XAI-Token-Auth":         "xai-grok-cli",
			"x-grok-client-version":    grokCLIClientVersion(),
			"x-grok-client-identifier": "eigen",
			"x-grok-model-override":    model,
		}
	}
	// Default search on for models the catalog marks as search-capable — but not
	// on the grok-cli proxy, where the legacy search_parameters Live Search is
	// deprecated (use the public API with XAI_API_KEY for Live Search).
	if info, ok := Lookup(model); ok && info.Search && base != grokCLIProxyBaseURL {
		g.search = "auto"
	}
	if v := strings.TrimSpace(os.Getenv("EIGEN_GROK_SEARCH")); v != "" {
		g.search = v
	}
	if v := strings.TrimSpace(os.Getenv("EIGEN_GROK_SEARCH_SOURCES")); v != "" {
		g.sources = splitCSV(v)
	}
	// Wire the Live Search request field.
	g.c.extra = g.searchParams
	return g, nil
}

func (g *Grok) Name() string    { return g.c.model + " (xai grok)" }
func (g *Grok) ModelID() string { return g.c.model }

func (g *Grok) Complete(ctx context.Context, req Request) (*Response, error) {
	return g.c.complete(ctx, g.prepare(req))
}

func (g *Grok) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	return g.c.stream(ctx, g.prepare(req), sink)
}

// prepare appends a hint to the system prompt when Live Search is active,
// telling Grok to prefer its built-in search over the client-side fetch tool.
// It lists the actual enabled sources so the model knows what it can search.
func (g *Grok) prepare(req Request) Request {
	if g.search == "off" {
		return req
	}
	srcList := strings.Join(g.sources, " and ")
	req.System += fmt.Sprintf(
		"\n\nYou have built-in live search via search_parameters (sources: %s). "+
			"Use it instead of the fetch tool for any web lookups — it is faster, more reliable, and returns fresher results. "+
			"You can search the web and X/Twitter in real time. Prefer this over fetch for all online information.",
		srcList,
	)
	return req
}

// SearchMode reports the current Live Search mode (off|auto|on).
func (g *Grok) SearchMode() string { return g.search }

// SetSearch changes the Live Search mode. Returns false for an unknown mode.
func (g *Grok) SetSearch(mode string) bool {
	switch mode {
	case "off", "auto", "on":
		g.search = mode
		return true
	default:
		return false
	}
}

// searchParams builds xAI's search_parameters field for Live Search, or nil
// when search is off (so the field is omitted entirely).
func (g *Grok) searchParams() map[string]any {
	if g.search == "off" || g.search == "" {
		return nil
	}
	srcs := make([]map[string]any, 0, len(g.sources))
	for _, s := range g.sources {
		srcs = append(srcs, map[string]any{"type": s})
	}
	return map[string]any{
		"search_parameters": map[string]any{
			"mode":    g.search, // "auto" | "on"
			"sources": srcs,
		},
	}
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// grokCLIClientVersion returns a grok CLI client version to satisfy the proxy's
// minimum-version check. It reads the installed CLI version from
// ~/.grok/version.json (override with EIGEN_GROK_CLIENT_VERSION), falling back
// to a value above the proxy's documented minimum.
func grokCLIClientVersion() string {
	if v := strings.TrimSpace(os.Getenv("EIGEN_GROK_CLIENT_VERSION")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err == nil {
		raw, rerr := os.ReadFile(filepath.Join(home, ".grok", "version.json"))
		if rerr == nil {
			var v struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(raw, &v) == nil && v.Version != "" {
				return v.Version
			}
		}
	}
	return "0.2.33" // a known-good version above the proxy minimum (0.1.202)
}

// grokCLIToken reads an unexpired bearer token from the grok CLI's auth store
// (~/.grok/auth.json, override with EIGEN_GROK_AUTH_FILE). The file maps an
// "<issuer>::<client_id>" key to an entry with a "key" (the bearer JWT) and an
// "expires_at" RFC3339 timestamp. Returns the freshest unexpired token.
func grokCLIToken() (string, error) {
	path := os.Getenv("EIGEN_GROK_AUTH_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".grok", "auth.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var entries map[string]struct {
		Key       string `json:"key"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return "", fmt.Errorf("parse grok auth: %w", err)
	}
	best := ""
	var bestExp time.Time
	for _, e := range entries {
		if e.Key == "" {
			continue
		}
		// Prefer an unexpired token; among those, the one expiring latest.
		exp, perr := time.Parse(time.RFC3339, e.ExpiresAt)
		if perr == nil && time.Now().After(exp) {
			continue // expired
		}
		if best == "" || exp.After(bestExp) {
			best, bestExp = e.Key, exp
		}
	}
	if best == "" {
		return "", fmt.Errorf("no unexpired grok CLI token in %s (run `grok` to log in)", path)
	}
	return best, nil
}
