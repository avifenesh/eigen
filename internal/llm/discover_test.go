package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListJSONParsesShapes(t *testing.T) {
	// OpenAI shape: {"data":[{"id":...}]}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"grok-4"},{"id":"grok-9-future"}]}`))
	}))
	defer srv.Close()
	ids, err := listJSON(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "grok-4" || ids[1] != "grok-9-future" {
		t.Fatalf("data shape parse wrong: %v", ids)
	}

	// Anthropic shape also uses data[].id; Bedrock-style modelSummaries:
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"modelSummaries":[{"modelId":"us.anthropic.claude-opus-4-9"}]}`))
	}))
	defer srv2.Close()
	ids, err = listJSON(context.Background(), srv2.URL, nil)
	if err != nil || len(ids) != 1 || ids[0] != "us.anthropic.claude-opus-4-9" {
		t.Fatalf("modelSummaries parse wrong: %v %v", ids, err)
	}
}

func TestListJSONHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`forbidden`))
	}))
	defer srv.Close()
	if _, err := listJSON(context.Background(), srv.URL, nil); err == nil {
		t.Fatal("HTTP 403 should error")
	}
}

func TestDiscoverSplitsKnownVsNew(t *testing.T) {
	// Drive the splitting logic directly: a known catalog id + a fake new one.
	known := "grok-4"
	if _, ok := Lookup(known); !ok {
		t.Skipf("%s not in catalog; skipping", known)
	}
	d := Discovered{Provider: "test"}
	for _, id := range []string{known, "totally-new-xyz"} {
		if _, ok := Lookup(id); ok {
			d.Known = append(d.Known, id)
		} else {
			d.New = append(d.New, id)
		}
	}
	if len(d.Known) != 1 || d.Known[0] != known {
		t.Fatalf("known wrong: %v", d.Known)
	}
	if len(d.New) != 1 || d.New[0] != "totally-new-xyz" {
		t.Fatalf("new wrong: %v", d.New)
	}
}

func TestIsSkippable(t *testing.T) {
	if !isSkippable(errSkip{"no creds"}) {
		t.Fatal("errSkip should be skippable")
	}
	if !isSkippable(errConnRefused{}) {
		t.Fatal("connection refused should be skippable")
	}
	if isSkippable(errReal{}) {
		t.Fatal("a real error should not be skippable")
	}
}

func TestBedrockDiscoverProfileMatchesConverse(t *testing.T) {
	// Discovery must resolve the same profile precedence as the chat path
	// (EIGEN_CONVERSE_PROFILE > AWS_PROFILE > aviary), or it probes the wrong
	// account for a user on a non-default profile.
	t.Setenv("EIGEN_CONVERSE_PROFILE", "")
	t.Setenv("AWS_PROFILE", "")
	if got := bedrockDiscoverProfile(); got != "aviary" {
		t.Fatalf("default profile: got %q, want aviary", got)
	}

	t.Setenv("AWS_PROFILE", "work")
	if got := bedrockDiscoverProfile(); got != "work" {
		t.Fatalf("AWS_PROFILE: got %q, want work", got)
	}

	t.Setenv("EIGEN_CONVERSE_PROFILE", "aviary")
	if got := bedrockDiscoverProfile(); got != "aviary" {
		t.Fatalf("EIGEN_CONVERSE_PROFILE should win: got %q, want aviary", got)
	}
}

type errConnRefused struct{}

func (errConnRefused) Error() string { return "dial tcp: connect: connection refused" }

type errReal struct{}

func (errReal) Error() string { return "HTTP 500: server exploded" }
