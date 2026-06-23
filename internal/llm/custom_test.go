package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCustomProviderCatalogAndLookup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := CustomProvider{
		Name:    "localai",
		Type:    "openai",
		API:     "chat",
		BaseURL: "http://127.0.0.1:9999/v1",
		NoAuth:  true,
		Models:  []CustomModel{{Name: "local-qwen", ID: "qwen-wire", ContextWindow: 32000}},
	}
	if err := UpsertCustomProvider(p); err != nil {
		t.Fatal(err)
	}
	providers, err := LoadCustomProviders()
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "localai" {
		t.Fatalf("providers wrong: %+v", providers)
	}
	if DefaultModel("localai") != "local-qwen" {
		t.Fatalf("default custom model wrong: %q", DefaultModel("localai"))
	}
	mi, ok := Lookup("local-qwen")
	if !ok || mi.Provider != "localai" || mi.ContextWindow != 32000 {
		t.Fatalf("custom model lookup wrong: %+v ok=%v", mi, ok)
	}
	if got := ResolveProvider("", "local-qwen"); got != "localai" {
		t.Fatalf("ResolveProvider custom = %q", got)
	}
	if !ProviderAvailable("localai") {
		t.Fatal("custom local no-auth provider should be available")
	}
	if got := AllCredentialedModels(); !containsString(got, "local-qwen") {
		t.Fatalf("credentialed models should include custom catalog model, got %v", got)
	}
	if _, err := New("localai", "not-in-catalog"); err == nil {
		t.Fatal("New should reject custom models outside the provider catalog")
	}
}

func TestCustomProviderUpsertMergesModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := UpsertCustomProvider(CustomProvider{Name: "lab", Type: "openai", BaseURL: "http://127.0.0.1:1/v1", NoAuth: true, Models: []CustomModel{{Name: "one", ID: "wire-one"}}}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCustomProvider(CustomProvider{Name: "lab", Type: "openai", BaseURL: "http://127.0.0.1:1/v1", NoAuth: true, Models: []CustomModel{{Name: "two", ID: "wire-two"}}}); err != nil {
		t.Fatal(err)
	}
	providers, err := LoadCustomProviders()
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || len(providers[0].Models) != 2 || providers[0].Models[0].Name != "one" || providers[0].Models[1].Name != "two" {
		t.Fatalf("upsert should merge model catalog, got %+v", providers)
	}
}

func TestCustomProviderValidationRejectsCollisionsAndUnsafeHTTP(t *testing.T) {
	if err := ValidateCustomProvider(CustomProvider{Name: "anthropic", Type: "openai", BaseURL: "https://example.com/v1", Models: []CustomModel{{Name: "m"}}}); err == nil {
		t.Fatal("builtin provider names should be rejected")
	}
	if err := ValidateCustomProvider(CustomProvider{Name: "lab", Type: "openai", BaseURL: "https://example.com/v1", Models: []CustomModel{{Name: Catalog[0].ID}}}); err == nil {
		t.Fatal("custom model aliases should not collide with built-in catalog ids")
	}
	if err := ValidateCustomProvider(CustomProvider{Name: "lab", Type: "openai", BaseURL: "http://example.com/v1", APIKeyEnv: "KEY", Models: []CustomModel{{Name: "m"}}}); err == nil {
		t.Fatal("credentialed non-loopback http endpoints should be rejected")
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestCustomOpenAIChatUsesWireModelAndEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOCALAI_KEY", "secret")
	var seenPath, seenAuth, seenModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		seenModel = body.Model
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2},
		})
	}))
	defer srv.Close()
	if err := UpsertCustomProvider(CustomProvider{
		Name:      "localai",
		Type:      "openai",
		API:       "chat",
		BaseURL:   srv.URL + "/v1/chat/completions",
		APIKeyEnv: "LOCALAI_KEY",
		Models:    []CustomModel{{Name: "friendly", ID: "wire-model"}},
	}); err != nil {
		t.Fatal(err)
	}
	p, err := New("localai", "friendly")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "ok" || seenPath != "/v1/chat/completions" || seenAuth != "Bearer secret" || seenModel != "wire-model" {
		t.Fatalf("custom chat mismatch out=%+v path=%q auth=%q model=%q", out, seenPath, seenAuth, seenModel)
	}
}

func TestCustomOpenAIResponsesUsesWireModelAndEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var seenPath, seenModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		seenModel = body.Model
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []map[string]any{{
				"type":    "message",
				"content": []map[string]any{{"type": "output_text", "text": "ok"}},
			}},
			"usage": map[string]any{"input_tokens": 3, "output_tokens": 2},
		})
	}))
	defer srv.Close()
	if err := UpsertCustomProvider(CustomProvider{
		Name:    "responseslab",
		Type:    "openai",
		API:     "responses",
		BaseURL: srv.URL + "/v1/responses",
		NoAuth:  true,
		Models:  []CustomModel{{Name: "friendly-r", ID: "wire-r"}},
	}); err != nil {
		t.Fatal(err)
	}
	p, err := New("responseslab", "friendly-r")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "ok" || seenPath != "/v1/responses" || seenModel != "wire-r" {
		t.Fatalf("custom responses mismatch out=%+v path=%q model=%q", out, seenPath, seenModel)
	}
}

func TestCustomAnthropicUsesWireModelEndpointAndVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANT_KEY", "secret")
	var seenPath, seenKey, seenVersion, seenModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenKey = r.Header.Get("x-api-key")
		seenVersion = r.Header.Get("anthropic-version")
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		seenModel = body.Model
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 4, "output_tokens": 2},
		})
	}))
	defer srv.Close()
	if err := UpsertCustomProvider(CustomProvider{
		Name:      "antlab",
		Type:      "ant",
		BaseURL:   srv.URL + "/v1/messages",
		APIKeyEnv: "ANT_KEY",
		Version:   "2024-01-01",
		Models:    []CustomModel{{Name: "friendly-ant", ID: "claude-wire"}},
	}); err != nil {
		t.Fatal(err)
	}
	p, err := New("antlab", "friendly-ant")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "ok" || seenPath != "/v1/messages" || seenKey != "secret" || seenVersion != "2024-01-01" || seenModel != "claude-wire" {
		t.Fatalf("custom anthropic mismatch out=%+v path=%q key=%q version=%q model=%q", out, seenPath, seenKey, seenVersion, seenModel)
	}
}

func TestCustomReasoningModelAppliesEffort(t *testing.T) {
	// A custom reasoning model with a catalog Effort must actually send that
	// effort on the wire for every kind, and SetEffort must change it live —
	// matching how a built-in reasoning model applies effort.

	t.Run("openai_chat", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		var seen struct {
			ReasoningEffort string `json:"reasoning_effort"`
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&seen)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
			})
		}))
		defer srv.Close()
		if err := UpsertCustomProvider(CustomProvider{
			Name: "reasonchat", Type: "openai", API: "chat", BaseURL: srv.URL + "/v1", NoAuth: true,
			Models: []CustomModel{{Name: "r-chat", ID: "wire", Reasoning: true, Effort: "high", EffortLevels: []string{"low", "medium", "high"}}},
		}); err != nil {
			t.Fatal(err)
		}
		p, err := New("reasonchat", "r-chat")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		if seen.ReasoningEffort != "high" {
			t.Fatalf("chat reasoning_effort = %q, want high", seen.ReasoningEffort)
		}
		es, ok := p.(EffortSetter)
		if !ok {
			t.Fatal("custom chat reasoning model must implement EffortSetter")
		}
		if es.Effort() != "high" {
			t.Fatalf("Effort() = %q, want high", es.Effort())
		}
		if !es.SetEffort("low") {
			t.Fatal("SetEffort(low) should succeed for a supported level")
		}
		if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		if seen.ReasoningEffort != "low" {
			t.Fatalf("after SetEffort, reasoning_effort = %q, want low", seen.ReasoningEffort)
		}
		if es.SetEffort("bogus") {
			t.Fatal("SetEffort should reject a level outside the model's set")
		}
	})

	t.Run("openai_responses", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		var seen struct {
			Reasoning *struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&seen)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"output": []map[string]any{{"type": "message", "content": []map[string]any{{"type": "output_text", "text": "ok"}}}},
			})
		}))
		defer srv.Close()
		if err := UpsertCustomProvider(CustomProvider{
			Name: "reasonresp", Type: "openai", API: "responses", BaseURL: srv.URL + "/v1", NoAuth: true,
			Models: []CustomModel{{Name: "r-resp", ID: "wire", Reasoning: true, Effort: "medium"}},
		}); err != nil {
			t.Fatal(err)
		}
		p, err := New("reasonresp", "r-resp")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		if seen.Reasoning == nil || seen.Reasoning.Effort != "medium" {
			t.Fatalf("responses reasoning effort = %+v, want medium", seen.Reasoning)
		}
	})

	t.Run("anthropic", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		var seen struct {
			Thinking     json.RawMessage `json:"thinking"`
			OutputConfig struct {
				Effort string `json:"effort"`
			} `json:"output_config"`
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&seen)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			})
		}))
		defer srv.Close()
		if err := UpsertCustomProvider(CustomProvider{
			Name: "reasonant", Type: "ant", BaseURL: srv.URL + "/v1", NoAuth: true,
			Models: []CustomModel{{Name: "r-ant", ID: "wire", Reasoning: true, Effort: "high", EffortLevels: []string{"low", "medium", "high", "max"}}},
		}); err != nil {
			t.Fatal(err)
		}
		p, err := New("reasonant", "r-ant")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		if string(seen.Thinking) != `{"type":"adaptive"}` || seen.OutputConfig.Effort != "high" {
			t.Fatalf("anthropic thinking=%s output_config.effort=%q, want adaptive/high", seen.Thinking, seen.OutputConfig.Effort)
		}
	})

	t.Run("non_reasoning_omits_effort", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		var raw map[string]json.RawMessage
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&raw)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
			})
		}))
		defer srv.Close()
		if err := UpsertCustomProvider(CustomProvider{
			Name: "plainchat", Type: "openai", API: "chat", BaseURL: srv.URL + "/v1", NoAuth: true,
			Models: []CustomModel{{Name: "plain", ID: "wire"}},
		}); err != nil {
			t.Fatal(err)
		}
		p, err := New("plainchat", "plain")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw["reasoning_effort"]; ok {
			t.Fatal("non-reasoning custom model should not send reasoning_effort")
		}
	})
}

func TestCustomProviderRejectsDuplicateModelAcrossProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := UpsertCustomProvider(CustomProvider{Name: "one", Type: "openai", BaseURL: "http://127.0.0.1:1/v1", NoAuth: true, Models: []CustomModel{{Name: "shared"}}}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCustomProvider(CustomProvider{Name: "two", Type: "openai", BaseURL: "http://127.0.0.1:2/v1", NoAuth: true, Models: []CustomModel{{Name: "shared"}}}); err == nil {
		t.Fatal("duplicate model aliases across providers should be rejected")
	}
}

func TestLoadCustomProvidersReportsInvalidCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".eigen", "providers.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"providers":[{"name":"bad","type":"openai","base_url":"file:///tmp/x","models":[{"name":"m"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCustomProviders(); err == nil {
		t.Fatal("invalid provider catalog should return an error")
	}
}

func TestSaveCustomProvidersPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := SaveCustomProviders([]CustomProvider{{Name: "p", Type: "openai", BaseURL: "http://x/v1", NoAuth: true, Models: []CustomModel{{Name: "m"}}}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, ".eigen", "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("providers.json mode = %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Join(home, ".eigen"))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf(".eigen mode = %v, want 0700", dirInfo.Mode().Perm())
	}
}
