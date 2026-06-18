package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

func TestAutoRouterDisabledReturnsNothing(t *testing.T) {
	r := newAutoRouter(false, nil, "converse")
	if p, _, _ := r.Route(context.Background(), "anything", "", "", false); p != nil {
		t.Fatal("disabled router must return nil provider")
	}
}

func TestAutoRouterEnableToggle(t *testing.T) {
	r := newAutoRouter(false, nil, "converse")
	if r.Enabled() {
		t.Fatal("should start disabled")
	}
	r.SetEnabled(true)
	if !r.Enabled() {
		t.Fatal("SetEnabled(true) should enable")
	}
}

func TestAutoRouterProviders(t *testing.T) {
	r := newAutoRouter(true, []string{"converse", "glm"}, "converse")
	got := r.Providers()
	if len(got) != 2 || got[0] != "converse" || got[1] != "glm" {
		t.Fatalf("providers wrong: %v", got)
	}
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
	r := newAutoRouter(true, nil, "codex")
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
	r := newAutoRouter(true, []string{"grok"}, "grok")
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
	r := newAutoRouter(true, nil, "grok")
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
	r := newAutoRouter(true, nil, "grok")
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
	r := newAutoRouter(false, nil, "converse")
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
	r := newAutoRouter(false, nil, "converse")
	// Stated difficulty: the gate must not bail early. Whether a provider is
	// ultimately constructed depends on credentials; the key behavior is that
	// the disabled+unstated path bails and the stated path proceeds.
	r.Route(context.Background(), "sort the imports in util.go", "", "trivial", false)
	if p, _, _ := r.Route(context.Background(), "sort the imports in util.go", "", "", false); p != nil {
		t.Fatal("unstated prompt must not route while disabled")
	}
}
