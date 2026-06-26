package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Version is eigen's base semantic version, reported in the User-Agent of
// provider requests and as the headline version everywhere. FullVersion()
// (version.go) annotates it with the build's git revision.
const Version = "0.2.4"

const (
	// maxAttempts bounds retries for transient provider failures (network, 429,
	// 5xx) — jittered exponential backoff between attempts, honoring Retry-After.
	maxAttempts = 5
	// maxResponseBytes caps how much of a response body we read into memory.
	maxResponseBytes = 16 << 20 // 16 MiB
)

// httpJSON POSTs body as JSON to url, retrying transient failures (network
// errors, HTTP 429, and 5xx) with jittered exponential backoff that honors a
// server Retry-After. It returns the response body and HTTP status; non-2xx
// statuses other than the retried ones are returned to the caller (with the
// body) so each provider can format its own error. Content-Type and User-Agent
// are set automatically; pass any auth headers in headers. If sign is non-nil
// it is called per attempt after headers are set (e.g. for SigV4), so each
// retry is freshly signed.
func httpJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte, sign func(*http.Request, []byte)) ([]byte, int, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
				return nil, 0, err
			}
			retryAfter = 0
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, 0, fmt.Errorf("build request: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "eigen/"+Version)
		if sign != nil {
			sign(req, body)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue // network error: retry
		}
		// Read one byte past the cap so an oversized body is detected rather than
		// silently truncated mid-JSON (which would surface as an opaque
		// "unexpected end of JSON input" at the caller's json.Unmarshal).
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		io.Copy(io.Discard, resp.Body) // drain so the connection can be reused
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}
		if len(raw) > maxResponseBytes {
			return nil, resp.StatusCode, fmt.Errorf("response exceeded %d MiB cap", maxResponseBytes>>20)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
			continue // transient: retry
		}
		return raw, resp.StatusCode, nil
	}
	return nil, 0, fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

// sleepBackoff waits for an exponential backoff (honoring a server Retry-After
// when larger) plus jitter, or returns early if the context is cancelled.
func sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := time.Duration(1<<(attempt-1)) * time.Second
	if retryAfter > delay {
		delay = retryAfter
	}
	delay += time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// parseRetryAfter parses a Retry-After header as delta-seconds or an HTTP-date.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// httpStream POSTs body and, on a 2xx, returns the open response for the caller
// to stream (the caller must Close the body). The initial connect is retried on
// transient failures (network, 429, 5xx); once streaming begins there is no
// retry. Non-2xx responses are read and returned as an error.
func httpStream(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte, sign func(*http.Request, []byte)) (*http.Response, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
				return nil, err
			}
			retryAfter = 0
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("User-Agent", "eigen/"+Version)
		if sign != nil {
			sign(req, body)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
			continue
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}
