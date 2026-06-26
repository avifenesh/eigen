package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

// fakeAuthServer is a minimal OAuth 2.1 authorization server for tests: it
// serves protected-resource + authorization-server metadata, dynamic client
// registration, and a token endpoint that checks the PKCE verifier.
type fakeAuthServer struct {
	*httptest.Server
	issuedCode string
	challenge  string
	clientID   string
}

func newFakeAuthServer(t *testing.T) *fakeAuthServer {
	t.Helper()
	f := &fakeAuthServer{}
	mux := http.NewServeMux()
	f.Server = httptest.NewServer(mux)
	base := f.URL

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"resource":              base + "/mcp",
			"authorization_servers": []string{base},
			"scopes_supported":      []string{"read", "write"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                           base,
			"authorization_endpoint":           base + "/authorize",
			"token_endpoint":                   base + "/token",
			"registration_endpoint":            base + "/register",
			"code_challenge_methods_supported": []string{"S256"},
			"scopes_supported":                 []string{"read", "write"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		f.clientID = "client-xyz"
		writeJSON(w, map[string]any{"client_id": f.clientID, "token_endpoint_auth_method": "none"})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.challenge = q.Get("code_challenge")
		f.issuedCode = "auth-code-123"
		// Redirect back to the loopback callback with code + state.
		redirect := q.Get("redirect_uri")
		u, _ := url.Parse(redirect)
		rq := u.Query()
		rq.Set("code", f.issuedCode)
		rq.Set("state", q.Get("state"))
		u.RawQuery = rq.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		// PKCE: the verifier must be present (we don't recompute S256 here, but a
		// missing verifier is a hard fail — proves the option was sent).
		if r.Form.Get("code_verifier") == "" {
			http.Error(w, "missing code_verifier", http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != f.issuedCode {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"access_token":  "access-tok-1",
			"token_type":    "Bearer",
			"refresh_token": "refresh-tok-1",
			"expires_in":    3600,
		})
	})
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestManagerConnectFlow(t *testing.T) {
	as := newFakeAuthServer(t)
	defer as.Close()

	dir := t.TempDir()
	// File store (not the keychain) so the test is hermetic on any platform.
	m := NewManagerWithStore(newFileStore(filepath.Join(dir, "connectors.json")))
	// Instead of a real browser, fetch the authorize URL ourselves so the AS
	// redirects to the loopback callback (completing the flow headlessly).
	m.openFn = func(authURL string) error {
		go func() {
			// Follow the redirect chain; the http.Client hits our loopback server.
			resp, err := http.Get(authURL)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	serverURL := as.URL + "/mcp"
	if err := m.Connect(context.Background(), "notion", serverURL, ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if !m.IsConnected("notion") {
		t.Fatal("connector should be connected after Connect")
	}
	// The auth header func yields the bearer.
	fn, ok := m.AuthHeaderFunc("notion", serverURL)
	if !ok {
		t.Fatal("AuthHeaderFunc should report connected")
	}
	if got := fn(); got != "Bearer access-tok-1" {
		t.Fatalf("auth header = %q, want Bearer access-tok-1", got)
	}

	// PKCE challenge must have been sent to /authorize.
	if as.challenge == "" {
		t.Error("no PKCE code_challenge sent to authorize endpoint")
	}

	// Status reflects connection.
	sts, err := m.Statuses()
	if err != nil {
		t.Fatal(err)
	}
	if len(sts) != 1 || !sts[0].Connected || sts[0].Name != "notion" {
		t.Fatalf("unexpected statuses: %+v", sts)
	}

	// Persistence: a fresh Manager over the same file sees the connection.
	m2 := NewManagerWithStore(newFileStore(filepath.Join(dir, "connectors.json")))
	if !m2.IsConnected("notion") {
		t.Fatal("connection should persist across Manager instances")
	}

	// Disconnect clears it.
	if err := m.Disconnect("notion"); err != nil {
		t.Fatal(err)
	}
	if m.IsConnected("notion") {
		t.Fatal("connector should be disconnected")
	}
}

func TestParseWWWAuthenticate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`Bearer realm="mcp", resource_metadata="https://h/.well-known/oauth-protected-resource"`, "https://h/.well-known/oauth-protected-resource"},
		{`Bearer resource_metadata="https://h/rm", error="invalid_token"`, "https://h/rm"},
		{`Bearer realm="x"`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := parseWWWAuthenticate(c.in); got != c.want {
			t.Errorf("parseWWWAuthenticate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDiscoverDerivesWellKnown(t *testing.T) {
	as := newFakeAuthServer(t)
	defer as.Close()
	// No resource_metadata hint: discover must derive the well-known path from the
	// server origin.
	asm, prm, err := discover(context.Background(), http.DefaultClient, as.URL+"/mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	if asm.TokenEndpoint != as.URL+"/token" {
		t.Errorf("token endpoint = %q", asm.TokenEndpoint)
	}
	if prm == nil || prm.Resource != as.URL+"/mcp" {
		t.Errorf("protected-resource metadata not resolved: %+v", prm)
	}
}
