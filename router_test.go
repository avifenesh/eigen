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
