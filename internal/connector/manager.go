package connector

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// callbackPath is the loopback redirect path the authorization code lands on.
const callbackPath = "/eigen/oauth/callback"

// authFlowTimeout bounds how long we wait for the user to complete the browser
// authorization (open page → consent → redirect).
const authFlowTimeout = 5 * time.Minute

// Manager owns connector OAuth state: it persists per-server tokens, runs the
// authorization-code+PKCE flow, and hands the MCP transport an auth-header func
// that yields a fresh bearer (refreshing via the refresh token as needed).
//
// It is safe for concurrent use. The HTTP client is shared across discovery,
// registration, and token operations.
type Manager struct {
	store  secretStore
	hc     *http.Client
	openFn func(url string) error // browser opener (overridable in tests)

	mu      sync.Mutex
	loaded  bool                          // cache has been read from the store
	cache   map[string]record             // name → record (loaded lazily)
	sources map[string]oauth2.TokenSource // name → live refreshing token source
}

// NewManager builds a Manager. OAuth tokens are stored in the OS keychain when
// available (metadata in the JSON file at path); on a platform with no usable
// keyring it falls back to the plain file store. Use NewManagerWithStore to pin
// a store in tests.
func NewManager(path string) *Manager {
	return NewManagerWithStore(newSecretStore(path))
}

// NewManagerWithStore builds a Manager over an explicit secretStore (tests use
// the in-memory / file store directly to avoid touching the real keychain).
func NewManagerWithStore(store secretStore) *Manager {
	return &Manager{
		store:   store,
		hc:      &http.Client{Timeout: 30 * time.Second},
		openFn:  openBrowser,
		cache:   map[string]record{},
		sources: map[string]oauth2.TokenSource{},
	}
}

// ensureLoaded reads records into the cache once (cheap memo). Caller holds m.mu.
func (m *Manager) ensureLoaded() error {
	if m.loaded {
		return nil
	}
	recs, err := m.store.load()
	if err != nil {
		return err
	}
	m.cache = recs
	m.loaded = true
	return nil
}

// oauthConfig rebuilds the oauth2.Config for a record (endpoints + client).
func (r record) oauthConfig(redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     r.ClientID,
		ClientSecret: r.ClientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: r.AuthURL, TokenURL: r.TokenURL},
		RedirectURL:  redirectURI,
		Scopes:       r.Scopes,
	}
}

// AuthHeaderFunc returns (fn, true) when a connector is connected: fn yields the
// current "Bearer <token>", refreshing transparently and persisting a rotated
// token. Returns ok=false when no connected record exists for name, so the MCP
// loader falls back to its static path. This is the func wired into
// mcp.RemoteAuthProvider.
func (m *Manager) AuthHeaderFunc(name, _serverURL string) (func() string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLoaded(); err != nil {
		return nil, false
	}
	rec, ok := m.cache[name]
	if !ok || rec.Token == nil || rec.Token.AccessToken == "" {
		return nil, false
	}
	src := m.sources[name]
	if src == nil {
		// A reusing token source refreshes with the refresh token and only does so
		// when the access token is near expiry. We wrap it to persist a rotated
		// token back to the store.
		cfg := rec.oauthConfig("") // redirect not needed for refresh
		base := cfg.TokenSource(context.Background(), rec.Token)
		src = &persistingSource{m: m, name: name, base: base}
		m.sources[name] = src
	}
	return func() string {
		tok, err := src.Token()
		if err != nil || tok == nil || tok.AccessToken == "" {
			return ""
		}
		typ := tok.Type()
		if typ == "" {
			typ = "Bearer"
		}
		return typ + " " + tok.AccessToken
	}, true
}

// persistingSource wraps an oauth2.TokenSource and writes a rotated token back
// to the manager's store, so a refresh survives a restart.
type persistingSource struct {
	m    *Manager
	name string
	base oauth2.TokenSource
	mu   sync.Mutex
	last *oauth2.Token
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
		p.m.persistToken(p.name, tok)
	}
	return tok, nil
}

// persistToken stores a (possibly refreshed) token for name.
func (m *Manager) persistToken(name string, tok *oauth2.Token) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLoaded(); err != nil {
		return
	}
	rec, ok := m.cache[name]
	if !ok {
		return
	}
	rec.Token = tok
	m.cache[name] = rec
	_ = m.store.save(m.cache)
}

// Connect runs the full interactive OAuth flow for a remote MCP server:
// discover → (register) → open browser for PKCE authorization → exchange code →
// persist token. resourceMetaHint is the resource_metadata URL from the server's
// 401 WWW-Authenticate (may be ""). Returns when the token is stored or the flow
// fails/times out. The browser is opened via openFn.
func (m *Manager) Connect(ctx context.Context, name, serverURL, resourceMetaHint string) error {
	asm, prm, err := discover(ctx, m.hc, serverURL, resourceMetaHint)
	if err != nil {
		return fmt.Errorf("connector %q: discovery failed: %w", name, err)
	}
	scopes := chooseScopes(prm, asm)

	cb, err := newLoopbackServer(callbackPath)
	if err != nil {
		return err
	}
	defer cb.close()
	redirect := cb.redirectURI()

	// Need a client_id. Reuse a stored one for this server; else dynamically
	// register (RFC 7591) if the AS supports it.
	clientID, clientSecret, err := m.clientFor(ctx, name, asm, redirect, scopes)
	if err != nil {
		return err
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: asm.AuthorizationEndpoint, TokenURL: asm.TokenEndpoint},
		RedirectURL:  redirect,
		Scopes:       scopes,
	}
	verifier := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier() // reuse the CSPRNG helper for an opaque state
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oauth2.AccessTypeOffline}
	// RFC 8707 resource indicator: bind the token to this MCP resource.
	if prm != nil && prm.Resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", prm.Resource))
	} else {
		opts = append(opts, oauth2.SetAuthURLParam("resource", serverURL))
	}
	authURL := cfg.AuthCodeURL(state, opts...)

	if err := m.openFn(authURL); err != nil {
		return fmt.Errorf("connector %q: open browser: %w (visit manually: %s)", name, err, authURL)
	}

	flowCtx, cancel := context.WithTimeout(ctx, authFlowTimeout)
	defer cancel()
	res, err := cb.wait(flowCtx)
	if err != nil {
		return fmt.Errorf("connector %q: timed out waiting for authorization: %w", name, err)
	}
	if res.err != "" {
		return fmt.Errorf("connector %q: authorization denied: %s", name, res.err)
	}
	if res.state != state {
		return fmt.Errorf("connector %q: state mismatch (possible CSRF) — aborting", name)
	}

	exchOpts := []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)}
	if prm != nil && prm.Resource != "" {
		exchOpts = append(exchOpts, oauth2.SetAuthURLParam("resource", prm.Resource))
	}
	tok, err := cfg.Exchange(ctx, res.code, exchOpts...)
	if err != nil {
		return fmt.Errorf("connector %q: token exchange failed: %w", name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLoaded(); err != nil {
		return err
	}
	m.cache[name] = record{
		Name:         name,
		ServerURL:    serverURL,
		AuthURL:      asm.AuthorizationEndpoint,
		TokenURL:     asm.TokenEndpoint,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       scopes,
		Token:        tok,
		Connected:    time.Now(),
	}
	delete(m.sources, name) // force a fresh token source next use
	return m.store.save(m.cache)
}

// clientFor returns a client_id/secret for the server: a stored one if we've
// registered before, else a freshly registered public client (RFC 7591). An AS
// without a registration endpoint and no stored client is an error the caller
// surfaces (the user would need a pre-provisioned client_id — future work).
func (m *Manager) clientFor(ctx context.Context, name string, asm *authServerMeta, redirect string, scopes []string) (string, string, error) {
	m.mu.Lock()
	if err := m.ensureLoaded(); err == nil {
		if rec, ok := m.cache[name]; ok && rec.ClientID != "" && rec.AuthURL == asm.AuthorizationEndpoint {
			m.mu.Unlock()
			return rec.ClientID, rec.ClientSecret, nil
		}
	}
	m.mu.Unlock()

	if asm.RegistrationEndpoint == "" {
		return "", "", fmt.Errorf("connector %q: authorization server has no dynamic registration endpoint and no client is configured", name)
	}
	reg, err := registerClient(ctx, m.hc, asm.RegistrationEndpoint, redirect, "Eigen", scopes)
	if err != nil {
		return "", "", fmt.Errorf("connector %q: %w", name, err)
	}
	return reg.ClientID, reg.ClientSecret, nil
}

// Disconnect drops a connector's stored token + client (revokes locally).
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLoaded(); err != nil {
		return err
	}
	delete(m.cache, name)
	delete(m.sources, name)
	return m.store.save(m.cache)
}

// Statuses lists every known connector's connection state.
func (m *Manager) Statuses() ([]Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLoaded(); err != nil {
		return nil, err
	}
	return sortedStatuses(m.cache), nil
}

// IsConnected reports whether name has a stored access token.
func (m *Manager) IsConnected(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLoaded(); err != nil {
		return false
	}
	rec, ok := m.cache[name]
	return ok && rec.Token != nil && rec.Token.AccessToken != ""
}

// chooseScopes prefers the resource's advertised scopes, then the AS's, else
// none (the server decides).
func chooseScopes(prm *protectedResourceMeta, asm *authServerMeta) []string {
	if prm != nil && len(prm.ScopesSupported) > 0 {
		return prm.ScopesSupported
	}
	if asm != nil && len(asm.ScopesSupported) > 0 {
		return asm.ScopesSupported
	}
	return nil
}
