// Package google is eigen's NATIVE Google integration — direct Calendar/Gmail
// REST exposed as agent tools, authorized with the user's own Google Cloud
// OAuth client over a loopback authorization-code flow. This mirrors atrium's
// proven approach (BYO Desktop OAuth client + loopback callback + a stored
// refresh token + plain REST, no Google SDK) and is the first step of folding
// atrium's personal-data integrations into eigen.
//
// Why this is NOT the connector layer (internal/connector): connectors are
// remote MCP servers authorized via OAuth 2.1 + dynamic client registration.
// Google offers neither a hosted MCP endpoint nor dynamic registration — you
// pre-create a Desktop OAuth client in Google Cloud Console. So Google is a
// direct-API built-in, not an MCP connector.
package google

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// Google's OAuth 2.0 endpoints (hardcoded — atrium-style, no SDK so we don't
// pull cloud.google.com/go/compute/metadata in just for two URLs).
var googleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

// Scopes eigen requests. Calendar read+write and Gmail read (metadata/read).
// Kept modest; widen only when a tool needs it.
var defaultScopes = []string{
	"https://www.googleapis.com/auth/calendar",
	"https://www.googleapis.com/auth/gmail.readonly",
}

// clientCreds is a Google Cloud OAuth client (the "Desktop app" type). The user
// downloads this JSON from the Cloud Console; eigen reads client_id/secret from
// the "installed" object (Google's wrapper key).
type clientCreds struct {
	ClientID     string
	ClientSecret string
}

// loadClientCreds reads the BYO Google Cloud client from the first present of:
//   - $EIGEN_GOOGLE_CLIENT (explicit path)
//   - ~/.config/eigen/google_client.json
//   - ~/.config/atrium/google_client.json  (shared with atrium — same machine,
//     same Cloud client works for both; eases the eventual atrium→eigen move)
//
// Returns ok=false (not an error) when none is present, so the caller can guide
// the user through setup instead of failing hard.
func loadClientCreds() (clientCreds, bool) {
	for _, p := range clientCredPaths() {
		if c, ok := readClientFile(p); ok {
			return c, true
		}
	}
	return clientCreds{}, false
}

func clientCredPaths() []string {
	var paths []string
	if env := strings.TrimSpace(os.Getenv("EIGEN_GOOGLE_CLIENT")); env != "" {
		paths = append(paths, env)
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "eigen", "google_client.json"),
			filepath.Join(home, ".config", "atrium", "google_client.json"),
		)
	}
	return paths
}

func readClientFile(path string) (clientCreds, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return clientCreds{}, false
	}
	// Google wraps creds under "installed" (Desktop) or "web"; accept either, and
	// a bare {client_id,client_secret} too.
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return clientCreds{}, false
	}
	pick := func(obj json.RawMessage) (clientCreds, bool) {
		var m struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if json.Unmarshal(obj, &m) != nil || m.ClientID == "" {
			return clientCreds{}, false
		}
		return clientCreds{ClientID: m.ClientID, ClientSecret: m.ClientSecret}, true
	}
	if v, ok := raw["installed"]; ok {
		if c, ok := pick(v); ok {
			return c, true
		}
	}
	if v, ok := raw["web"]; ok {
		if c, ok := pick(v); ok {
			return c, true
		}
	}
	if c, ok := pick(data); ok {
		return c, true
	}
	return clientCreds{}, false
}

// Auth brokers the Google OAuth token for eigen: it holds the BYO client creds,
// runs the loopback authorize flow on demand, persists the refresh token (OS
// keychain via the store), and hands callers a refreshing http client.
type Auth struct {
	store  tokenStore
	openFn func(string) error // browser opener (overridable in tests)

	mu    sync.Mutex
	creds clientCreds
	haveC bool
	src   oauth2.TokenSource
}

// NewAuth builds the Google auth broker over a token store.
func NewAuth(store tokenStore) *Auth {
	c, ok := loadClientCreds()
	return &Auth{store: store, openFn: openBrowser, creds: c, haveC: ok}
}

// Configured reports whether a BYO Google Cloud client is present (without it,
// no flow can start — the GUI shows setup guidance instead of a Connect button).
func (a *Auth) Configured() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.haveC
}

// Connected reports whether a refresh token is stored (the account is linked).
func (a *Auth) Connected() bool {
	tok, err := a.store.load()
	return err == nil && tok != nil && tok.RefreshToken != ""
}

func (a *Auth) oauthConfig(redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.creds.ClientID,
		ClientSecret: a.creds.ClientSecret,
		Endpoint:     googleEndpoint,
		RedirectURL:  redirect,
		Scopes:       defaultScopes,
	}
}

// tokenSource returns a refreshing source seeded from the stored token; it
// persists a rotated token back. Errors when not connected/configured.
func (a *Auth) tokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.haveC {
		return nil, fmt.Errorf("google: no OAuth client configured — add a Google Cloud Desktop client (see SetupHint)")
	}
	if a.src != nil {
		return a.src, nil
	}
	tok, err := a.store.load()
	if err != nil || tok == nil || tok.RefreshToken == "" {
		return nil, fmt.Errorf("google: not connected — run the connect flow first")
	}
	base := a.oauthConfig("").TokenSource(ctx, tok)
	a.src = &persistingSource{store: a.store, base: base}
	return a.src, nil
}

// Disconnect drops the stored token (and the live source).
func (a *Auth) Disconnect() error {
	a.mu.Lock()
	a.src = nil
	a.mu.Unlock()
	return a.store.clear()
}

// persistingSource writes a rotated token back to the store on refresh.
type persistingSource struct {
	store tokenStore
	base  oauth2.TokenSource
	mu    sync.Mutex
	last  *oauth2.Token
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	changed := p.last == nil || tok.AccessToken != p.last.AccessToken || tok.RefreshToken != p.last.RefreshToken
	p.last = tok
	p.mu.Unlock()
	if changed {
		// A refresh response often omits the refresh_token; keep the stored one.
		if tok.RefreshToken == "" {
			if prev, err := p.store.load(); err == nil && prev != nil {
				tok.RefreshToken = prev.RefreshToken
			}
		}
		_ = p.store.save(tok)
	}
	return tok, nil
}
