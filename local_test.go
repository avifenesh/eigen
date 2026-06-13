package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalReady(t *testing.T) {
	// Ready: /health → 200 ok.
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ready.Close()
	if !localReady(ready.URL + "/v1") {
		t.Error("a /health=ok server should be ready (base with /v1 suffix stripped)")
	}

	// Loading: /health → 503 (or an ok body that says loading).
	loading := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"loading model"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer loading.Close()
	if localReady(loading.URL + "/v1") {
		t.Error("a loading (503) server must NOT be ready — this is the 'up but chaining' case")
	}

	// No /health but /v1/models works → serving.
	modelsOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer modelsOnly.Close()
	if !localReady(modelsOnly.URL + "/v1") {
		t.Error("a server with /v1/models but no /health should count as serving")
	}

	// Down: nothing listening.
	if localReady("http://127.0.0.1:0/v1") {
		t.Error("a down server must not be ready")
	}
}

func TestSmallProviderRespectsOptIn(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`)) // any path → ready-ish
	}))
	defer ready.Close()
	t.Setenv("EIGEN_LLAMA_BASE_URL", ready.URL+"/v1")
	t.Setenv("EIGEN_SMALL_MODEL", "")
	t.Setenv("EIGEN_TITLE_MODEL", "")

	// Opt-in OFF: must NOT pick the local model even though it's ready.
	pOff := smallProviderFor(nil, false)
	if pOff != nil && pOff.Name() != "" && isLocalProvider(pOff) {
		t.Error("local_background=false must not route to the local model")
	}
	// Opt-in ON + ready: picks local.
	pOn := smallProviderFor(nil, true)
	if pOn == nil || !isLocalProvider(pOn) {
		t.Errorf("local_background=true + ready server should pick the local model, got %v", pOn)
	}
}

func isLocalProvider(p interface{ Name() string }) bool {
	return p != nil && containsAny(p.Name(), "llama", "/v1", "local")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && (len(s) >= len(sub)) && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
