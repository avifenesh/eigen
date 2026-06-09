package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// websearchTimeout bounds a single search request.
const websearchTimeout = 20 * time.Second

// maxSearchResults caps how many results are returned to the model.
const maxSearchResults = 8

// searchResult is one normalized web result.
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searchBackend performs a query and returns normalized results.
type searchBackend interface {
	name() string
	search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error)
}

// WebSearch returns the web-search tool and true when a backend is configured
// (via env), or a zero Definition and false when none is — so eigen only
// advertises the tool when it can actually run. Like fetch, it performs network
// egress and is treated as mutating (requires approval in gated mode).
//
// Backends, in resolution order:
//   - Tavily:  TAVILY_API_KEY            (POST https://api.tavily.com/search)
//   - Brave:   BRAVE_API_KEY             (GET  https://api.search.brave.com/...)
//   - Generic: EIGEN_WEBSEARCH_URL       (a URL template with %s or {query};
//              must return JSON; parsed leniently for title/url/snippet)
//
// Endpoint base URLs are overridable via EIGEN_TAVILY_URL / EIGEN_BRAVE_URL so
// the tool is testable against a local server.
func WebSearch() (Definition, bool) {
	be := resolveBackend()
	if be == nil {
		return Definition{}, false
	}
	hc := &http.Client{Timeout: websearchTimeout}
	return Definition{
		Name:        "websearch",
		Description: "Search the web and return ranked results (title, url, snippet) via the configured search backend (" + be.name() + "). Performs network access: requires approval in gated mode.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": { "type": "string", "description": "The search query." },
    "count": { "type": "integer", "description": "Max results to return (1-8, default 5)." }
  },
  "required": ["query"],
  "additionalProperties": false
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Query string `json:"query"`
				Count int    `json:"count"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			in.Query = strings.TrimSpace(in.Query)
			if in.Query == "" {
				return "", fmt.Errorf("query is required")
			}
			count := in.Count
			if count <= 0 {
				count = 5
			}
			if count > maxSearchResults {
				count = maxSearchResults
			}
			results, err := be.search(ctx, hc, in.Query, count)
			if err != nil {
				return "", err
			}
			if len(results) == 0 {
				return "(no results)", nil
			}
			return formatResults(results), nil
		},
	}, true
}

// resolveBackend picks the first configured backend from the environment.
func resolveBackend() searchBackend {
	if k := strings.TrimSpace(os.Getenv("TAVILY_API_KEY")); k != "" {
		return &tavilyBackend{key: k, base: envOr("EIGEN_TAVILY_URL", "https://api.tavily.com/search")}
	}
	if k := strings.TrimSpace(os.Getenv("BRAVE_API_KEY")); k != "" {
		return &braveBackend{key: k, base: envOr("EIGEN_BRAVE_URL", "https://api.search.brave.com/res/v1/web/search")}
	}
	if tmpl := strings.TrimSpace(os.Getenv("EIGEN_WEBSEARCH_URL")); tmpl != "" {
		return &genericBackend{template: tmpl}
	}
	return nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// formatResults renders results as compact numbered entries.
func formatResults(rs []searchResult) string {
	var b strings.Builder
	for i, r := range rs {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, strings.TrimSpace(r.Title), strings.TrimSpace(r.URL))
		if s := strings.TrimSpace(r.Snippet); s != "" {
			b.WriteString("   " + collapseWS(s) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// collapseWS flattens internal whitespace/newlines into single spaces.
func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// --- Tavily -----------------------------------------------------------------

type tavilyBackend struct {
	key  string
	base string
}

func (t *tavilyBackend) name() string { return "tavily" }

func (t *tavilyBackend) search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	body, _ := json.Marshal(map[string]any{
		"api_key":     t.key,
		"query":       query,
		"max_results": count,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily HTTP %d", resp.StatusCode)
	}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("tavily decode: %w", err)
	}
	results := make([]searchResult, 0, len(out.Results))
	for _, r := range out.Results {
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	return results, nil
}

// --- Brave ------------------------------------------------------------------

type braveBackend struct {
	key  string
	base string
}

func (b *braveBackend) name() string { return "brave" }

func (b *braveBackend) search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("q", query)
	q.Set("count", strconv.Itoa(count))
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.key)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("brave HTTP %d", resp.StatusCode)
	}
	var out struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("brave decode: %w", err)
	}
	results := make([]searchResult, 0, len(out.Web.Results))
	for _, r := range out.Web.Results {
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	return results, nil
}

// --- Generic JSON endpoint --------------------------------------------------

type genericBackend struct {
	template string // contains %s or {query}
}

func (g *genericBackend) name() string { return "custom" }

func (g *genericBackend) search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	url := g.template
	enc := queryEscape(query)
	if strings.Contains(url, "{query}") {
		url = strings.ReplaceAll(url, "{query}", enc)
	} else if strings.Contains(url, "%s") {
		url = strings.Replace(url, "%s", enc, 1)
	} else {
		// No placeholder: append as ?q=.
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url = url + sep + "q=" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if k := strings.TrimSpace(os.Getenv("EIGEN_WEBSEARCH_KEY")); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("websearch HTTP %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("websearch decode: %w", err)
	}
	results := parseGenericResults(raw)
	if len(results) > count {
		results = results[:count]
	}
	return results, nil
}

// parseGenericResults leniently extracts results from common JSON shapes: a
// top-level array, or an object with a "results"/"items"/"data" array. Each
// element's title/url/snippet are read from several common key spellings.
func parseGenericResults(raw json.RawMessage) []searchResult {
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		for _, key := range []string{"results", "items", "data", "web"} {
			if v, ok := obj[key]; ok {
				if json.Unmarshal(v, &arr) == nil && len(arr) > 0 {
					break
				}
				// "web": {"results": [...]}
				var nested map[string]json.RawMessage
				if json.Unmarshal(v, &nested) == nil {
					if rv, ok := nested["results"]; ok {
						_ = json.Unmarshal(rv, &arr)
					}
				}
			}
		}
	}
	out := make([]searchResult, 0, len(arr))
	for _, m := range arr {
		r := searchResult{
			Title:   firstString(m, "title", "name", "heading"),
			URL:     firstString(m, "url", "link", "href"),
			Snippet: firstString(m, "snippet", "description", "content", "text", "summary"),
		}
		if r.URL != "" || r.Title != "" {
			out = append(out, r)
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// queryEscape percent-encodes a query for a URL query value.
func queryEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('+')
		default:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}
