package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Codex drives an OpenAI model through the **Codex** backend — the same path
// the `codex` CLI uses: the Responses API over chatgpt.com/backend-api with a
// ChatGPT-account OAuth token (NOT api.openai.com / OPENAI_API_KEY). It reuses
// the Responses request/reply shapes + SSE parsing from the mantle provider
// (same wire API), swapping auth + base URL and adding the service_tier "fast
// mode" knob.
//
// Auth: ~/.codex/auth.json {auth_mode:"chatgpt", tokens:{access_token,
// refresh_token, account_id, id_token}}. The access_token is sent as a Bearer;
// account_id rides in the ChatGPT-Account-Id header. On a 401 the token is
// refreshed via auth.openai.com/oauth/token (refresh_token grant) and the file
// is rewritten — mirroring the codex CLI.
type Codex struct {
	BaseURL  string
	Model    string
	effort   string
	tier     string // service_tier: "priority" (fast) | "flex" | "" (default)
	authPath string
	oauthURL string // OAuth token endpoint (override for tests)

	mu        sync.Mutex
	token     string // access_token
	refresh   string // refresh_token
	accountID string
	http      *http.Client
}

const (
	// codexBaseURL is the Codex backend the CLI targets (Responses API).
	codexBaseURL = "https://chatgpt.com/backend-api/codex"
	// codexOAuthTokenURL is the OAuth token endpoint for refresh.
	codexOAuthTokenURL = "https://auth.openai.com/oauth/token"
	// codexClientID is the public Codex CLI OAuth client id (from the binary).
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// codexAuth is the on-disk ~/.codex/auth.json shape (only the fields we use).
type codexAuth struct {
	AuthMode string `json:"auth_mode,omitempty"`
	APIKey   string `json:"OPENAI_API_KEY,omitempty"`
	Tokens   struct {
		IDToken      string `json:"id_token,omitempty"`
		AccessToken  string `json:"access_token,omitempty"`
		RefreshToken string `json:"refresh_token,omitempty"`
		AccountID    string `json:"account_id,omitempty"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh,omitempty"`
}

// codexAuthPath is ~/.codex/auth.json (EIGEN_CODEX_AUTH overrides).
func codexAuthPath() string {
	if p := strings.TrimSpace(os.Getenv("EIGEN_CODEX_AUTH")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
}

// NewCodex builds the Codex provider from ~/.codex/auth.json. Requires a
// ChatGPT-account login (codex login); API-key-only auth.json is rejected (that
// path is api.openai.com, which is the mantle/openai story, not Codex).
func NewCodex(model string) (*Codex, error) {
	if model == "" {
		model = "gpt-5.5"
	}
	path := codexAuthPath()
	auth, err := readCodexAuth(path)
	if err != nil {
		return nil, fmt.Errorf("no Codex credentials at %s (run `codex login`): %w", path, err)
	}
	if auth.Tokens.AccessToken == "" {
		return nil, fmt.Errorf("Codex auth at %s has no ChatGPT access token (run `codex login` — API-key-only auth uses the mantle/openai provider, not codex)", path)
	}
	c := &Codex{
		BaseURL:   firstNonEmpty(os.Getenv("EIGEN_CODEX_BASE_URL"), codexBaseURL),
		Model:     model,
		authPath:  path,
		oauthURL:  firstNonEmpty(os.Getenv("EIGEN_CODEX_OAUTH_URL"), codexOAuthTokenURL),
		token:     auth.Tokens.AccessToken,
		refresh:   auth.Tokens.RefreshToken,
		accountID: auth.Tokens.AccountID,
		effort:    reasoningEffort,
		http:      &http.Client{Timeout: 5 * time.Minute},
	}
	// Per-model default effort + service tier from the catalog.
	if info, ok := Lookup(model); ok {
		if info.Effort != "" {
			c.effort = info.Effort
		}
		c.tier = info.ServiceTier
	}
	// EIGEN_REASONING_EFFORT applies only if the model accepts it.
	if e := strings.TrimSpace(os.Getenv("EIGEN_REASONING_EFFORT")); e != "" {
		if levels := ModelEffortLevels(model); effortSupported(e, levels) {
			c.effort = e
		}
	}
	// Fast mode: env override of the service tier (priority|flex|off|"").
	if t := strings.TrimSpace(os.Getenv("EIGEN_CODEX_SERVICE_TIER")); t != "" {
		c.tier = normalizeTier(t)
	}
	return c, nil
}

func (c *Codex) Name() string    { return c.Model + " (codex)" }
func (c *Codex) ModelID() string { return c.Model }

// SetEffort changes reasoning effort if the model supports the level.
func (c *Codex) SetEffort(level string) bool {
	levels := ModelEffortLevels(c.Model)
	if len(levels) == 0 {
		levels = EffortLevels
	}
	if !effortSupported(level, levels) {
		return false
	}
	c.mu.Lock()
	c.effort = level
	c.mu.Unlock()
	return true
}

// Effort returns the current reasoning effort.
func (c *Codex) Effort() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.effort
}

// FastMode reports whether the fast (priority) service tier is active.
func (c *Codex) FastMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tier == "priority"
}

// SetFast toggles the fast/priority service tier on or off (off → backend
// default, i.e. no service_tier sent). Returns the new state.
func (c *Codex) SetFast(on bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		c.tier = "priority"
	} else {
		c.tier = ""
	}
	return on
}

// normalizeTier maps user-facing aliases to the wire value. "fast" → priority.
func normalizeTier(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "fast", "priority":
		return "priority"
	case "flex":
		return "flex"
	case "off", "default", "none", "":
		return ""
	default:
		return strings.ToLower(t)
	}
}

func (c *Codex) snapshot() (token, account, tier, effort string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, c.accountID, c.tier, c.effort
}

func (c *Codex) buildPayload(req Request, stream bool) responsesRequest {
	_, _, tier, effort := c.snapshot()
	// Codex requires the system prompt in the top-level `instructions` field
	// (the backend rejects a request without it: "Instructions are required").
	// So we pull System out of the message stream and pass it separately —
	// unlike mantle, which carries it as a developer-role input item.
	sys := req.System
	noSys := req
	noSys.System = ""
	storeFalse := false
	return responsesRequest{
		Model:        c.Model,
		Instructions: sys,
		Input:        buildInput(noSys),
		Tools:        toResponsesTools(req.Tools),
		Reasoning:    &reasoningConfig{Effort: effort, Summary: reasoningSummary},
		ServiceTier:  tier,
		Store:        &storeFalse,                             // Codex requires store:false
		Include:      []string{"reasoning.encrypted_content"}, // carry reasoning across turns
		Stream:       stream,
	}
}

// headers builds the Codex request headers (Bearer + account id + beta flag).
func (c *Codex) headers() map[string]string {
	token, account, _, _ := c.snapshot()
	h := map[string]string{
		"Authorization": "Bearer " + token,
		"OpenAI-Beta":   "responses=experimental",
		"originator":    "codex_cli_rs",
	}
	if account != "" {
		h["ChatGPT-Account-Id"] = account
	}
	return h
}

func (c *Codex) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	// The Codex backend is STREAM-ONLY (it 400s "Stream must be set to true" on
	// a non-stream request), so Complete drives the SSE path with a nil sink and
	// returns the assembled final response.
	return c.Stream(ctx, req, nil)
}

// Stream runs a streamed completion over SSE, reusing the mantle SSE assembler.
func (c *Codex) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := json.Marshal(c.buildPayload(req, true))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	for attempt := 0; ; attempt++ {
		emitted := false
		wrappedSink := sink
		if sink != nil {
			wrappedSink = func(c StreamChunk) {
				emitted = true
				sink(c)
			}
		}
		resp, err := c.openStream(ctx, body)
		if err != nil {
			return nil, err
		}
		out, err := parseResponsesSSE(resp, wrappedSink)
		if err == nil {
			return out, nil
		}
		// Codex often reports transient backend failures as a response.failed SSE
		// event over HTTP 200. If NOTHING was streamed to the user yet, retrying is
		// safe and avoids killing the turn. Once deltas have been emitted, surface
		// the failure to avoid duplicating visible text/reasoning.
		if !emitted && isTransientCodexStreamFailure(err) && attempt < maxStreamFailRetries {
			if berr := sleepBackoff(ctx, attempt+1, 0); berr != nil {
				return nil, berr
			}
			continue
		}
		return nil, err
	}
}

func (c *Codex) openStream(ctx context.Context, body []byte) (*http.Response, error) {
	resp, err := httpStream(ctx, c.http, c.BaseURL+"/responses", c.headers(), body, nil)
	if err != nil {
		// httpStream returns a non-2xx as an error ("HTTP 401: …"), not a
		// response. On a 401 (expired access token) refresh once and retry.
		if isUnauthorized(err) {
			if rerr := c.refreshToken(ctx); rerr != nil {
				return nil, fmt.Errorf("codex auth expired and refresh failed: %w", rerr)
			}
			resp, err = httpStream(ctx, c.http, c.BaseURL+"/responses", c.headers(), body, nil)
		}
		if err != nil {
			return nil, fmt.Errorf("codex: %w", err)
		}
	}
	return resp, nil
}

func isTransientCodexStreamFailure(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "codex stream failed: server_error") ||
		strings.Contains(s, "codex stream failed: rate_limit") ||
		strings.Contains(s, "codex stream failed: overloaded")
}

// isUnauthorized reports whether an httpStream error is a 401 (expired token).
func isUnauthorized(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 401")
}

// refreshToken exchanges the refresh_token for a new access_token and persists
// it back to auth.json (matching the codex CLI's rotation). Codex uses ROTATING
// refresh tokens: each refresh call returns a NEW refresh_token and invalidates
// the old one. If two processes refresh concurrently (two GUI windows, CLI + GUI)
// the loser's write clobbers the winner's rotated token → both invalidated → user
// forced to re-login. We guard the critical section with an flock on auth.json.lock:
// acquire → RE-CHECK freshness by re-reading auth.json (the other process may have
// already refreshed while we waited for the lock) → only refresh+write if STILL
// stale → release. The re-check after acquiring is essential: without it a second
// process that sees "token expired" before the first's refresh completes will
// still issue its own refresh once it gets the lock, clobbering the first's new
// rotated token.
func (c *Codex) refreshToken(ctx context.Context) error {
	c.mu.Lock()
	refresh := c.refresh
	c.mu.Unlock()
	if refresh == "" {
		return fmt.Errorf("no refresh token (run `codex login`)")
	}
	return c.refreshWithLock(ctx, refresh)
}

// refreshWithLock guards the refresh critical section: acquire flock → re-check
// freshness → refresh+write only if still stale. Factored out for testing the
// lock+recheck logic with a fake refresh function.
func (c *Codex) refreshWithLock(ctx context.Context, refresh string) error {
	if c.authPath == "" {
		return fmt.Errorf("no auth path (cannot lock)")
	}
	// Acquire an exclusive flock on auth.json.lock. This blocks if another
	// process is mid-refresh; once we acquire it the other process has finished
	// writing auth.json and we should re-check before issuing our own refresh.
	lockPath := c.authPath + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockFile.Close()
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("acquire flock: %w", err)
	}
	defer unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
	// RE-CHECK: the other process may have already refreshed while we waited.
	// Read auth.json again and see if the tokens changed (last_refresh updated
	// or access_token changed). If so, load the new tokens and skip our refresh.
	// This is the essential concurrency guard: without it we'd refresh with the
	// OLD refresh_token (now invalidated by the first process's rotation) and
	// clobber the NEW tokens.
	auth, err := readCodexAuth(c.authPath)
	if err == nil {
		c.mu.Lock()
		// If the access token on disk is DIFFERENT from what we last loaded, the
		// other process refreshed it — adopt the new tokens and skip refresh.
		if auth.Tokens.AccessToken != "" && auth.Tokens.AccessToken != c.token {
			c.token = auth.Tokens.AccessToken
			if auth.Tokens.RefreshToken != "" {
				c.refresh = auth.Tokens.RefreshToken
			}
			if auth.Tokens.AccountID != "" {
				c.accountID = auth.Tokens.AccountID
			}
			c.mu.Unlock()
			return nil // other process already refreshed; we're done
		}
		c.mu.Unlock()
	}
	// Still stale (no concurrent refresh updated auth.json while we waited for
	// the lock) — proceed with our own refresh + write.
	return c.doRefresh(ctx, refresh)
}

// doRefresh performs the actual OAuth token exchange + persist, extracted so
// the lock+recheck logic above doesn't hold the flock during the network call
// (we already hold it here, but the factoring makes testing simpler).
func (c *Codex) doRefresh(ctx context.Context, refresh string) error {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {codexClientID},
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oauthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("oauth token refresh HTTP %d", resp.StatusCode)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return err
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("oauth refresh returned no access_token")
	}
	c.mu.Lock()
	c.token = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refresh = tok.RefreshToken
	}
	c.mu.Unlock()
	c.persist(tok.AccessToken, tok.RefreshToken, tok.IDToken)
	return nil
}

// persist rewrites the access/refresh/id tokens into auth.json (best-effort,
// preserving other fields).
func (c *Codex) persist(access, refresh, id string) {
	if c.authPath == "" {
		return
	}
	auth, err := readCodexAuth(c.authPath)
	if err != nil {
		auth = &codexAuth{AuthMode: "chatgpt"}
	}
	auth.Tokens.AccessToken = access
	if refresh != "" {
		auth.Tokens.RefreshToken = refresh
	}
	if id != "" {
		auth.Tokens.IDToken = id
	}
	auth.LastRefresh = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return
	}
	tmp := c.authPath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, c.authPath)
	}
}

func readCodexAuth(path string) (*codexAuth, error) {
	if path == "" {
		return nil, fmt.Errorf("no auth path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a codexAuth
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// parseResponsesSSE assembles a Responses-API SSE stream into a final Response,
// forwarding text/reasoning deltas to sink. It collects the authoritative output
// from `response.output_item.done` events (the Codex backend delivers tool calls
// and the final message there; its `response.completed` event carries an EMPTY
// output array). Text/reasoning deltas are accumulated for streaming + as a
// fallback. This is the single source for both the Codex and mantle wire shapes.
func parseResponsesSSE(resp *http.Response, sink StreamSink) (*Response, error) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	out := &Response{}
	var sbText, sbReason strings.Builder
	var completedReply *Response // from response.completed (mantle fills output; codex empty)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Item     json.RawMessage `json:"item"`
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			sbText.WriteString(ev.Delta)
			if sink != nil {
				sink(StreamChunk{Kind: ChunkText, Text: ev.Delta})
			}
		case "response.reasoning_summary_text.delta":
			sbReason.WriteString(ev.Delta)
			if sink != nil {
				sink(StreamChunk{Kind: ChunkReasoning, Text: ev.Delta})
			}
		case "response.output_item.done":
			// The authoritative output channel for Codex: a completed output
			// item — a function_call (tool use), a message (assistant text), or
			// a reasoning summary. Collect each into the final response.
			applyOutputItem(ev.Item, out)
		case "response.completed", "response.incomplete":
			r, status, reason, perr := parseReply(ev.Response)
			if perr != nil {
				return nil, perr
			}
			if status == "incomplete" {
				if reason == "" {
					reason = "unknown"
				}
				return nil, fmt.Errorf("codex response incomplete (%s): refusing possibly-truncated output", reason)
			}
			completedReply = r
		case "response.failed":
			if r := outputFromFailed(ev.Response); r != nil &&
				(strings.TrimSpace(r.Text) != "" || len(r.ToolCalls) > 0) {
				return r, nil
			}
			return nil, fmt.Errorf("codex stream failed: %s", streamFailReason(ev.Response))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	// Prefer output_item.done collection (Codex). If it yielded nothing but the
	// completed event carried output (mantle), use that. Then backfill
	// text/reasoning from the accumulated deltas, and carry usage.
	if len(out.ToolCalls) == 0 && strings.TrimSpace(out.Text) == "" && completedReply != nil {
		out = completedReply
	} else if completedReply != nil {
		out.Usage = completedReply.Usage // usage always lives on the completed event
	}
	if strings.TrimSpace(out.Text) == "" && sbText.Len() > 0 {
		out.Text = sbText.String()
	}
	if out.Reasoning == "" && sbReason.Len() > 0 {
		out.Reasoning = sbReason.String()
	}
	return out, nil
}

// applyOutputItem folds one response.output_item.done item into the response:
// a function_call becomes a ToolCall, a message's output_text becomes Text, a
// reasoning item's summary becomes Reasoning (with its id for cross-turn carry).
func applyOutputItem(raw json.RawMessage, out *Response) {
	if len(raw) == 0 {
		return
	}
	var item struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		CallID    string `json:"call_id"`
		Arguments string `json:"arguments"`
		Content   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
		Encrypted string `json:"encrypted_content"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	switch item.Type {
	case "function_call":
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        item.CallID,
			Name:      item.Name,
			Arguments: normalizeArgs(item.Arguments),
		})
	case "message":
		for _, p := range item.Content {
			if p.Type == "output_text" {
				out.Text += p.Text
			}
		}
	case "reasoning":
		// Each encrypted_content blob is bound to a SPECIFIC item id — the
		// server verifies they match ("Encrypted content item_id did not match
		// the target item id" = 400). So ReasoningID + ReasoningEncrypted must
		// come from the SAME reasoning item, always paired. A turn can emit
		// several reasoning items (xhigh/fast often does): we take the LAST one
		// with a blob (the most recent chain of thought), setting BOTH id and
		// blob from it together — never first-id + last-blob (that mismatch 400s).
		if item.Encrypted != "" {
			out.ReasoningEncrypted = item.Encrypted
			out.ReasoningID = item.ID // pair the id with THIS blob
		} else if out.ReasoningID == "" {
			out.ReasoningID = item.ID
		}
		for _, s := range item.Summary {
			if s.Text != "" {
				if out.Reasoning != "" {
					out.Reasoning += "\n"
				}
				out.Reasoning += s.Text
			}
		}
	}
}
