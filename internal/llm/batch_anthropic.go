package llm

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Anthropic Messages Batches: eigen's first BatchProvider. A batch lets the
// dream pipeline submit many independent prompts now and collect them within
// the provider's window (~minutes–hours) at roughly half the per-token input
// price. The wire format (verified against the documented Messages Batches API):
//
//	SubmitBatch  → POST /v1/messages/batches  {"requests":[{"custom_id","params"}...]}
//	                                            → {"id":"msgbatch_...", ...}
//	PollBatch    → GET  /v1/messages/batches/<id>
//	                  → {"processing_status":"in_progress|canceling|ended",
//	                     "request_counts":{processing,succeeded,errored,canceled,expired}}
//	CollectBatch → GET  the batch's "results_url" → JSONL, one line per request:
//	                  {"custom_id":"...","result":{"type":"succeeded|errored|...",
//	                                                "message":{<a full messages reply>}}}
//
// auth + the claude-code spoof come for free by reusing headers() and buildBody()
// from anthropic.go — the OAuth gate rejects per-request params that lack the
// spoof system block, and buildBody adds it via systemBlocks.

const (
	// maxBatchResultsBytes caps how much of the JSONL results stream we read.
	maxBatchResultsBytes = 256 << 20 // 256 MiB
	// maxCustomIDLen is the Anthropic limit on custom_id (≤64 chars, [a-zA-Z0-9_-]).
	maxCustomIDLen = 64
)

// Compile-time proof that *Anthropic satisfies BatchProvider, so AsBatch (and the
// dream pipeline) selects the batch path for the native Anthropic provider.
var _ BatchProvider = (*Anthropic)(nil)

// anthropicBatchURL is the batches collection endpoint (POST to create, GET
// <url>/<id> to retrieve). var, not const, so tests can point it at httptest.
var anthropicBatchURL = "https://api.anthropic.com/v1/messages/batches"

// batchKeyMaps is the same-process fast path for reversing a HASHED custom_id
// back to the original item Key. The key-sanitize transform is stateless and
// deterministic (safeCustomID) — a passthrough id IS the key, needing no map at
// all — but a hashed id ("h-...") can only be reversed by re-deriving over the
// candidate keys. In the common path submit and collect run in the same process,
// so SubmitBatch records hashedID→Key here keyed by batch id, and CollectBatch
// reads it. If the daemon RESTARTED between submit and collect the map is gone;
// collectKey then falls back to "custom_id is the key" (true for every
// passthrough id, and harmless for a hashed id — the dream caller simply
// reprocesses the unrecognized key synchronously). Keys with bad chars or >64
// chars are the only ones that ever hash, so a restart loses at most those.
//
// map[batchID]map[hashedCustomID]originalKey. A package-level sync.Map (rather
// than a field on Anthropic) keeps anthropic.go untouched.
var batchKeyMaps sync.Map

// --- batch wire types ---

// anthropicBatchRequest is one entry in the submit payload. params is the EXACT
// single-message body buildBody produces minus the streaming flag (model/system/
// messages/tools/thinking, including the claude-code spoof), carried verbatim.
type anthropicBatchRequest struct {
	CustomID string          `json:"custom_id"`
	Params   json.RawMessage `json:"params"`
}

type anthropicBatchSubmit struct {
	Requests []anthropicBatchRequest `json:"requests"`
}

// anthropicBatchObject is the batch resource returned by create + retrieve. Only
// the fields eigen needs are decoded; the API carries more.
type anthropicBatchObject struct {
	ID               string `json:"id"`
	ProcessingStatus string `json:"processing_status"` // in_progress | canceling | ended
	RequestCounts    struct {
		Processing int `json:"processing"`
		Succeeded  int `json:"succeeded"`
		Errored    int `json:"errored"`
		Canceled   int `json:"canceled"`
		Expired    int `json:"expired"`
	} `json:"request_counts"`
	ResultsURL *string `json:"results_url"` // populated once processing has ended
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicBatchResultLine is one JSONL line from the results_url stream. result.type
// is succeeded | errored | canceled | expired; only succeeded lines carry a usable
// message (a full native Messages reply, decoded as anthropicReply).
type anthropicBatchResultLine struct {
	CustomID string `json:"custom_id"`
	Result   struct {
		Type    string         `json:"type"`
		Message anthropicReply `json:"message"`
	} `json:"result"`
}

// batchParams returns the per-request params: the SINGLE-message body buildBody
// produces with the streaming flag off. Reusing buildBody guarantees the params
// carry the model, max_tokens, messages, thinking, AND the claude-code spoof
// system block (added inside buildBody via systemBlocks) — the spoof is REQUIRED
// or the OAuth gate rejects the request. The result is valid JSON, so it folds
// straight into the submit payload as a json.RawMessage with no re-marshal.
func (a *Anthropic) batchParams(req Request) (json.RawMessage, error) {
	body, err := a.buildBody(req, false)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

// safeCustomID maps an arbitrary item Key to a valid Anthropic custom_id —
// unique, ≤64 chars, drawn from [a-zA-Z0-9_-]. The transform is DELIBERATELY
// STATELESS and deterministic so CollectBatch can reverse it without any
// in-process map surviving a daemon restart between submit and collect:
//
//   - A key that is already safe (all chars in the set, ≤64) passes through
//     unchanged — the common case, and trivially reversible (id == key).
//   - Anything else is hashed: "h-" + first 16 hex chars of sha256(key). The
//     "h-" prefix marks it as hashed so collectKey knows the id is NOT the key.
//
// reversible reports whether the id round-trips to the original key by itself.
// When it doesn't (a hashed id), the caller keeps a local id→key map for the
// same-process fast path; collectKey falls back to "id is the key" otherwise.
func safeCustomID(key string) (id string, reversible bool) {
	if isSafeCustomID(key) {
		return key, true
	}
	sum := sha256.Sum256([]byte(key))
	return "h-" + hex.EncodeToString(sum[:])[:16], false
}

// isSafeCustomID reports whether key is a valid custom_id as-is: non-empty,
// ≤64 chars, every char in [a-zA-Z0-9_-], and not colliding with the "h-"
// hashed-id namespace (so a passthrough id is never mistaken for a hash).
func isSafeCustomID(key string) bool {
	if key == "" || len(key) > maxCustomIDLen || strings.HasPrefix(key, "h-") {
		return false
	}
	for _, c := range key {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// SubmitBatch creates a Messages batch from items and returns the batch id
// (msgbatch_...). custom_id is the deterministic safeCustomID of each item Key,
// so CollectBatch can map results back without persisted state. Any error →
// ("", err): the dream caller falls back to a synchronous Complete loop.
func (a *Anthropic) SubmitBatch(ctx context.Context, items []BatchItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("anthropic batch: no items")
	}
	reqs := make([]anthropicBatchRequest, 0, len(items))
	// hashed collects only the keys that needed hashing (bad chars / too long), so
	// the same-process collect can reverse them; passthrough ids reverse for free.
	var hashed map[string]string
	for _, it := range items {
		params, err := a.batchParams(it.Req)
		if err != nil {
			return "", fmt.Errorf("anthropic batch: build params for %q: %w", it.Key, err)
		}
		id, reversible := safeCustomID(it.Key)
		if !reversible {
			if hashed == nil {
				hashed = make(map[string]string)
			}
			hashed[id] = it.Key
		}
		reqs = append(reqs, anthropicBatchRequest{CustomID: id, Params: params})
	}
	body, err := json.Marshal(anthropicBatchSubmit{Requests: reqs})
	if err != nil {
		return "", fmt.Errorf("anthropic batch: marshal submit: %w", err)
	}
	headers, err := a.headers()
	if err != nil {
		return "", err
	}
	raw, status, err := httpJSON(ctx, a.http, anthropicBatchURL, headers, body, nil)
	if err != nil {
		return "", fmt.Errorf("anthropic batch submit: %w", err)
	}
	var obj anthropicBatchObject
	if jerr := json.Unmarshal(raw, &obj); jerr != nil {
		return "", fmt.Errorf("anthropic batch submit: decode response: %w", jerr)
	}
	if status < 200 || status >= 300 {
		if obj.Error != nil {
			return "", fmt.Errorf("anthropic batch submit HTTP %d: %s: %s", status, obj.Error.Type, obj.Error.Message)
		}
		return "", fmt.Errorf("anthropic batch submit HTTP %d: %s", status, string(raw))
	}
	if obj.ID == "" {
		return "", fmt.Errorf("anthropic batch submit: response missing id")
	}
	if len(hashed) > 0 {
		batchKeyMaps.Store(obj.ID, hashed)
	}
	return obj.ID, nil
}

// PollBatch fetches the batch object and maps processing_status + request_counts
// to a BatchStatus snapshot. in_progress → BatchProcessing; ended → BatchDone; a
// whole-batch error → BatchFailed. Done is the terminal-per-request count
// (succeeded+errored+canceled+expired); Total adds the still-processing count.
func (a *Anthropic) PollBatch(ctx context.Context, id string) (BatchStatus, error) {
	obj, raw, status, err := a.fetchBatch(ctx, id)
	if err != nil {
		return BatchStatus{}, err
	}
	if status < 200 || status >= 300 {
		if obj.Error != nil {
			return BatchStatus{}, fmt.Errorf("anthropic batch poll HTTP %d: %s: %s", status, obj.Error.Type, obj.Error.Message)
		}
		return BatchStatus{}, fmt.Errorf("anthropic batch poll HTTP %d: %s", status, string(raw))
	}
	c := obj.RequestCounts
	done := c.Succeeded + c.Errored + c.Canceled + c.Expired
	st := BatchStatus{Done: done, Total: done + c.Processing}
	switch {
	case obj.Error != nil:
		st.State = BatchFailed
	case obj.ProcessingStatus == "ended":
		st.State = BatchDone
	default:
		// in_progress and canceling both mean "still in flight"; there is no
		// separate pending phase in this API.
		st.State = BatchProcessing
	}
	return st, nil
}

// CollectBatch fetches the batch's results_url and parses the JSONL stream,
// returning one *Response per succeeded line keyed by the ORIGINAL item Key.
// Non-succeeded lines (errored/canceled/expired) are skipped — the dream caller
// reprocesses any missing key synchronously. Each succeeded line's .message is
// decoded as an anthropicReply and folded through toResponse() — the EXACT block
// handling Complete uses. Any error → (nil, err).
func (a *Anthropic) CollectBatch(ctx context.Context, id string) (map[string]*Response, error) {
	obj, raw, status, err := a.fetchBatch(ctx, id)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		if obj.Error != nil {
			return nil, fmt.Errorf("anthropic batch collect HTTP %d: %s: %s", status, obj.Error.Type, obj.Error.Message)
		}
		return nil, fmt.Errorf("anthropic batch collect HTTP %d: %s", status, string(raw))
	}
	if obj.ResultsURL == nil || *obj.ResultsURL == "" {
		return nil, fmt.Errorf("anthropic batch collect: results not ready (status %q)", obj.ProcessingStatus)
	}

	// The results stream is JSONL (one JSON doc per line), which httpJSON can't
	// parse (it expects a single doc). Do a plain authenticated GET and scan the
	// body line by line, capped at maxBatchResultsBytes.
	headers, err := a.headers()
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, *obj.ResultsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic batch collect: build request: %w", err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("User-Agent", "eigen/"+Version)
	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic batch collect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return nil, fmt.Errorf("anthropic batch collect results HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Reverse hashed custom_ids via the same-process map recorded at submit; absent
	// (daemon restarted), every id reverses as itself (collectKey handles both).
	var hashed map[string]string
	if v, ok := batchKeyMaps.Load(id); ok {
		hashed, _ = v.(map[string]string)
	}

	out := make(map[string]*Response, obj.RequestCounts.Succeeded)
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxBatchResultsBytes))
	// A single result line can be large (a full reply with thinking + tool calls),
	// so allow a generous per-line buffer rather than bufio's 64 KiB default.
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var rl anthropicBatchResultLine
		if jerr := json.Unmarshal(line, &rl); jerr != nil {
			// One malformed line shouldn't sink the whole collect; skip it and let
			// the caller reprocess its key synchronously.
			continue
		}
		if rl.Result.Type != "succeeded" {
			continue
		}
		out[collectKey(rl.CustomID, hashed)] = rl.Result.Message.toResponse()
	}
	if serr := scanner.Err(); serr != nil {
		return nil, fmt.Errorf("anthropic batch collect: read results: %w", serr)
	}
	// Done with this batch's mapping; free it so the daemon doesn't leak entries.
	batchKeyMaps.Delete(id)
	return out, nil
}

// fetchBatch GETs /v1/messages/batches/<id> and decodes the batch object,
// returning the decoded object, the raw body, the HTTP status, and any transport
// error. Shared by PollBatch and CollectBatch.
func (a *Anthropic) fetchBatch(ctx context.Context, id string) (anthropicBatchObject, []byte, int, error) {
	headers, err := a.headers()
	if err != nil {
		return anthropicBatchObject{}, nil, 0, err
	}
	url := anthropicBatchURL + "/" + id
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return anthropicBatchObject{}, nil, 0, fmt.Errorf("anthropic batch: build request: %w", err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("User-Agent", "eigen/"+Version)
	resp, err := a.http.Do(httpReq)
	if err != nil {
		return anthropicBatchObject{}, nil, 0, fmt.Errorf("anthropic batch: %w", err)
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if rerr != nil {
		return anthropicBatchObject{}, nil, resp.StatusCode, fmt.Errorf("anthropic batch: read response: %w", rerr)
	}
	var obj anthropicBatchObject
	if jerr := json.Unmarshal(raw, &obj); jerr != nil {
		return anthropicBatchObject{}, raw, resp.StatusCode, fmt.Errorf("anthropic batch: decode response: %w", jerr)
	}
	return obj, raw, resp.StatusCode, nil
}

// collectKey reverses safeCustomID to recover the original item Key from a
// result line's custom_id:
//
//   - A passthrough id (not "h-" prefixed) IS the original key — return it. This
//     is stateless and the overwhelmingly common case, so it survives a daemon
//     restart between submit and collect with no persisted state.
//   - A hashed id ("h-...") is looked up in the same-process hashed map recorded
//     at submit. If present, return the original key.
//   - No mapping (hashed map absent after a restart, or unknown id) → fall back
//     to the id itself; a key the dream caller doesn't recognize is simply
//     reprocessed synchronously.
func collectKey(customID string, hashed map[string]string) string {
	if !strings.HasPrefix(customID, "h-") {
		return customID
	}
	if key, ok := hashed[customID]; ok {
		return key
	}
	return customID
}
