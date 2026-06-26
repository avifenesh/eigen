package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// restTimeout bounds a single Google API call.
const restTimeout = 15 * time.Second

// httpClient returns an *http.Client that attaches a fresh bearer token to every
// request (refreshing transparently). Errors when not connected/configured.
func (a *Auth) httpClient(ctx context.Context) (*http.Client, error) {
	src, err := a.tokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, src), nil
}

// getJSON performs an authorized GET against a Google API and decodes JSON.
func (a *Auth) getJSON(ctx context.Context, rawurl string, out any) error {
	hc, err := a.httpClient(ctx)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, restTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google API %s: HTTP %d: %s", trimURL(rawurl), resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

// postJSON performs an authorized POST with a JSON body.
func (a *Auth) postJSON(ctx context.Context, rawurl string, in any, out any) error {
	hc, err := a.httpClient(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, restTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, rawurl, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google API %s: HTTP %d: %s", trimURL(rawurl), resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func trimURL(s string) string {
	if u, err := url.Parse(s); err == nil {
		return u.Host + u.Path
	}
	return s
}
