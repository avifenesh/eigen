package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CustomProvider is a user-added provider stored in ~/.eigen/providers.json.
// It describes the provider's wire protocol and the exact model catalog Eigen
// should expose for that provider. API keys are referenced by env var by
// default so the catalog can be committed/exported without secrets.
type CustomProvider struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"` // openai | openai_chat | openai_responses | anthropic | ant
	BaseURL   string        `json:"base_url"`
	API       string        `json:"api,omitempty"` // chat | responses (OpenAI-compatible only)
	APIKeyEnv string        `json:"api_key_env,omitempty"`
	APIKey    string        `json:"api_key,omitempty"` // supported for private local configs; app does not write it
	NoAuth    bool          `json:"no_auth,omitempty"` // explicit no-auth local endpoint
	Version   string        `json:"version,omitempty"` // Anthropic API version (default 2023-06-01)
	Models    []CustomModel `json:"models"`
}

// CustomModel is one user-facing model alias in a custom provider catalog.
// Name is what Eigen shows and accepts in /model. ID is the wire id sent to the
// endpoint; when empty, Name is sent.
type CustomModel struct {
	Name          string   `json:"name"`
	ID            string   `json:"id,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Reasoning     bool     `json:"reasoning,omitempty"`
	Effort        string   `json:"effort,omitempty"`
	EffortLevels  []string `json:"effort_levels,omitempty"`
	Vision        bool     `json:"vision,omitempty"`
	Search        bool     `json:"search,omitempty"`
	Social        bool     `json:"social,omitempty"`
}

type customProviderFile struct {
	Providers []CustomProvider `json:"providers"`
}

// CustomProvidersPath returns the per-user custom provider catalog path.
func CustomProvidersPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "providers.json")
}

// LoadCustomProviders reads ~/.eigen/providers.json. Missing file is normal.
func LoadCustomProviders() ([]CustomProvider, error) {
	p := CustomProvidersPath()
	if p == "" {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f customProviderFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	providers := normalizeCustomProviders(f.Providers)
	if err := validateCustomCatalog(providers); err != nil {
		return nil, err
	}
	return providers, nil
}

// SaveCustomProviders writes the complete custom provider catalog.
func SaveCustomProviders(providers []CustomProvider) error {
	p := CustomProvidersPath()
	if p == "" {
		return os.ErrNotExist
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	f := customProviderFile{Providers: normalizeCustomProviders(providers)}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".providers-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		return err
	}
	ok = true
	return nil
}

// UpsertCustomProvider validates and inserts/replaces one custom provider.
func UpsertCustomProvider(p CustomProvider) error {
	if err := ValidateCustomProvider(p); err != nil {
		return err
	}
	providers, err := LoadCustomProviders()
	if err != nil {
		return err
	}
	p = normalizeCustomProvider(p)
	for i := range providers {
		if providers[i].Name == p.Name {
			providers[i] = mergeCustomProvider(providers[i], p)
			if err := validateCustomCatalog(providers); err != nil {
				return err
			}
			return SaveCustomProviders(providers)
		}
	}
	providers = append(providers, p)
	if err := validateCustomCatalog(providers); err != nil {
		return err
	}
	return SaveCustomProviders(providers)
}

func validateCustomCatalog(providers []CustomProvider) error {
	seenModels := map[string]string{}
	for _, p := range providers {
		if err := ValidateCustomProvider(p); err != nil {
			name := p.Name
			if name == "" {
				name = "(unnamed)"
			}
			return fmt.Errorf("custom provider %s: %w", name, err)
		}
		for _, m := range p.Models {
			m = normalizeCustomModel(m)
			if owner, ok := seenModels[m.Name]; ok && owner != p.Name {
				return fmt.Errorf("custom model name %q appears in both %s and %s", m.Name, owner, p.Name)
			}
			seenModels[m.Name] = p.Name
		}
	}
	return nil
}

// ValidateCustomProvider checks the public, user-editable provider catalog.
func ValidateCustomProvider(p CustomProvider) error {
	p = normalizeCustomProvider(p)
	if p.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	if strings.ContainsAny(p.Name, " \t\n:/") {
		return fmt.Errorf("provider name must not contain whitespace, ':' or '/'")
	}
	if isBuiltinProvider(p.Name) {
		return fmt.Errorf("provider name %q is reserved", p.Name)
	}
	if p.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if err := validateCustomBaseURL(p); err != nil {
		return err
	}
	switch customKind(p) {
	case "openai_chat", "openai_responses", "anthropic":
	default:
		return fmt.Errorf("type/api must be openai chat, openai responses, or anthropic")
	}
	if len(p.Models) == 0 {
		return fmt.Errorf("at least one model is required")
	}
	seen := map[string]bool{}
	for _, m := range p.Models {
		m = normalizeCustomModel(m)
		if m.Name == "" {
			return fmt.Errorf("model name is required")
		}
		if _, ok := lookupBuiltinModel(m.Name); ok {
			return fmt.Errorf("model name %q collides with a built-in catalog model", m.Name)
		}
		if strings.Contains(m.Name, ":") {
			return fmt.Errorf("model name %q must not contain ':'", m.Name)
		}
		if seen[m.Name] {
			return fmt.Errorf("duplicate model name %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}

func mergeCustomProvider(old, next CustomProvider) CustomProvider {
	old = normalizeCustomProvider(old)
	next = normalizeCustomProvider(next)
	merged := next
	byName := map[string]CustomModel{}
	var order []string
	add := func(m CustomModel) {
		m = normalizeCustomModel(m)
		if m.Name == "" {
			return
		}
		if _, ok := byName[m.Name]; !ok {
			order = append(order, m.Name)
		}
		byName[m.Name] = m
	}
	for _, m := range old.Models {
		add(m)
	}
	for _, m := range next.Models {
		add(m)
	}
	merged.Models = merged.Models[:0]
	for _, name := range order {
		merged.Models = append(merged.Models, byName[name])
	}
	return merged
}

func lookupBuiltinModel(id string) (ModelInfo, bool) {
	for _, m := range Catalog {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

func validateCustomBaseURL(p CustomProvider) error {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("base_url must be an http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base_url must use http or https")
	}
	if u.Scheme == "http" && !p.NoAuth && (p.APIKey != "" || p.APIKeyEnv != "") && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("refusing to send credentials to non-loopback http endpoint; use https or mark the provider no_auth")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func normalizeCustomProviders(in []CustomProvider) []CustomProvider {
	out := make([]CustomProvider, 0, len(in))
	seen := map[string]bool{}
	for _, p := range in {
		p = normalizeCustomProvider(p)
		if p.Name == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out
}

func normalizeCustomProvider(p CustomProvider) CustomProvider {
	p.Name = strings.TrimSpace(p.Name)
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	p.API = strings.ToLower(strings.TrimSpace(p.API))
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	p.APIKeyEnv = strings.TrimSpace(p.APIKeyEnv)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.Version = strings.TrimSpace(p.Version)
	for i := range p.Models {
		p.Models[i] = normalizeCustomModel(p.Models[i])
	}
	return p
}

func normalizeCustomModel(m CustomModel) CustomModel {
	m.Name = strings.TrimSpace(m.Name)
	m.ID = strings.TrimSpace(m.ID)
	m.Effort = strings.TrimSpace(m.Effort)
	if m.ID == "" {
		m.ID = m.Name
	}
	return m
}

func customKind(p CustomProvider) string {
	t := strings.ToLower(strings.TrimSpace(p.Type))
	api := strings.ToLower(strings.TrimSpace(p.API))
	switch t {
	case "anthropic", "ant":
		return "anthropic"
	case "openai_responses", "responses":
		return "openai_responses"
	case "openai_chat", "chat":
		return "openai_chat"
	case "openai", "openai-compatible", "openai_compatible":
		if api == "responses" || api == "openai_responses" {
			return "openai_responses"
		}
		return "openai_chat"
	}
	return t
}

func isBuiltinProvider(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai", "openai_chat", "openai_responses", "chat", "responses", "custom":
		return true
	}
	switch canonicalProvider(name) {
	case "mantle", "converse", "anthropic", "codex", "llama", "grok", "glm", "moa":
		return true
	}
	return false
}

func customProviderByName(name string) (CustomProvider, bool) {
	providers, err := LoadCustomProviders()
	if err != nil {
		return CustomProvider{}, false
	}
	for _, p := range providers {
		if p.Name == name {
			return p, true
		}
	}
	return CustomProvider{}, false
}

func customModelByName(model string) (CustomProvider, CustomModel, bool) {
	providers, err := LoadCustomProviders()
	if err != nil {
		return CustomProvider{}, CustomModel{}, false
	}
	for _, p := range providers {
		for _, m := range p.Models {
			if m.Name == model {
				return p, normalizeCustomModel(m), true
			}
		}
	}
	return CustomProvider{}, CustomModel{}, false
}

func customModels() []ModelInfo {
	providers, err := LoadCustomProviders()
	if err != nil {
		return nil
	}
	var out []ModelInfo
	for _, p := range providers {
		for _, cm := range p.Models {
			cm = normalizeCustomModel(cm)
			if cm.Name == "" {
				continue
			}
			win := cm.ContextWindow
			if win == 0 {
				win = defaultCustomWindow(customKind(p))
			}
			out = append(out, ModelInfo{
				ID:            cm.Name,
				Provider:      p.Name,
				ContextWindow: win,
				Reasoning:     cm.Reasoning,
				Effort:        cm.Effort,
				EffortLevels:  cm.EffortLevels,
				Vision:        cm.Vision,
				Search:        cm.Search,
				Social:        cm.Social,
			})
		}
	}
	return out
}

func defaultCustomWindow(kind string) int {
	switch kind {
	case "anthropic":
		return 200000
	default:
		return 128000
	}
}

func customProviderAvailable(p CustomProvider) bool {
	p = normalizeCustomProvider(p)
	if p.BaseURL == "" {
		return false
	}
	if p.NoAuth {
		return true
	}
	if p.APIKey != "" {
		return true
	}
	if p.APIKeyEnv == "" {
		return false
	}
	return strings.TrimSpace(os.Getenv(p.APIKeyEnv)) != ""
}

func customAPIKey(p CustomProvider) string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		return strings.TrimSpace(os.Getenv(p.APIKeyEnv))
	}
	return ""
}

func newCustomProvider(providerName, modelName string) (Provider, error) {
	p, ok := customProviderByName(providerName)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (want: mantle | llama | converse | anthropic | codex | grok | glm or a custom provider in %s)", providerName, CustomProvidersPath())
	}
	var cm CustomModel
	if modelName == "" {
		if len(p.Models) == 0 {
			return nil, fmt.Errorf("custom provider %q has no models", p.Name)
		}
		cm = normalizeCustomModel(p.Models[0])
	} else {
		for _, m := range p.Models {
			m = normalizeCustomModel(m)
			if m.Name == modelName || m.ID == modelName {
				cm = m
				break
			}
		}
		if cm.Name == "" {
			return nil, fmt.Errorf("model %q is not in custom provider %q catalog", modelName, p.Name)
		}
	}
	switch customKind(p) {
	case "openai_chat":
		return newCustomOpenAIChat(p, cm), nil
	case "openai_responses":
		return newCustomOpenAIResponses(p, cm), nil
	case "anthropic":
		return newCustomAnthropic(p, cm), nil
	default:
		return nil, fmt.Errorf("custom provider %q has unsupported type/api", p.Name)
	}
}

// customEffortLevels resolves the accepted effort labels for a custom reasoning
// model: the catalog-style EffortLevels when given, else the global fallback.
// Non-reasoning models accept nothing (effort is ignored).
func customEffortLevels(m CustomModel) []string {
	if !m.Reasoning {
		return nil
	}
	if len(m.EffortLevels) > 0 {
		return m.EffortLevels
	}
	return EffortLevels
}

// customEffort is the runtime reasoning-effort knob shared by all three custom
// provider kinds. It mirrors how built-in reasoning providers carry effort: the
// model's catalog default seeds it, EIGEN_REASONING_EFFORT overrides at startup
// (when the model accepts it), and SetEffort/Effort satisfy EffortSetter so a
// live /effort switch works without rebuilding the provider.
type customEffort struct {
	mu        sync.RWMutex
	reasoning bool
	levels    []string
	effort    string
}

func newCustomEffort(m CustomModel) *customEffort {
	e := &customEffort{reasoning: m.Reasoning, levels: customEffortLevels(m), effort: m.Effort}
	if env := strings.TrimSpace(os.Getenv("EIGEN_REASONING_EFFORT")); env != "" && effortSupported(env, e.levels) {
		e.effort = env
	}
	return e
}

// SetEffort changes the reasoning effort if the model supports the level.
func (e *customEffort) SetEffort(level string) bool {
	if !e.reasoning || !effortSupported(level, e.levels) {
		return false
	}
	e.mu.Lock()
	e.effort = level
	e.mu.Unlock()
	return true
}

// Effort returns the current reasoning-effort label ("" when not reasoning).
func (e *customEffort) Effort() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.effort
}

// snapshot returns the current effort label and whether it's an active reasoning
// request (a reasoning model with a non-empty, non-disabling effort).
func (e *customEffort) snapshot() (effort string, active bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	eff := strings.TrimSpace(e.effort)
	if !e.reasoning || eff == "" || eff == "off" || eff == "none" || eff == "minimal" {
		return eff, false
	}
	return eff, true
}

func normalizeOpenAIBase(base string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	for _, suffix := range []string{"/chat/completions", "/responses"} {
		b = strings.TrimSuffix(b, suffix)
	}
	return strings.TrimRight(b, "/")
}

func normalizeAnthropicBase(base string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	b = strings.TrimSuffix(b, "/messages")
	return strings.TrimRight(b, "/")
}

var (
	_ EffortSetter = (*customOpenAIChat)(nil)
	_ Streamer     = (*customOpenAIChat)(nil)
)

type customOpenAIChat struct {
	name  string
	model string
	wire  string
	c     *chatClient
	*customEffort
}

func newCustomOpenAIChat(p CustomProvider, m CustomModel) *customOpenAIChat {
	m = normalizeCustomModel(m)
	c := newChatClient(normalizeOpenAIBase(p.BaseURL), m.ID, customAPIKey(p), p.Name)
	cc := &customOpenAIChat{name: p.Name, model: m.Name, wire: m.ID, c: c, customEffort: newCustomEffort(m)}
	// Inject the chat-completions reasoning-effort field when set, the way GLM
	// injects its thinking field via the shared chatClient extra hook.
	c.extra = cc.bodyExtra
	return cc
}

// bodyExtra adds the OpenAI-compatible chat-completions reasoning-effort field
// for a reasoning custom model with an active effort. Nil otherwise.
func (p *customOpenAIChat) bodyExtra() map[string]any {
	if effort, active := p.snapshot(); active {
		return map[string]any{"reasoning_effort": effort}
	}
	return nil
}

func (p *customOpenAIChat) Name() string    { return p.model + " (" + p.name + " openai-chat)" }
func (p *customOpenAIChat) ModelID() string { return p.model }
func (p *customOpenAIChat) Complete(ctx context.Context, req Request) (*Response, error) {
	return p.c.complete(ctx, req)
}
func (p *customOpenAIChat) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	return p.c.stream(ctx, req, sink)
}

var (
	_ EffortSetter = (*customOpenAIResponses)(nil)
	_ Streamer     = (*customOpenAIResponses)(nil)
)

type customOpenAIResponses struct {
	name    string
	model   string
	wire    string
	baseURL string
	apiKey  string
	http    *http.Client
	*customEffort
}

func newCustomOpenAIResponses(p CustomProvider, m CustomModel) *customOpenAIResponses {
	m = normalizeCustomModel(m)
	return &customOpenAIResponses{name: p.Name, model: m.Name, wire: m.ID, baseURL: normalizeOpenAIBase(p.BaseURL), apiKey: customAPIKey(p), http: &http.Client{Timeout: 5 * time.Minute}, customEffort: newCustomEffort(m)}
}

func (p *customOpenAIResponses) Name() string    { return p.model + " (" + p.name + " openai-responses)" }
func (p *customOpenAIResponses) ModelID() string { return p.model }

// buildBody builds the Responses-API request JSON. stream sets stream:true so
// Complete and Stream share one request shape (only the flag differs), the way
// mantle's buildBody does.
func (p *customOpenAIResponses) buildBody(req Request, stream bool) ([]byte, error) {
	payload := responsesRequest{Model: p.wire, Input: buildInput(req), Tools: toResponsesTools(req.Tools), Stream: stream}
	// Apply reasoning effort the way mantle/codex do for the Responses API.
	if effort, active := p.snapshot(); active {
		payload.Reasoning = &reasoningConfig{Effort: effort, Summary: reasoningSummary}
	}
	return json.Marshal(payload)
}

func (p *customOpenAIResponses) headers() map[string]string {
	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	return headers
}

func (p *customOpenAIResponses) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := p.buildBody(req, false)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	// Custom Responses endpoints hit the same empty-completed-response quirk as
	// Bedrock Mantle, so re-request bounded by maxEmptyRetries the way
	// Mantle.Complete does rather than returning a do-nothing turn.
	for attempt := 0; ; attempt++ {
		raw, status, err := httpJSON(ctx, p.http, p.baseURL+"/responses", p.headers(), body, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("%s HTTP %d: %s", p.name, status, string(raw))
		}
		out, st, reason, err := parseReply(raw)
		if err != nil {
			return nil, err
		}
		if st == "incomplete" {
			if reason == "" {
				reason = "unknown"
			}
			return nil, fmt.Errorf("%s response incomplete (%s): refusing possibly-truncated output", p.name, reason)
		}
		if out.Text != "" || len(out.ToolCalls) > 0 || attempt >= maxEmptyRetries {
			return out, nil
		}
		// Empty completed response: re-request (the same quirk Mantle retries).
	}
}

// Stream POSTs stream:true to /responses and assembles the SSE with the shared
// parseResponsesSSE, forwarding text/reasoning deltas to sink as they arrive so
// user-defined Responses-API providers stay live mid-turn instead of blocking
// with no deltas (the Streamer check now succeeds for them).
func (p *customOpenAIResponses) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := p.buildBody(req, true)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	resp, err := httpStream(ctx, p.http, p.baseURL+"/responses", p.headers(), body, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.name, err)
	}
	return parseResponsesSSE(resp, sink)
}

var (
	_ EffortSetter = (*customAnthropic)(nil)
	_ Streamer     = (*customAnthropic)(nil)
)

type customAnthropic struct {
	name    string
	model   string
	wire    string
	baseURL string
	apiKey  string
	version string
	http    *http.Client
	*customEffort
}

func newCustomAnthropic(p CustomProvider, m CustomModel) *customAnthropic {
	m = normalizeCustomModel(m)
	version := p.Version
	if version == "" {
		version = anthropicVersion
	}
	return &customAnthropic{name: p.Name, model: m.Name, wire: m.ID, baseURL: normalizeAnthropicBase(p.BaseURL), apiKey: customAPIKey(p), version: version, http: &http.Client{Timeout: 5 * time.Minute}, customEffort: newCustomEffort(m)}
}

func (p *customAnthropic) Name() string    { return p.model + " (" + p.name + " anthropic)" }
func (p *customAnthropic) ModelID() string { return p.model }

// buildBody builds the native Messages request JSON. stream sets stream:true so
// Complete and Stream share one request shape (only the flag differs), mirroring
// the built-in Anthropic provider.
func (p *customAnthropic) buildBody(req Request, stream bool) ([]byte, error) {
	payload := anthropicRequest{
		Model:     p.wire,
		MaxTokens: anthropicMaxTok,
		Messages:  anthropicMessages(req),
		Tools:     anthropicTools(req.Tools, false),
		Stream:    stream,
	}
	// Adaptive thinking + output_config.effort, mirroring the built-in Anthropic
	// provider's effort path (api.anthropic.com/v1/messages).
	if effort, active := p.snapshot(); active {
		payload.Thinking = json.RawMessage(`{"type":"adaptive"}`)
		payload.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":%q}`, effort))
	}
	if strings.TrimSpace(req.System) != "" {
		payload.System = []anthropicTextBlock{{Type: "text", Text: req.System}}
	}
	return json.Marshal(payload)
}

func (p *customAnthropic) headers() map[string]string {
	headers := map[string]string{"anthropic-version": p.version}
	if p.apiKey != "" {
		headers["x-api-key"] = p.apiKey
	}
	return headers
}

func (p *customAnthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := p.buildBody(req, false)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, status, err := httpJSON(ctx, p.http, p.baseURL+"/messages", p.headers(), body, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.name, err)
	}
	var reply anthropicReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if status < 200 || status >= 300 {
		if reply.Error != nil {
			return nil, fmt.Errorf("%s HTTP %d: %s: %s", p.name, status, reply.Error.Type, reply.Error.Message)
		}
		return nil, fmt.Errorf("%s HTTP %d: %s", p.name, status, string(raw))
	}
	if reply.StopReason == "max_tokens" {
		return nil, fmt.Errorf("%s response truncated (max_tokens): refusing possibly-truncated output", p.name)
	}
	out := &Response{Usage: Usage{InputTokens: reply.Usage.InputTokens, OutputTokens: reply.Usage.OutputTokens, CacheReadTokens: reply.Usage.CacheReadInputTokens, CacheWriteTokens: reply.Usage.CacheCreationInputTokens}}
	for _, blk := range reply.Content {
		switch blk.Type {
		case "text":
			out.Text += blk.Text
		case "thinking":
			out.Reasoning += blk.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: blk.ID, Name: blk.Name, Arguments: normalizeArgsRaw(blk.Input)})
		}
	}
	return out, nil
}

// Stream POSTs stream:true to /messages and assembles the native Messages SSE
// with the shared parseAnthropicSSE, forwarding text/thinking deltas to sink as
// they arrive so user-defined Anthropic providers stay live mid-turn instead of
// blocking with no deltas (the Streamer check now succeeds for them).
func (p *customAnthropic) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := p.buildBody(req, true)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	resp, err := httpStream(ctx, p.http, p.baseURL+"/messages", p.headers(), body, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.name, err)
	}
	return parseAnthropicSSE(resp, p.name, sink)
}

// parseAnthropicSSE assembles a native Anthropic Messages SSE stream into a
// final Response, forwarding text/thinking deltas to sink as they arrive. The
// event flow is: message_start (usage), then per content block a
// content_block_start / content_block_delta* / content_block_stop, then one or
// more message_delta (stop_reason + cumulative output usage), then message_stop.
// tool_use input arrives as partial_json string deltas accumulated per index.
// The label prefixes errors so a custom provider's failures name that provider.
func parseAnthropicSSE(resp *http.Response, label string, sink StreamSink) (*Response, error) {
	defer resp.Body.Close()

	// One partial block per content index; kind+accumulators distinguish text,
	// thinking, and tool_use (whose input arrives as partial JSON strings).
	type partialBlock struct {
		kind     string // "text" | "thinking" | "tool_use"
		id, name string
		args     strings.Builder
	}
	byIndex := map[int]*partialBlock{}
	var order []int
	var text, reasoning strings.Builder
	var reasoningSig string // signature of the thinking block (interleaved)
	var usage Usage
	var stopReason string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // skip SSE "event:" lines and blanks; the data line is self-describing
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message *struct {
				Usage *anthropicStreamUsage `json:"usage"`
			} `json:"message"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage *anthropicStreamUsage `json:"usage"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "error":
			if ev.Error != nil {
				return nil, fmt.Errorf("%s stream error: %s: %s", label, ev.Error.Type, ev.Error.Message)
			}
			return nil, fmt.Errorf("%s stream error", label)
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				ev.Message.Usage.applyTo(&usage)
			}
		case "content_block_start":
			pb := &partialBlock{}
			if ev.ContentBlock != nil {
				pb.kind = ev.ContentBlock.Type
				pb.id = ev.ContentBlock.ID
				pb.name = ev.ContentBlock.Name
			}
			byIndex[ev.Index] = pb
			order = append(order, ev.Index)
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				text.WriteString(ev.Delta.Text)
				if sink != nil {
					sink(StreamChunk{Kind: ChunkText, Text: ev.Delta.Text})
				}
			case "thinking_delta":
				reasoning.WriteString(ev.Delta.Thinking)
				if sink != nil {
					sink(StreamChunk{Kind: ChunkReasoning, Text: ev.Delta.Thinking})
				}
			case "signature_delta":
				reasoningSig += ev.Delta.Signature
			case "input_json_delta":
				if pb := byIndex[ev.Index]; pb != nil {
					pb.args.WriteString(ev.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				ev.Usage.applyTo(&usage)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	// Refuse truncated output rather than applying it (parity with Complete).
	if stopReason == "max_tokens" {
		return nil, fmt.Errorf("%s response truncated (max_tokens): refusing possibly-truncated output", label)
	}

	out := &Response{Text: text.String(), Reasoning: reasoning.String(), ReasoningEncrypted: reasoningSig, Usage: usage}
	for _, idx := range order {
		pb := byIndex[idx]
		if pb == nil || pb.kind != "tool_use" {
			continue
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        pb.id,
			Name:      pb.name,
			Arguments: normalizeArgsRaw(json.RawMessage(pb.args.String())),
		})
	}
	return out, nil
}
