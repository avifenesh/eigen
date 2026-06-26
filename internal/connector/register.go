package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// clientRegistration is the RFC 7591 dynamic-client-registration response we
// keep: the issued client_id (and optional secret for confidential clients).
type clientRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	// ClientSecretExpiresAt is 0 for "never expires" (public clients).
	ClientSecretExpiresAt int64 `json:"client_secret_expires_at,omitempty"`
}

// registerClient performs RFC 7591 dynamic client registration against the
// authorization server's registration endpoint, requesting a public client
// (no secret) usable with the loopback redirect + PKCE. Many MCP authorization
// servers require DCR because there's no human to pre-register an app.
func registerClient(ctx context.Context, hc *http.Client, registrationEndpoint, redirectURI, clientName string, scopes []string) (*clientRegistration, error) {
	body := map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none", // public client (PKCE secures it)
		"application_type":           "native",
	}
	if len(scopes) > 0 {
		body["scope"] = joinScopes(scopes)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, registrationEndpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dynamic client registration: HTTP %d: %s", resp.StatusCode, string(data))
	}
	var reg clientRegistration
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	if reg.ClientID == "" {
		return nil, fmt.Errorf("dynamic client registration returned no client_id")
	}
	return &reg, nil
}

// joinScopes space-joins scopes (OAuth scope syntax).
func joinScopes(scopes []string) string {
	out := ""
	for i, s := range scopes {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}
