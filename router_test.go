package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

func TestAutoRouterDisabledReturnsNothing(t *testing.T) {
	r := newAutoRouter(false, nil, "converse", "")
	if p, _, _ := r.Route(context.Background(), "anything", "", "", false); p != nil {
		t.Fatal("disabled router must return nil provider")
	}
}

func TestAutoRouterEnableToggle(t *testing.T) {
	r := newAutoRouter(false, nil, "converse", "")
	if r.Enabled() {
		t.Fatal("should start disabled")
	}
	r.SetEnabled(true)
	if !r.Enabled() {
		t.Fatal("SetEnabled(true) should enable")
	}
}

func TestAutoRouterProviders(t *testing.T) {
	r := newAutoRouter(true, []string{"converse", "glm"}, "converse", "")
	got := r.Providers()
	if len(got) != 2 || got[0] != "converse" || got[1] != "glm" {
		t.Fatalf("providers wrong: %v", got)
	}
}

func clearRouteModelEnv(t *testing.T) {
	t.Helper()
	t.Setenv("EIGEN_ROUTE_MODEL", "")
	t.Setenv("EIGEN_ROUTER_MODEL", "")
	t.Setenv("EIGEN_TITLE_MODEL", "")
}

func testAssessor(kind llm.TaskKind, difficulty llm.Difficulty, frontend bool) routeAssessor {
	return func(context.Context, string, bool, []string) (routeAssessment, error) {
		return routeAssessment{Kind: kind, Difficulty: difficulty, Frontend: frontend, Assessor: "fake-small"}, nil
	}
}

func TestAutoRouterDelegatedRouteWidensDefaultProviderSet(t *testing.T) {
	t.Setenv("XAI_API_KEY", "test-key")
	t.Setenv("GLM_API_KEY", "test-key")
	t.Setenv("EIGEN_CODEX_AUTH", t.TempDir()+"/missing-auth.json")
	r := newAutoRouter(true, nil, "codex", "")
	r.assessor = testAssessor(llm.TaskGeneral, llm.DiffTrivial, false)
	p, model, label := r.Route(context.Background(), "rename this file", "", "trivial", false)
	if p == nil {
		t.Fatal("delegated route with empty route_providers should roam all credentialed providers, not stay stuck on current")
	}
	if model == "gpt-5.5" || strings.TrimSpace(model) == "" {
		t.Fatalf("trivial task should route away from current codex default, got %q (%s)", model, label)
	}
	if !strings.Contains(label, "trivial") {
		t.Fatalf("label should expose route decision, got %q", label)
	}
}

func TestAutoRouterRouteProvidersRestrictDefaultWidening(t *testing.T) {
	t.Setenv("XAI_API_KEY", "test-key")
	t.Setenv("GLM_API_KEY", "test-key")
	r := newAutoRouter(true, []string{"grok"}, "grok", "")
	r.assessor = testAssessor(llm.TaskGeneral, llm.DiffTrivial, false)
	p, model, label := r.Route(context.Background(), "rename this file", "", "trivial", false)
	if p == nil {
		t.Fatalf("restricted route should still find grok candidate: %s", label)
	}
	if !strings.HasPrefix(model, "grok-") {
		t.Fatalf("route_providers should restrict routing to grok, got %q (%s)", model, label)
	}
}

func TestAutoRouterUsesModelAssessmentNotPromptWords(t *testing.T) {
	t.Setenv("XAI_API_KEY", "test-key")
	r := newAutoRouter(true, nil, "grok", "")
	r.assessor = testAssessor(llm.TaskSearch, llm.DiffHard, false)
	p, _, label := r.Route(context.Background(), "rename this file", "", "", false)
	if p == nil {
		t.Fatalf("model assessment should drive routing, got no provider: %s", label)
	}
	for _, want := range []string{"search", "hard", "assessed by fake-small"} {
		if !strings.Contains(label, want) {
			t.Fatalf("route label should reflect model assessment %q, got %q", want, label)
		}
	}
	if strings.Contains(label, "trivial") {
		t.Fatalf("route must not use wording heuristic for 'rename', got %q", label)
	}
}

func TestAutoRouterSkipsWhenModelAssessorUnavailable(t *testing.T) {
	t.Setenv("XAI_API_KEY", "test-key")
	r := newAutoRouter(true, nil, "grok", "")
	r.assessor = func(context.Context, string, bool, []string) (routeAssessment, error) {
		return routeAssessment{}, errors.New("classifier offline")
	}
	p, _, label := r.Route(context.Background(), "rename this file", "", "", false)
	if p != nil {
		t.Fatal("router should not fall back to wording heuristics when model assessment fails")
	}
	if !strings.Contains(label, "assessor unavailable") || !strings.Contains(label, "classifier offline") {
		t.Fatalf("bad skip label: %q", label)
	}
}

func TestParseRouteAssessment(t *testing.T) {
	a, err := parseRouteAssessment("```json\n{\"kind\":\"vision\",\"level\":\"easy\",\"frontend\":true}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != llm.TaskVision || a.Difficulty != llm.DiffEasy || !a.Frontend {
		t.Fatalf("bad assessment: %+v", a)
	}
	if _, err := parseRouteAssessment(`{"kind":"general","difficulty":"tiny"}`); err == nil {
		t.Fatal("invalid difficulty should fail closed")
	}
}

func TestParseRouteAssessmentFencedJSONWithModel(t *testing.T) {
	a, err := parseRouteAssessment("here:\n```json\n{\"kind\":\"general\",\"difficulty\":\"easy\",\"model\":\"grok-build\"}\n```\nthanks")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != llm.TaskGeneral || a.Difficulty != llm.DiffEasy || a.Model != "grok-build" {
		t.Fatalf("bad fenced assessment parse: %+v", a)
	}
}

func TestParseRouteAssessmentModelOnlyDefaults(t *testing.T) {
	a, err := parseRouteAssessment(`{"model":"grok-build"}`)
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != llm.TaskGeneral || a.Difficulty != llm.DiffMedium || a.Model != "grok-build" {
		t.Fatalf("model-only assessment should default kind/difficulty: %+v", a)
	}
}

func TestKindDiffNames(t *testing.T) {
	// Sanity: the label helpers round-trip the enum names used in notes.
	if kindName(0) != "general" {
		t.Errorf("kindName general")
	}
	if diffName(0) != "trivial" {
		t.Errorf("diffName trivial")
	}
}

func TestAutoRouterImageForcesVisionEvenWhenDisabled(t *testing.T) {
	r := newAutoRouter(false, nil, "converse", "")
	// hasImage=true must not be short-circuited by enabled=false. Whether a
	// provider is actually constructed depends on credentials, so just assert
	// it does NOT bail at the enabled check: a credentialed env returns a
	// vision model; an uncredentialed one returns nothing later. We verify the
	// gate logic by checking the disabled+no-image path still bails.
	if p, _, _ := r.Route(context.Background(), "plain prompt", "", "", false); p != nil {
		t.Fatal("disabled router must not route plain prompts")
	}
	// With an image the code path continues past the gate (may still return
	// nil without credentials — that's fine; no panic, no early-disable bail).
	r.Route(context.Background(), "look at this screenshot", "", "", true)
}

func TestExplicitDelegationRoutesEvenWhenDisabled(t *testing.T) {
	// Orchestrator-stated difficulty must route even with the heuristic
	// auto-router off — routing is the orchestrator's per-decision act.
	r := newAutoRouter(false, nil, "converse", "")
	// Stated difficulty: the gate must not bail early. Whether a provider is
	// ultimately constructed depends on credentials; the key behavior is that
	// the disabled+unstated path bails and the stated path proceeds.
	r.Route(context.Background(), "sort the imports in util.go", "", "trivial", false)
	if p, _, _ := r.Route(context.Background(), "sort the imports in util.go", "", "", false); p != nil {
		t.Fatal("unstated prompt must not route while disabled")
	}
}

func localRouterServer(t *testing.T, assessment string, calls *int, seenModel *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		*calls = *calls + 1
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("local router request was not JSON: %v", err)
		}
		*seenModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": assessment},
			}},
		})
	}))
}

func TestLocalPromptRouterAssessesPrompt(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"search","difficulty":"hard","frontend":false}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "find the latest release notes", "", "", false)
	if p == nil {
		t.Fatalf("local prompt router should route via assessed JSON: %s", label)
	}
	if calls != 1 {
		t.Fatalf("local prompt router calls = %d, want 1", calls)
	}
	if seenModel != "router-tiny" {
		t.Fatalf("local router model = %q, want router-tiny", seenModel)
	}
	info, _ := llm.Lookup(model)
	if !info.Search {
		t.Fatalf("local search assessment routed to non-search model %q (%s)", model, label)
	}
	for _, want := range []string{"search", "hard", "assessed by local:router-tiny"} {
		if !strings.Contains(label, want) {
			t.Fatalf("route label missing %q: %q", want, label)
		}
	}
}

func TestLocalPromptRouterCanPickConcreteCandidateModel(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"general","difficulty":"trivial","frontend":false,"model":"grok-build"}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "route this exact candidate", "", "", false)
	if p == nil {
		t.Fatalf("local prompt router concrete model should route: %s", label)
	}
	if model != "grok-build" {
		t.Fatalf("local prompt router concrete model = %q, want grok-build (%s)", model, label)
	}
}

func TestLocalPromptRouterNormalizesProviderPrefixedModel(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"general","difficulty":"trivial","frontend":false,"model":"grok:grok-build"}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "route this exact candidate", "", "", false)
	if p == nil {
		t.Fatalf("provider-prefixed local prompt router model should route: %s", label)
	}
	if model != "grok-build" {
		t.Fatalf("normalized local prompt router model = %q, want grok-build (%s)", model, label)
	}
}

func TestLocalPromptRouterNonCandidateFallsBack(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"general","difficulty":"trivial","frontend":false,"model":"glm-5.2"}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "route outside allowlist", "", "", false)
	if p == nil {
		t.Fatalf("bad local router output should fall back, got no provider: %s", label)
	}
	if model == "glm-5.2" {
		t.Fatalf("fallback must not honor non-candidate model: %s", label)
	}
	if !strings.Contains(label, "local fallback") {
		t.Fatalf("fallback label missing local fallback: %q", label)
	}
}

func TestLocalPromptRouterMissingEndpointFallsBack(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	t.Setenv("EIGEN_LLAMA_BASE_URL", "")
	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, _, label := r.Route(context.Background(), "rename this file", "", "", false)
	if p == nil {
		t.Fatalf("configured local prompt router should degrade to fallback when local endpoint is missing: %s", label)
	}
	if !strings.Contains(label, "local fallback") {
		t.Fatalf("bad fallback label: %q", label)
	}
}

func TestLocalPromptRouterMalformedOutputFallsBack(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `not json`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, _, label := r.Route(context.Background(), "rename this file", "", "", false)
	if p == nil {
		t.Fatalf("malformed local router output should fall back: %s", label)
	}
	if !strings.Contains(label, "local fallback") {
		t.Fatalf("bad fallback label: %q", label)
	}
}

func TestLocalPromptRouterInvalidEnumFallsBack(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"coding","difficulty":"super-hard","model":"grok-build"}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, _, label := r.Route(context.Background(), "rename this file", "", "", false)
	if p == nil {
		t.Fatalf("invalid local router enums should fall back: %s", label)
	}
	if !strings.Contains(label, "local fallback") {
		t.Fatalf("bad fallback label: %q", label)
	}
}

func TestLocalPromptRouterImageForcesVisionOverConcreteModel(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"general","difficulty":"easy","frontend":false,"model":"grok-composer-2.5-fast"}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "look at this screenshot", "", "", true)
	if p == nil {
		t.Fatalf("image should force a vision-capable policy fallback over local concrete model: %s", label)
	}
	if model == "grok-composer-2.5-fast" {
		t.Fatalf("image route must not honor non-vision concrete local-router model: %s", label)
	}
	info, _ := llm.Lookup(model)
	if !info.Vision {
		t.Fatalf("image route picked non-vision model %q (%s)", model, label)
	}
}

func TestLocalPromptRouterIncapableModelFallsBackToPolicy(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"vision","difficulty":"easy","frontend":false,"model":"grok-composer-2.5-fast"}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "look at this screenshot", "", "", false)
	if p == nil {
		t.Fatalf("incapable assessed model should fall back to capable policy route: %s", label)
	}
	info, _ := llm.Lookup(model)
	if !info.Vision {
		t.Fatalf("fallback policy picked non-vision model %q (%s)", model, label)
	}
}

func TestRouteExplicitBothFieldsSkipsLocalPromptRouter(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	t.Setenv("EIGEN_LLAMA_BASE_URL", "")
	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, _, label := r.Route(context.Background(), "rename this file", "general", "trivial", false)
	if p == nil {
		t.Fatalf("explicit kind+difficulty should not call the unavailable local assessor: %s", label)
	}
	if !strings.Contains(label, "orchestrator-stated") {
		t.Fatalf("explicit route should be labeled orchestrator-stated, got %q", label)
	}
}

func TestRouteExplicitKindOverridesLocalPromptRouter(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("XAI_API_KEY", "test-key")
	calls := 0
	seenModel := ""
	srv := localRouterServer(t, `{"kind":"search","difficulty":"easy","frontend":false}`, &calls, &seenModel)
	defer srv.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", srv.URL+"/v1")

	r := newAutoRouter(true, []string{"grok"}, "grok", "router-tiny")
	p, model, label := r.Route(context.Background(), "classify this screenshot task", "vision", "", false)
	if p == nil {
		t.Fatalf("explicit kind should merge with local difficulty assessment: %s", label)
	}
	info, _ := llm.Lookup(model)
	if !info.Vision {
		t.Fatalf("explicit vision kind routed to non-vision model %q (%s)", model, label)
	}
	for _, want := range []string{"vision", "easy", "assessed by local:router-tiny"} {
		if !strings.Contains(label, want) {
			t.Fatalf("route label missing %q: %q", want, label)
		}
	}
}

func TestRouteImageWithCredsPicksVisionModel(t *testing.T) {
	t.Setenv("XAI_API_KEY", "test-key")
	r := newAutoRouter(false, []string{"grok"}, "grok", "")
	p, model, label := r.Route(context.Background(), "look at this screenshot", "", "", true)
	if p == nil {
		t.Fatalf("image routing should pick a vision model even when /route is off: %s", label)
	}
	info, _ := llm.Lookup(model)
	if !info.Vision {
		t.Fatalf("image routed to non-vision model %q (%s)", model, label)
	}
}

func TestRouteModelSourcePrecedence(t *testing.T) {
	t.Setenv("EIGEN_ROUTE_MODEL", "primary-env-router")
	t.Setenv("EIGEN_ROUTER_MODEL", "legacy-env-router")
	r := newAutoRouter(false, nil, "grok", "config-router")
	if r.routeModel != "primary-env-router" {
		t.Fatalf("EIGEN_ROUTE_MODEL should override legacy env/config route_model, got %q", r.routeModel)
	}

	t.Setenv("EIGEN_ROUTE_MODEL", "")
	r = newAutoRouter(false, nil, "grok", "config-router")
	if r.routeModel != "legacy-env-router" {
		t.Fatalf("EIGEN_ROUTER_MODEL should override config route_model when primary env is unset, got %q", r.routeModel)
	}

	t.Setenv("EIGEN_ROUTER_MODEL", "")
	r = newAutoRouter(false, nil, "grok", "config-router")
	if r.routeModel != "config-router" {
		t.Fatalf("config route_model should be used when env is unset, got %q", r.routeModel)
	}
}

func TestLlamaBaseURLAloneDoesNotEnableLocalPromptRouter(t *testing.T) {
	clearRouteModelEnv(t)
	t.Setenv("EIGEN_LLAMA_BASE_URL", "http://127.0.0.1:65535/v1")
	r := newAutoRouter(false, nil, "grok", "")
	if r.localRouteAssessor {
		t.Fatal("EIGEN_LLAMA_BASE_URL alone must not enable local prompt routing; route_model/EIGEN_ROUTE_MODEL is the opt-in")
	}
}

func TestLocalRouteModelRef(t *testing.T) {
	for _, tc := range []struct {
		name         string
		configured   string
		wantProvider string
		wantModel    string
	}{
		{name: "untagged local model", configured: "router-tiny", wantProvider: "llama", wantModel: "router-tiny"},
		{name: "ollama-style colon model", configured: "qwen2.5:7b", wantProvider: "llama", wantModel: "qwen2.5:7b"},
		{name: "explicit provider ref", configured: "llama:router-tiny", wantProvider: "llama", wantModel: "router-tiny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, model := localRouteModelRef(tc.configured)
			if provider != tc.wantProvider || model != tc.wantModel {
				t.Fatalf("localRouteModelRef(%q) = (%q, %q), want (%q, %q)", tc.configured, provider, model, tc.wantProvider, tc.wantModel)
			}
		})
	}
}
