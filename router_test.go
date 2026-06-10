package main

import (
	"context"
	"testing"
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

func TestKindDiffNames(t *testing.T) {
	// Sanity: the label helpers round-trip the enum names used in notes.
	if kindName(0) != "general" {
		t.Errorf("kindName general")
	}
	if diffName(0) != "trivial" {
		t.Errorf("diffName trivial")
	}
}
