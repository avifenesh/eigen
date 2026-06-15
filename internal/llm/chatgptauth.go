package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ChatGPT-subscription auth for gpt-5.5.
//
// The Bedrock "mantle" gateway's openai.gpt-5.5 engine is unreliable (HTTP 500
// "Engine not found"), so eigen reaches gpt-5.5 the way the codex CLI does:
// directly against the ChatGPT backend (chatgpt.com/backend-api/codex) using the
// ChatGPT-plan OAuth token in ~/.codex/auth.json. This keeps eigen SELF-
// CONTAINED — a native Go path, no external bridge process — depending only on
// the user's existing `codex login` credential file (like AWS creds for Bedrock).

const (
	chatgptCodexBaseURL  = "https://chatgpt.com/backend-api/codex"
	chatgptOAuthTokenURL = "https://auth.openai.com/oauth/token"
	chatgptClientID      = "app_EMoamEEZ73f0CkXaXp7hrann" // codex CLI OAuth client
)

// codexAuth is the subset of ~/.codex/auth.json we use.
type codexAuth struct {
	AuthMode string `json:"auth_mode"`
	Tokens   struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// chatgptAuth caches the parsed credential and serializes refreshes.
type chatgptAuth struct {
	mu      sync.Mutex
	loaded  bool
	access  string
	refresh string
	account string
	exp     time.Time
}

var sharedChatGPTAuth = &chatgptAuth{}

func codexAuthPath() string {
	if h := strings.TrimSpace(os.Getenv("CODEX_HOME")); h != "" {
		return filepath.Join(h, "auth.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "auth.json")
}

// chatgptAvailable reports whether a usable ChatGPT-plan credential exists.
func chatgptAvailable() bool {
	data, err := os.ReadFile(codexAuthPath())
	if err != nil {
		return false
	}
	var a codexAuth
	if json.Unmarshal(data, &a) != nil {
		return false
	}
	return a.AuthMode == "chatgpt" && a.Tokens.AccessToken != ""
}

// load reads auth.json into the cache (once, unless forced).
func (c *chatgptAuth) load(force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded && !force {
		return nil
	}
	data, err := os.ReadFile(codexAuthPath())
	if err != nil {
		return fmt.Errorf("read codex auth: %w", err)
	}
	var a codexAuth
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("parse codex auth: %w", err)
	}
	if a.AuthMode != "chatgpt" || a.Tokens.AccessToken == "" {
		return fmt.Errorf("codex auth is not a ChatGPT login (run `codex login`)")
	}
	c.access = a.Tokens.AccessToken
	c.refresh = a.Tokens.RefreshToken
	c.account = a.Tokens.AccountID
	c.exp = jwtExpiry(a.Tokens.AccessToken)
	c.loaded = true
	return nil
}

// headers returns the auth headers for a ChatGPT-backend Responses call,
// refreshing the token first if it's expired/near-expiry.
func (c *chatgptAuth) headers(ctx context.Context, hc *http.Client) (map[string]string, error) {
	if err := c.load(false); err != nil {
		return nil, err
	}
	c.mu.Lock()
	access, account, exp, refresh := c.access, c.account, c.exp, c.refresh
	c.mu.Unlock()
	// Refresh when within 2 minutes of expiry (or already expired).
	if refresh != "" && !exp.IsZero() && time.Until(exp) < 2*time.Minute {
		if err := c.doRefresh(ctx, hc); err == nil {
			c.mu.Lock()
			access, account = c.access, c.account
			c.mu.Unlock()
		}
		// On refresh failure, fall through and try the (possibly stale) token —
		// the backend will 401 and the caller can surface a clear message.
	}
	h := map[string]string{
		"Authorization": "Bearer " + access,
		"Content-Type":  "application/json",
		"OpenAI-Beta":   "responses=experimental",
		"originator":    "codex_cli_rs",
	}
	if account != "" {
		h["ChatGPT-Account-ID"] = account
	}
	return h, nil
}

// refreshNow forces a token refresh (called on a 401 from the backend).
func (c *chatgptAuth) refreshNow(ctx context.Context, hc *http.Client) error {
	if err := c.load(false); err != nil {
		return err
	}
	return c.doRefresh(ctx, hc)
}

// doRefresh exchanges the refresh token for a new access token and writes it
// back to auth.json (so codex + eigen stay in sync). Serialized by the caller's
// lock discipline: it takes c.mu only around field updates.
func (c *chatgptAuth) doRefresh(ctx context.Context, hc *http.Client) error {
	c.mu.Lock()
	refresh := c.refresh
	c.mu.Unlock()
	if refresh == "" {
		return fmt.Errorf("no refresh token in codex auth")
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     chatgptClientID,
		"refresh_token": refresh,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptOAuthTokenURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != 200 {
		return fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, truncErr(raw))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("parse refresh response: %w", err)
	}
	if out.AccessToken == "" {
		return fmt.Errorf("refresh response had no access_token")
	}
	c.mu.Lock()
	c.access = out.AccessToken
	if out.RefreshToken != "" {
		c.refresh = out.RefreshToken
	}
	c.exp = jwtExpiry(out.AccessToken)
	c.mu.Unlock()
	c.writeBack(out.AccessToken, out.RefreshToken, out.IDToken)
	return nil
}

// writeBack persists refreshed tokens to auth.json (best-effort; preserves all
// other fields by read-modify-write).
func (c *chatgptAuth) writeBack(access, refresh, idTok string) {
	path := codexAuthPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return
	}
	tokens, _ := m["tokens"].(map[string]any)
	if tokens == nil {
		tokens = map[string]any{}
	}
	tokens["access_token"] = access
	if refresh != "" {
		tokens["refresh_token"] = refresh
	}
	if idTok != "" {
		tokens["id_token"] = idTok
	}
	m["tokens"] = tokens
	m["last_refresh"] = time.Now().UTC().Format(time.RFC3339Nano)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	// Atomic-ish write preserving 0600.
	tmp := path + ".eigen.tmp"
	if os.WriteFile(tmp, out, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// jwtExpiry reads the exp claim from a JWT access token (zero if unreadable).
func jwtExpiry(tok string) time.Time {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens pad; try standard.
		if p, e2 := base64.URLEncoding.DecodeString(parts[1] + strings.Repeat("=", (4-len(parts[1])%4)%4)); e2 == nil {
			payload = p
		} else {
			return time.Time{}
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}
