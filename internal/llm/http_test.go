package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPJSONRejectsOversizedBody verifies that a 2xx response whose body
// exceeds maxResponseBytes is reported as an explicit error rather than being
// silently truncated mid-JSON (which would surface as an opaque
// "unexpected end of JSON input" at the caller's json.Unmarshal).
func TestHTTPJSONRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// One byte past the cap: enough to trip the guard.
		w.Write(make([]byte, maxResponseBytes+1))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	raw, _, err := httpJSON(context.Background(), client, srv.URL, nil, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded") || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("error should mention exceeding the cap, got: %v", err)
	}
	if raw != nil {
		t.Fatalf("expected nil body on cap error, got %d bytes", len(raw))
	}
}

// TestHTTPJSONAcceptsBodyAtCap verifies that a body exactly at the cap is read
// in full and returned without error (the +1 read must not false-positive).
func TestHTTPJSONAcceptsBodyAtCap(t *testing.T) {
	want := make([]byte, maxResponseBytes)
	for i := range want {
		want[i] = 'a'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(want)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	raw, status, err := httpJSON(context.Background(), client, srv.URL, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("body at cap should succeed, got: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if len(raw) != maxResponseBytes {
		t.Fatalf("read %d bytes, want %d", len(raw), maxResponseBytes)
	}
}

func TestHTTPJSONReturnsLastTransientStatusOnBackoffCancel(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"quota"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Timeout: 5 * time.Second}
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	_, status, err := httpJSON(ctx, client, srv.URL, nil, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected retry/backoff cancellation error")
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", status)
	}
	if calls < 1 {
		t.Fatal("server was not called")
	}
	if !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("error should retain last HTTP 429 cause, got %v", err)
	}
}

func TestHTTPStreamRetainsTransientCauseOnBackoffCancel(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"quota"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Timeout: 5 * time.Second}
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	_, err := httpStream(ctx, client, srv.URL, nil, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected retry/backoff cancellation error")
	}
	if calls < 1 {
		t.Fatal("server was not called")
	}
	if !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("error should retain last HTTP 429 cause, got %v", err)
	}
}
