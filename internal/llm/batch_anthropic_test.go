package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSafeCustomIDRoundTrips checks the deterministic, STATELESS key transform:
// a key already in the safe charset passes through unchanged (reversible by
// itself), while a key with bad chars or >64 chars hashes to a stable "h-..."
// id that reverses via the same-process hashed map.
func TestSafeCustomIDRoundTrips(t *testing.T) {
	cases := []struct {
		key            string
		wantReversible bool
	}{
		{"sess_01ABC-xyz", true},         // already safe → passthrough
		{"thread-123_456", true},         // already safe → passthrough
		{"sess/with spaces!", false},     // bad chars → hashed
		{"héllo", false},                 // non-ascii → hashed
		{strings.Repeat("a", 65), false}, // too long → hashed
		{"", false},                      // empty is not a valid id → hashed
		{"h-collides", false},            // collides with hash namespace → re-hashed
	}
	for _, tc := range cases {
		id, reversible := safeCustomID(tc.key)
		if reversible != tc.wantReversible {
			t.Errorf("safeCustomID(%q) reversible=%v, want %v (id=%q)", tc.key, reversible, tc.wantReversible, id)
		}
		// The produced id is always valid on the wire: non-empty, ≤64 chars, and
		// drawn from [a-zA-Z0-9_-]. (We don't reuse isSafeCustomID here — its
		// extra "h-" guard is a passthrough-eligibility check, not a wire check,
		// and hashed ids legitimately start with "h-".)
		if !isValidWireCustomID(id) {
			t.Errorf("safeCustomID(%q) produced invalid wire id %q", tc.key, id)
		}
		// Deterministic: same input → same id every time.
		if id2, _ := safeCustomID(tc.key); id2 != id {
			t.Errorf("safeCustomID(%q) not deterministic: %q vs %q", tc.key, id, id2)
		}

		// Reverse it the way CollectBatch does.
		var hashed map[string]string
		if !reversible {
			hashed = map[string]string{id: tc.key}
		}
		if got := collectKey(id, hashed); got != tc.key {
			t.Errorf("collectKey(%q) = %q, want original key %q", id, got, tc.key)
		}
	}
}

// TestCollectKeyFallsBackWithoutMap verifies the post-restart path: a hashed id
// with no same-process map reverses to itself (the dream caller then reprocesses
// the unrecognized key synchronously), while a passthrough id still reverses to
// the original key with no state at all.
func TestCollectKeyFallsBackWithoutMap(t *testing.T) {
	passthrough, _ := safeCustomID("sess_ok-1")
	if got := collectKey(passthrough, nil); got != "sess_ok-1" {
		t.Errorf("passthrough collectKey(%q, nil) = %q, want sess_ok-1", passthrough, got)
	}
	hashedID, _ := safeCustomID("bad key!")
	if got := collectKey(hashedID, nil); got != hashedID {
		t.Errorf("hashed collectKey(%q, nil) = %q, want the id itself (no map)", hashedID, got)
	}
}

// TestSubmitPollCollectRoundTrip drives the full lifecycle against a fake server:
// submit returns a batch id, poll maps processing_status/request_counts, and
// collect parses the JSONL results_url stream — mapping succeeded lines back to
// the ORIGINAL keys (one passthrough, one hashed) and skipping the errored line.
func TestSubmitPollCollectRoundTrip(t *testing.T) {
	const safeKey = "sess_keep-1"
	const badKey = "weird key/with spaces"
	hashedID, _ := safeCustomID(badKey)

	var resultsPath string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resultsURL := srv.URL + "/results.jsonl"

	// POST create.
	mux.HandleFunc("/v1/messages/batches", func(w http.ResponseWriter, r *http.Request) {
		// Subpaths (/<id>) are handled below; only the exact collection path creates.
		if r.URL.Path != "/v1/messages/batches" {
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("create: method = %s, want POST", r.Method)
		}
		var submit anthropicBatchSubmit
		if err := json.NewDecoder(r.Body).Decode(&submit); err != nil {
			t.Errorf("create: decode body: %v", err)
		}
		if len(submit.Requests) != 2 {
			t.Errorf("create: got %d requests, want 2", len(submit.Requests))
		}
		// Each per-request params must carry the claude-code spoof system block
		// (proof buildBody was reused, satisfying the OAuth gate).
		for _, req := range submit.Requests {
			if !strings.Contains(string(req.Params), claudeCodeSpoof) {
				t.Errorf("create: params for %q missing claude-code spoof: %s", req.CustomID, req.Params)
			}
		}
		w.Write([]byte(`{"id":"msgbatch_test1","processing_status":"in_progress","request_counts":{"processing":2,"succeeded":0,"errored":0,"canceled":0,"expired":0}}`))
	})

	// GET retrieve /<id>.
	mux.HandleFunc("/v1/messages/batches/msgbatch_test1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("retrieve: method = %s, want GET", r.Method)
		}
		w.Write([]byte(`{"id":"msgbatch_test1","processing_status":"ended","request_counts":{"processing":0,"succeeded":2,"errored":1,"canceled":0,"expired":0},"results_url":` + jsonString(resultsURL) + `}`))
	})

	// GET the JSONL results stream: one succeeded line per surviving key + one
	// errored line that collect must skip.
	mux.HandleFunc("/results.jsonl", func(w http.ResponseWriter, r *http.Request) {
		resultsPath = r.URL.Path
		lines := []string{
			`{"custom_id":"` + safeKey + `","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"alpha"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}}}`,
			`{"custom_id":"` + hashedID + `","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"beta"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":3}}}}`,
			`{"custom_id":"some_errored","result":{"type":"errored","message":{}}}`,
		}
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
	})

	origURL := anthropicBatchURL
	anthropicBatchURL = srv.URL + "/v1/messages/batches"
	defer func() { anthropicBatchURL = origURL }()

	a := &Anthropic{Model: "claude-sonnet-4-5", apiKey: "k", http: srv.Client()}
	ctx := context.Background()

	items := []BatchItem{
		{Key: safeKey, Req: Request{Messages: []Message{{Role: RoleUser, Text: "hi alpha"}}}},
		{Key: badKey, Req: Request{Messages: []Message{{Role: RoleUser, Text: "hi beta"}}}},
	}

	id, err := a.SubmitBatch(ctx, items)
	if err != nil {
		t.Fatalf("SubmitBatch: %v", err)
	}
	if id != "msgbatch_test1" {
		t.Fatalf("SubmitBatch id = %q, want msgbatch_test1", id)
	}

	st, err := a.PollBatch(ctx, id)
	if err != nil {
		t.Fatalf("PollBatch: %v", err)
	}
	if st.State != BatchDone {
		t.Errorf("poll state = %q, want %q", st.State, BatchDone)
	}
	// succeeded(2)+errored(1) = 3 done; +processing(0) = 3 total.
	if st.Done != 3 || st.Total != 3 {
		t.Errorf("poll Done/Total = %d/%d, want 3/3", st.Done, st.Total)
	}

	res, err := a.CollectBatch(ctx, id)
	if err != nil {
		t.Fatalf("CollectBatch: %v", err)
	}
	if resultsPath != "/results.jsonl" {
		t.Errorf("results stream not fetched (path=%q)", resultsPath)
	}
	// Two succeeded keys map back to their ORIGINAL keys; the errored line is skipped.
	if len(res) != 2 {
		t.Fatalf("collect returned %d results, want 2: %v", len(res), keysOf(res))
	}
	if r := res[safeKey]; r == nil || r.Text != "alpha" {
		t.Errorf("result[%q] = %+v, want text alpha", safeKey, r)
	}
	if r := res[badKey]; r == nil || r.Text != "beta" {
		t.Errorf("result[%q] (hashed) = %+v, want text beta", badKey, r)
	}
	if r := res[safeKey]; r != nil && (r.Usage.InputTokens != 5 || r.Usage.OutputTokens != 2) {
		t.Errorf("result[%q] usage = %+v, want in=5 out=2", safeKey, r.Usage)
	}
	// The same-process hashed map must be cleaned up after collect.
	if _, ok := batchKeyMaps.Load(id); ok {
		t.Errorf("batchKeyMaps still holds %q after collect", id)
	}
}

// isValidWireCustomID checks the raw Anthropic custom_id constraints (non-empty,
// ≤64 chars, [a-zA-Z0-9_-]) without the passthrough-namespace guard.
func isValidWireCustomID(id string) bool {
	if id == "" || len(id) > maxCustomIDLen {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func keysOf(m map[string]*Response) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// jsonString quotes s as a JSON string literal (small helper to keep the inline
// results_url JSON readable above).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
