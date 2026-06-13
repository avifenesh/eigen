package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGLMRequiresKey(t *testing.T) {
	t.Setenv("GLM_API_KEY", "")
	t.Setenv("ZHIPUAI_API_KEY", "")
	t.Setenv("EIGEN_GLM_API_KEY", "")
	if _, err := NewGLM("glm-4.6"); err == nil {
		t.Fatal("NewGLM should require an API key")
	}
}

func TestGLMDefaults(t *testing.T) {
	t.Setenv("GLM_API_KEY", "glm-test")
	t.Setenv("EIGEN_GLM_BASE_URL", "")
	g, err := NewGLM("")
	if err != nil {
		t.Fatal(err)
	}
	if g.c.model != "glm-5.1" {
		t.Fatalf("empty model should default to glm-5.1, got %q", g.c.model)
	}
	if !strings.HasPrefix(g.c.baseURL, "https://api.z.ai") {
		t.Fatalf("default base URL should be z.ai, got %q", g.c.baseURL)
	}
	if !strings.Contains(g.Name(), "glm-5.1") || !strings.Contains(g.Name(), "zhipu") {
		t.Fatalf("unexpected name %q", g.Name())
	}
}

func TestGLMHonorsAltKeysAndBase(t *testing.T) {
	t.Setenv("GLM_API_KEY", "")
	t.Setenv("ZHIPUAI_API_KEY", "zp-test")
	t.Setenv("EIGEN_GLM_BASE_URL", "https://example.test/v4")
	g, err := NewGLM("glm-4.5")
	if err != nil {
		t.Fatal(err)
	}
	if g.c.apiKey != "zp-test" {
		t.Fatalf("ZHIPUAI_API_KEY should be honored, got %q", g.c.apiKey)
	}
	if g.c.baseURL != "https://example.test/v4" {
		t.Fatalf("base URL override failed, got %q", g.c.baseURL)
	}
}

func TestGLMSearchDefaultAuto(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	g, err := NewGLM("glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	if g.SearchMode() != "auto" {
		t.Fatalf("GLM search should default to auto, got %q", g.SearchMode())
	}
	// The web_search tool should be injected.
	tools := g.webSearchTool()
	if len(tools) != 1 || tools[0]["type"] != "web_search" {
		t.Fatalf("web_search tool should be injected when search is auto, got %v", tools)
	}
}

func TestGLMSearchOffDisablesTool(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	g, err := NewGLM("glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	if !g.SetSearch("off") {
		t.Fatal("SetSearch(off) should succeed")
	}
	if g.SearchMode() != "off" {
		t.Fatalf("search should be off, got %q", g.SearchMode())
	}
	if tools := g.webSearchTool(); tools != nil {
		t.Fatalf("web_search tool should be nil when search is off, got %v", tools)
	}
}

func TestGLMSearchOnEnablesTool(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	g, err := NewGLM("glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	if !g.SetSearch("on") {
		t.Fatal("SetSearch(on) should succeed")
	}
	tools := g.webSearchTool()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool entry, got %d", len(tools))
	}
	if tools[0]["type"] != "web_search" {
		t.Fatalf("tool type should be web_search, got %v", tools[0])
	}
	ws, _ := tools[0]["web_search"].(map[string]any)
	if ws["enable"] != true {
		t.Fatal("web_search.enable should be true")
	}
	if ws["search_result"] != true {
		t.Fatal("web_search.search_result should be true")
	}
}

func TestGLMSearchEnvOverride(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	t.Setenv("EIGEN_GLM_SEARCH", "off")
	g, err := NewGLM("glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	if g.SearchMode() != "off" {
		t.Fatalf("env should force search off, got %q", g.SearchMode())
	}
}

func TestGLMWebSearchToolInRequestBody(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	g, err := NewGLM("glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	// Build a request body and verify the web_search tool is in the tools array.
	body, err := g.c.body(Request{
		Messages: []Message{{Role: RoleUser, Text: "hello"}},
		Tools:    []ToolSpec{{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	// Should have 2 tools: the function tool + web_search.
	foundWebSearch := false
	for _, raw := range parsed.Tools {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil && m["type"] == "web_search" {
			foundWebSearch = true
		}
	}
	if !foundWebSearch {
		t.Fatalf("request body should contain web_search tool, tools=%v", parsed.Tools)
	}
}

func TestGLMBadSearchMode(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	g, err := NewGLM("glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	if g.SetSearch("bogus") {
		t.Fatal("bogus search mode should be rejected")
	}
}

func TestGLMThinkingModes(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	p, err := NewGLM("glm-5.2")
	if err != nil {
		t.Fatal(err)
	}
	// Reasoning model defaults to thinking on.
	if p.Effort() != "on" {
		t.Fatalf("glm-5.2 should default to effort on, got %q", p.Effort())
	}
	// bodyExtra emits the enabled thinking field + preserved thinking
	// (clear_thinking:false) by default for coherence/cache.
	ex := p.bodyExtra()
	if ex == nil || ex["thinking"].(map[string]any)["type"] != "enabled" {
		t.Fatalf("on → thinking.type=enabled, got %v", ex)
	}
	if ct, ok := ex["clear_thinking"]; !ok || ct != false {
		t.Fatalf("on → clear_thinking:false (preserved thinking), got %v", ex["clear_thinking"])
	}
	// Turn it off → disabled.
	if !p.SetEffort("off") || p.Effort() != "off" {
		t.Fatalf("SetEffort(off) failed: %q", p.Effort())
	}
	off := p.bodyExtra()
	if off["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatal("off → thinking.type=disabled")
	}
	if _, ok := off["clear_thinking"]; ok {
		t.Fatal("off → no clear_thinking field (only meaningful while enabled)")
	}
	// Any reasoning word → enabled.
	if !p.SetEffort("high") || p.Effort() != "on" {
		t.Fatalf("SetEffort(high) → on, got %q", p.Effort())
	}
	// A non-reasoning GLM model has no thinking control.
	t.Setenv("GLM_API_KEY", "test")
	air, _ := NewGLM("glm-4.5-air")
	if air.Effort() != "" || air.SetEffort("on") {
		t.Fatalf("glm-4.5-air should have no thinking control, got %q", air.Effort())
	}
}

func TestGLMClearThinkingEnvOverride(t *testing.T) {
	t.Setenv("GLM_API_KEY", "test")
	t.Setenv("EIGEN_GLM_CLEAR_THINKING", "1")
	p, err := NewGLM("glm-5.2")
	if err != nil {
		t.Fatal(err)
	}
	// With the override, an enabled turn must NOT set clear_thinking:false.
	ex := p.bodyExtra()
	if _, ok := ex["clear_thinking"]; ok {
		t.Fatalf("EIGEN_GLM_CLEAR_THINKING=1 should drop preserved thinking, got %v", ex["clear_thinking"])
	}
}
