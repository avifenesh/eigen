package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMantleRecoversCompletedOutputFromFailedEvent reproduces codex#27185:
// mantle streams a COMPLETE reply, then sends response.failed carrying the
// finished output. eigen must USE that output, not error/retry.
func TestMantleRecoversCompletedOutputFromFailedEvent(t *testing.T) {
	failedWithOutput := `data: {"type":"response.created","response":{}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","delta":"Hi"}` + "\n\n" +
		`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"oops"},"output":[{"type":"message","status":"completed","content":[{"type":"output_text","text":"Hi. What can I help with?"}]}]}}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(failedWithOutput))
	}))
	defer srv.Close()
	m := &Mantle{BaseURL: srv.URL, Model: "openai.gpt-5.5", effort: "medium", token: "t", http: http.DefaultClient}
	final, _, failErr, err := m.streamOnce(context.Background(), []byte(`{}`), nil)
	if err != nil || failErr != nil {
		t.Fatalf("a response.failed WITH completed output should NOT be an error: err=%v failErr=%v", err, failErr)
	}
	if final == nil || !strings.Contains(final.Text, "What can I help") {
		t.Fatalf("should recover the embedded completed text, got %+v", final)
	}
}

// TestMantleEmptyFailedRetries: a response.failed with NO output is a real
// transient → surfaced as failErr so the caller retries.
func TestMantleEmptyFailedRetries(t *testing.T) {
	emptyFail := `data: {"type":"response.created","response":{}}` + "\n\n" +
		`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"oops"},"output":[]}}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(emptyFail))
	}))
	defer srv.Close()
	m := &Mantle{BaseURL: srv.URL, Model: "openai.gpt-5.5", effort: "medium", token: "t", http: http.DefaultClient}
	_, emitted, failErr, err := m.streamOnce(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("transport ok, got err=%v", err)
	}
	if failErr == nil {
		t.Fatal("an empty response.failed should surface failErr (so the caller retries)")
	}
	if emitted {
		t.Fatal("nothing was emitted; emitted should be false so retry is safe")
	}
}
