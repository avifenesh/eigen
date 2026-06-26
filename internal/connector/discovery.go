// Package connector implements OAuth 2.1 for remote MCP servers ("connectors"
// like Google Workspace, Slack, Notion). When a remote MCP server answers 401
// with a WWW-Authenticate challenge, this package discovers its authorization
// server (RFC 9728 protected-resource metadata → RFC 8414 / OpenID
// authorization-server metadata), runs the PKCE authorization-code flow against
// a loopback redirect, and persists the resulting token so the MCP transport
// can attach a bearer that refreshes transparently.
//
// The OAuth mechanics (token exchange, refresh, PKCE) lean on golang.org/x/
// oauth2; the MCP-specific discovery + dynamic client registration are here.
package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// discoveryTimeout bounds a single metadata fetch.
const discoveryTimeout = 15 * time.Second

// authServerMeta is the subset of RFC 8414 / OpenID authorization-server
// metadata eigen needs to drive the flow.
type authServerMeta struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
	// CodeChallengeMethodsSupported tells us whether the server advertises S256
	// (PKCE). We send S256 regardless (OAuth 2.1 requires it), but a server that
	// only lists "plain" is non-compliant — surfaced for diagnostics.
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

// protectedResourceMeta is the subset of RFC 9728 protected-resource metadata:
// it points at the authorization server(s) that guard this MCP resource.
type protectedResourceMeta struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// parseWWWAuthenticate extracts the resource_metadata URL from a Bearer
// WWW-Authenticate challenge (RFC 9728 §5.1), e.g.
//
//	Bearer realm="x", resource_metadata="https://host/.well-known/oauth-protected-resource"
//
// Returns "" when the header carries no resource_metadata hint (we then fall
// back to deriving the well-known URL from the server's own origin).
func parseWWWAuthenticate(header string) string {
	// Strip the leading scheme token ("Bearer"/"DPoP") if present.
	h := strings.TrimSpace(header)
	if i := strings.IndexByte(h, ' '); i > 0 {
		scheme := strings.ToLower(h[:i])
		if scheme == "bearer" || scheme == "dpop" {
			h = h[i+1:]
		}
	}
	for _, part := range splitAuthParams(h) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k), "resource_metadata") {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

// splitAuthParams splits a comma-separated auth-param list, respecting quoted
// values (a quoted value may itself contain commas).
func splitAuthParams(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

// discover resolves a remote MCP server URL (+ optional resource_metadata hint
// from its 401 challenge) to its authorization-server metadata. Flow:
//  1. Fetch protected-resource metadata (from the hint, else derived from the
//     server origin) to learn which authorization server(s) guard it.
//  2. Fetch that authorization server's metadata for the endpoints.
//
// Either step degrades to deriving the well-known path from an origin, so a
// minimally-compliant server (no resource metadata, AS == resource origin) still
// works.
func discover(ctx context.Context, hc *http.Client, serverURL, resourceMetaHint string) (*authServerMeta, *protectedResourceMeta, error) {
	prm, _ := fetchProtectedResource(ctx, hc, serverURL, resourceMetaHint)
	// Choose the authorization server: the first one the resource names, else the
	// resource's own origin (a server that is its own AS).
	asBase := originOf(serverURL)
	if prm != nil && len(prm.AuthorizationServers) > 0 {
		asBase = strings.TrimRight(prm.AuthorizationServers[0], "/")
	}
	asm, err := fetchAuthServer(ctx, hc, asBase)
	if err != nil {
		return nil, prm, err
	}
	return asm, prm, nil
}

// fetchProtectedResource gets RFC 9728 metadata. hint (from WWW-Authenticate)
// wins; otherwise we try the well-known path on the server's origin.
func fetchProtectedResource(ctx context.Context, hc *http.Client, serverURL, hint string) (*protectedResourceMeta, error) {
	u := hint
	if u == "" {
		u = originOf(serverURL) + "/.well-known/oauth-protected-resource"
	}
	var m protectedResourceMeta
	if err := fetchJSON(ctx, hc, u, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// fetchAuthServer gets RFC 8414 metadata, trying the standard well-known path
// then the OpenID Connect variant (some servers only expose the latter).
func fetchAuthServer(ctx context.Context, hc *http.Client, asBase string) (*authServerMeta, error) {
	asBase = strings.TrimRight(asBase, "/")
	candidates := []string{
		asBase + "/.well-known/oauth-authorization-server",
		asBase + "/.well-known/openid-configuration",
	}
	var lastErr error
	for _, u := range candidates {
		var m authServerMeta
		if err := fetchJSON(ctx, hc, u, &m); err != nil {
			lastErr = err
			continue
		}
		if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
			lastErr = fmt.Errorf("authorization-server metadata at %s lacks endpoints", u)
			continue
		}
		return &m, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorization-server metadata under %s", asBase)
	}
	return nil, lastErr
}

// fetchJSON GETs url and decodes a JSON body into out, bounding the read.
func fetchJSON(ctx context.Context, hc *http.Client, url string, out any) error {
	cctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// originOf returns the scheme://host[:port] origin of a URL (no path), used to
// derive well-known metadata paths.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	return u.Scheme + "://" + u.Host
}
