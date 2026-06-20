package gui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
)

func TestServeRejectsNonLocalBind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Serve(ctx, NewService(nil), ServeOptions{Addr: "0.0.0.0:0"})
	if err == nil || !strings.Contains(err.Error(), "refuses non-local bind") {
		t.Fatalf("Serve should reject non-local bind, got %v", err)
	}
}

func TestHandlerStaticAndAPIContracts(t *testing.T) {
	svc := NewService(func() (*daemon.Client, error) { return nil, context.DeadlineExceeded })
	ts := httptest.NewServer(Handler(svc))
	defer ts.Close()

	get := func(path string) (int, string, string) {
		t.Helper()
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("content-type"), string(body)
	}

	status, ctype, body := get("/api/health")
	if status != http.StatusOK || !strings.Contains(ctype, "application/json") {
		t.Fatalf("/api/health status=%d content-type=%q body=%s", status, ctype, body)
	}
	var health Health
	if err := json.Unmarshal([]byte(body), &health); err != nil {
		t.Fatal(err)
	}
	if health.OK || health.Error == "" || health.Socket == "" {
		t.Fatalf("health should report offline daemon with socket/error, got %+v", health)
	}

	status, _, body = get("/")
	if status != http.StatusOK {
		t.Fatalf("/ status=%d", status)
	}
	for _, want := range []string{"id=\"new-session\"", "id=\"feature-nav\"", "data-feature=\"home\"", "data-feature=\"chat\"", "id=\"desktop-overview\"", "id=\"feature-workspace\"", "id=\"timeline\"", "id=\"model-input\"", "id=\"profile-modal\"", "id=\"system-modal\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q", want)
		}
	}

	_, _, app := get("/app.js")
	for _, want := range []string{"function renderFeatureWorkspace", "function renderHomeWorkspace", "Every Eigen desktop page", "async function runFeatureAction", "async function applySettingFromFeature", "async function sendAllowedToolTurn", "function setFeature", "function renderUnifiedDiff", "function shellSummaryHTML", "async function openSystemModal", "connectEvents", "desktop().Subscribe"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js missing %q", want)
		}
	}

	_, _, css := get("/styles.css")
	for _, want := range []string{".feature-nav", ".home-surface", ".surface-directory", ".surface-tile", ".desktop-overview", ".feature-workspace", ".diff-view", ".shell-mini", ".system-card", ".approval-card", ".tool-card"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css missing %q", want)
		}
	}
}

func TestStreamJSONLinesStopsOnContextOrClosedEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan StreamEvent)
	done := make(chan error, 1)
	go func() {
		done <- StreamJSONLines(ctx, io.Discard, events, func(io.Writer, StreamEvent) error { return nil })
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("StreamJSONLines did not stop after context cancel")
	}

	ctx = context.Background()
	events = make(chan StreamEvent)
	close(events)
	if err := StreamJSONLines(ctx, io.Discard, events, func(io.Writer, StreamEvent) error { return nil }); err != nil {
		t.Fatalf("closed event channel should end cleanly, got %v", err)
	}
}
