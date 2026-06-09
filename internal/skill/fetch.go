package skill

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxFetchBytes caps how much a remote SKILL.md may be (skills are small).
const maxFetchBytes = 512 * 1024

// DefaultFetcher fetches a URL with a short timeout and a byte cap — the
// production Fetcher for GitHub installs.
func DefaultFetcher(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "eigen/skill-install")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no SKILL.md found (HTTP 404)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFetchBytes {
		return nil, fmt.Errorf("SKILL.md too large (> %d bytes)", maxFetchBytes)
	}
	return data, nil
}
