package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMantlePostRetriesTransient(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			http.Error(w, `{"error":{"message":"transient"}}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}`))
	}))
	defer srv.Close()

	m := &Mantle{BaseURL: srv.URL, Model: "test", token: "t", http: &http.Client{Timeout: 5 * time.Second}}
	// Shrink the loop's notion of time isn't exposed; with two failures the
	// backoff is 1s+2s ~3s, acceptable for a unit test.
	resp, err := m.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "x"}}})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if resp.Text != "hi" {
		t.Fatalf("got %q, want %q", resp.Text, "hi")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 calls (2 failures + success), got %d", got)
	}
}

func TestMantlePostDoesNotRetry4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	m := &Mantle{BaseURL: srv.URL, Model: "test", token: "t", http: &http.Client{Timeout: 5 * time.Second}}
	if _, err := m.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "x"}}}); err == nil {
		t.Fatal("expected error on 400")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 call (no retry on 4xx), got %d", got)
	}
}

func TestMantleRetriesEmptyCompletedResponse(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		if n < 2 {
			// Completed but empty: no message, no function_call (mantle quirk).
			w.Write([]byte(`{"output":[{"type":"reasoning"}]}`))
			return
		}
		w.Write([]byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered"}]}]}`))
	}))
	defer srv.Close()

	m := &Mantle{BaseURL: srv.URL, Model: "test", token: "t", http: &http.Client{Timeout: 5 * time.Second}}
	resp, err := m.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "recovered" {
		t.Fatalf("got %q, want %q", resp.Text, "recovered")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls (1 empty + recovery), got %d", got)
	}
}
