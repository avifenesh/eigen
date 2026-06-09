package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxFetchBytes   = 256 * 1024
	defaultFetchTTL = 30 * time.Second
)

// Fetch returns the web-fetch tool: a GET against an HTTP(S) URL returning the
// response body as text (truncated). It performs network egress, so it is
// treated as mutating (requires approval in gated mode).
//
// NOTE: this does not implement SSRF protection beyond restricting the scheme;
// it can reach hosts on the local network. Run gated if that matters.
func Fetch() Definition {
	return Definition{
		Name:        "fetch",
		Description: "Fetch an HTTP(S) URL with a GET request and return the response body as text (truncated). Performs network access: requires approval in gated mode.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "Absolute http:// or https:// URL to fetch."
    }
  },
  "required": ["url"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.URL == "" {
				return "", fmt.Errorf("url is required")
			}
			u, err := url.Parse(in.URL)
			if err != nil {
				return "", fmt.Errorf("invalid url: %w", err)
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				return "", fmt.Errorf("unsupported url scheme %q (want http or https)", u.Scheme)
			}
			if u.Host == "" {
				return "", fmt.Errorf("url has no host")
			}

			ctx, cancel := context.WithTimeout(ctx, defaultFetchTTL)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "eigen/fetch")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
			if err != nil {
				return "", err
			}
			truncated := len(body) > maxFetchBytes
			if truncated {
				body = body[:maxFetchBytes]
			}
			if !utf8.Valid(body) {
				return fmt.Sprintf("HTTP %d %s\n[non-text body: %d bytes]", resp.StatusCode, resp.Header.Get("Content-Type"), len(body)), nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "HTTP %d\n", resp.StatusCode)
			b.Write(body)
			if truncated {
				b.WriteString("\n[truncated]")
			}
			return b.String(), nil
		},
	}
}
