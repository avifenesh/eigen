package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Web-search engines + the fallback chain. Ported natively to Go from the
// user's @agent-sh/harness-websearch v2 design (no MCP/runtime dep — eigen
// stays a single static binary). The headline: search works KEYLESS, zero
// config, via a fallback chain (Mojeek → Marginalia → Wikipedia); a configured
// Brave/Tavily key or a self-hosted SearXNG becomes the preferred head.
//
// Every engine implements searchEngine; the chain (searchChain) tries them in
// order, accumulating + de-duplicating results until the requested count is met
// or the engines run out, with per-engine failure isolation. An SSRF host check
// runs before every request (loopback/LAN refused unless opted in).

// searchUserAgent is an honest UA (no browser spoofing) — verified accepted by
// Mojeek + MediaWiki.
const searchUserAgent = "eigen-websearch/1.0 (+https://github.com/avifenesh/eigen)"

// engineClass labels an engine's coverage so the chain can reason about an
// empty result (a broad-web "nothing" is authoritative; an encyclopedic-only
// "nothing" while a general engine errored is degraded → keep trying).
type engineClass int

const (
	classGeneral  engineClass = iota // broad web: Mojeek, Brave, Tavily, SearXNG
	classNiche                       // Marginalia (small-web index)
	classVertical                    // Wikipedia (encyclopedic)
)

// searchEngine performs one query against one backend.
type searchEngine interface {
	name() string
	class() engineClass
	// host returns the backend host (for the SSRF check + provenance).
	host() string
	search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error)
}

// searchChain runs engines best-first, gathering + de-duplicating results.
//   - Fast path: if the FIRST engine alone returns >= count, return it (no mix).
//   - Otherwise accumulate across engines (dedup by normalized URL) until count
//     is met or engines run out; first engine to surface a URL owns the row.
//   - Per-engine failure is isolated (the chain moves on). An engine-class-aware
//     empty: a general engine erroring while only a niche/vertical engine
//     returned empty is "degraded" → surface the error so the model retries,
//     rather than trusting an encyclopedic-empty as "the web had nothing".
type searchChain struct {
	engines   []searchEngine
	checkSSRF func(host string) error // nil = allow all (tests)
}

func (c *searchChain) run(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	var (
		acc        []searchResult
		seen       = map[string]bool{}
		lastErr    error
		generalErr bool // a general-class engine failed
		anyEmpty   bool // some engine returned successfully-but-empty
	)
	for i, e := range c.engines {
		if ctx.Err() != nil {
			break
		}
		if c.checkSSRF != nil {
			if err := c.checkSSRF(e.host()); err != nil {
				lastErr = fmt.Errorf("%s: %w", e.name(), err)
				if e.class() == classGeneral {
					generalErr = true
				}
				continue
			}
		}
		// Per-engine timeout slice: split the remaining budget over the
		// remaining engines so one slow engine can't starve the rest (WS-D11).
		ectx, cancel := c.engineCtx(ctx, len(c.engines)-i)
		res, err := e.search(ectx, hc, query, count)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", e.name(), err)
			if e.class() == classGeneral {
				generalErr = true
			}
			continue
		}
		if len(res) == 0 {
			anyEmpty = true
			// A general engine's empty is authoritative ("the web had nothing").
			if e.class() == classGeneral {
				return nil, nil
			}
			continue
		}
		// Fast path: a sufficient first engine short-circuits (single-engine
		// provenance, no added latency).
		if i == 0 && len(res) >= count {
			return res[:count], nil
		}
		for _, r := range res {
			k := normalizeURL(r.URL)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			acc = append(acc, r)
			if len(acc) >= count {
				return acc, nil
			}
		}
	}
	if len(acc) > 0 {
		return acc, nil
	}
	// Nothing accumulated. If a general engine errored and only a niche/vertical
	// engine reported empty, that's degraded — surface the error so the model
	// retries rather than trusting an encyclopedic-empty.
	if anyEmpty && !generalErr {
		return nil, nil // a niche/vertical empty with no general failure = best signal
	}
	return nil, lastErr // all failed (or degraded): report it
}

// engineCtx derives a per-engine context that gets a fair slice of the parent's
// remaining deadline (the remaining budget / remaining engines), so one slow
// engine can't consume the whole budget. Falls back to a fixed slice when the
// parent has no deadline.
func (c *searchChain) engineCtx(parent context.Context, remaining int) (context.Context, context.CancelFunc) {
	if remaining < 1 {
		remaining = 1
	}
	if dl, ok := parent.Deadline(); ok {
		slice := time.Until(dl) / time.Duration(remaining)
		if slice < 500*time.Millisecond {
			slice = 500 * time.Millisecond
		}
		return context.WithTimeout(parent, slice)
	}
	return context.WithTimeout(parent, searchClientTimeout/time.Duration(remaining))
}

// normalizeURL keys a result for dedup: lowercase scheme/host, drop www. and
// default ports, strip the fragment + tracking params, sort the query. Keeps
// meaningful params so distinct pages don't merge.
func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimSuffix(host, ":80")
	host = strings.TrimSuffix(host, ":443")
	u.Host = host
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "gclid" || lk == "fbclid" || lk == "ref" || lk == "ref_src" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode() // url.Values.Encode sorts by key
	p := strings.TrimSuffix(u.Path, "/")
	u.Path = p
	return u.String()
}

// --- the keyless engines (always available) ---------------------------------

// marginaliaEngine — keyless JSON: GET {base}/public/search/{q}?count=N →
// {results:[{title,url,description,quality}]}. Niche "small-web" index.
type marginaliaEngine struct{ base string }

func (m *marginaliaEngine) name() string       { return "marginalia" }
func (m *marginaliaEngine) class() engineClass { return classNiche }
func (m *marginaliaEngine) host() string       { return hostOf(m.base, "api.marginalia.nu") }

func (m *marginaliaEngine) search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	base := strings.TrimRight(envOr2(m.base, "https://api.marginalia.nu"), "/")
	u := base + "/public/search/" + url.PathEscape(query) + "?count=" + strconv.Itoa(count)
	raw, err := searchHTTPGet(ctx, hc, u, "application/json", "marginalia", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("marginalia: bad JSON: %w", err)
	}
	results := make([]searchResult, 0, len(out.Results))
	for _, r := range out.Results {
		if r.Title == "" || r.URL == "" {
			continue
		}
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Snippet: stripTags(r.Description)})
	}
	return results, nil
}

// wikipediaEngine — keyless MediaWiki JSON: the encyclopedic backstop that
// (with an honest UA) effectively never anti-bot-challenges.
type wikipediaEngine struct{ base string }

func (w *wikipediaEngine) name() string       { return "wikipedia" }
func (w *wikipediaEngine) class() engineClass { return classVertical }
func (w *wikipediaEngine) host() string       { return hostOf(w.base, "en.wikipedia.org") }

func (w *wikipediaEngine) search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	base := strings.TrimRight(envOr2(w.base, "https://en.wikipedia.org"), "/")
	q := url.Values{}
	q.Set("action", "query")
	q.Set("list", "search")
	q.Set("srsearch", query)
	q.Set("srlimit", strconv.Itoa(count))
	q.Set("format", "json")
	u := base + "/w/api.php?" + q.Encode()
	raw, err := searchHTTPGet(ctx, hc, u, "application/json", "wikipedia", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				PageID  int    `json:"pageid"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("wikipedia: bad JSON: %w", err)
	}
	results := make([]searchResult, 0, len(out.Query.Search))
	for _, r := range out.Query.Search {
		if r.Title == "" || r.PageID == 0 {
			continue
		}
		results = append(results, searchResult{
			Title:   r.Title,
			URL:     fmt.Sprintf("%s/?curid=%d", base, r.PageID),
			Snippet: stripTags(r.Snippet),
		})
	}
	return results, nil
}

// mojeekEngine — keyless HTML SERP scrape. Independent full-web crawl with
// mainstream coverage; ToS-gray (robots disallows /search), so it's opt-OUT via
// EIGEN_WEBSEARCH_NO_MOJEEK. Honest UA (no browser spoofing).
type mojeekEngine struct{ base string }

func (m *mojeekEngine) name() string       { return "mojeek" }
func (m *mojeekEngine) class() engineClass { return classGeneral }
func (m *mojeekEngine) host() string       { return hostOf(m.base, "www.mojeek.com") }

func (m *mojeekEngine) search(ctx context.Context, hc *http.Client, query string, count int) ([]searchResult, error) {
	base := strings.TrimRight(envOr2(m.base, "https://www.mojeek.com"), "/")
	u := base + "/search?q=" + url.QueryEscape(query)
	raw, err := searchHTTPGet(ctx, hc, u, "text/html,application/xhtml+xml", "mojeek", nil)
	if err != nil {
		return nil, err
	}
	html := string(raw)
	results := parseMojeek(html)
	if len(results) > count {
		results = results[:count]
	}
	// A real SERP (even zero-hit) carries the result scaffold; an anti-bot
	// interstitial does not — only the latter should fail the engine.
	if len(results) == 0 && mojeekChallenged(html) {
		return nil, fmt.Errorf("mojeek: no parseable results (likely an anti-bot interstitial from this IP)")
	}
	return results, nil
}

// parseMojeek extracts result blocks (delimited by <!--rs-->…<!--re-->, each
// with <a class="title" href> + <p class="s">).
func parseMojeek(html string) []searchResult {
	var out []searchResult
	rest := html
	for {
		start := strings.Index(rest, "<!--rs-->")
		if start < 0 {
			break
		}
		after := rest[start+len("<!--rs-->"):]
		end := strings.Index(after, "<!--re-->")
		if end < 0 {
			break
		}
		block := after[:end]
		rest = after[end+len("<!--re-->"):]

		u, title := mojeekTitleAnchor(block)
		if u == "" || title == "" {
			continue
		}
		out = append(out, searchResult{URL: u, Title: title, Snippet: mojeekSnippet(block)})
	}
	return out
}

// mojeekTitleAnchor finds <a … class="title" … href="URL" …>TITLE</a>.
func mojeekTitleAnchor(block string) (url, title string) {
	from := 0
	for {
		rel := strings.Index(block[from:], "<a ")
		if rel < 0 {
			return "", ""
		}
		tagStart := from + rel
		gt := strings.IndexByte(block[tagStart:], '>')
		if gt < 0 {
			return "", ""
		}
		tagEnd := tagStart + gt
		tag := block[tagStart : tagEnd+1]
		if strings.Contains(tag, `class="title"`) {
			href := attrValue(tag, "href")
			if href == "" {
				return "", ""
			}
			after := block[tagEnd+1:]
			close := strings.Index(after, "</a>")
			if close < 0 {
				return "", ""
			}
			return strings.ReplaceAll(href, "&amp;", "&"), stripTags(after[:close])
		}
		from = tagEnd + 1
	}
}

func mojeekSnippet(block string) string {
	const marker = `<p class="s">`
	i := strings.Index(block, marker)
	if i < 0 {
		return ""
	}
	after := block[i+len(marker):]
	end := strings.Index(after, "</p>")
	if end < 0 {
		return ""
	}
	return stripTags(after[:end])
}

func attrValue(tag, attr string) string {
	needle := attr + `="`
	i := strings.Index(tag, needle)
	if i < 0 {
		return ""
	}
	after := tag[i+len(needle):]
	end := strings.IndexByte(after, '"')
	if end < 0 {
		return ""
	}
	return after[:end]
}

func mojeekChallenged(html string) bool {
	lower := strings.ToLower(html)
	scaffold := strings.Contains(html, "results-standard") ||
		strings.Contains(html, "serp-results") ||
		strings.Contains(html, "results-count") ||
		strings.Contains(lower, "no pages found")
	return !scaffold
}

// --- shared HTTP + helpers ---------------------------------------------------

// searchHTTPGet GETs url with the honest UA + accept header, mapping non-2xx to
// errors the same way across engines: 5xx/429/401/403 → "unavailable" (a
// per-engine failure the chain skips), other 4xx → a query rejection.
func searchHTTPGet(ctx context.Context, hc *http.Client, url, accept, engine string, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", searchUserAgent)
	req.Header.Set("Accept", accept)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if resp.StatusCode >= 500 || resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode == 403 {
			suffix := ""
			if resp.StatusCode == 429 || resp.StatusCode == 403 {
				suffix = "; rate-limited or bot-blocked"
			}
			return nil, fmt.Errorf("%s unavailable (HTTP %d%s)", engine, resp.StatusCode, suffix)
		}
		return nil, fmt.Errorf("%s rejected the query (HTTP %d)", engine, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSearchBodyBytes))
}

// maxSearchBodyBytes caps a response body (a Mojeek SERP is the largest).
const maxSearchBodyBytes = 4 << 20

// envOr2 returns v when non-empty, else def (for overridable engine base URLs).
func envOr2(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// hostOf returns the host of base, or def when base is empty/unparseable.
func hostOf(base, def string) string {
	if strings.TrimSpace(base) == "" {
		return def
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Hostname()
	}
	return def
}

// stripTags removes HTML tags + decodes the few entities the engines emit, then
// collapses whitespace — for snippets that arrive as HTML (Marginalia,
// Wikipedia, Mojeek).
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&quot;", "\"")
	out = strings.ReplaceAll(out, "&#39;", "'")
	out = strings.ReplaceAll(out, "&nbsp;", " ")
	return strings.Join(strings.Fields(out), " ")
}

// --- SSRF host check ---------------------------------------------------------

// ssrfCheck refuses a backend host that resolves into a blocked IP range
// (loopback, link-local/metadata, RFC-1918 private, CGNAT, reserved), unless
// the matching opt-in env is set. A self-hosted SearXNG on localhost is the
// routine opt-in (EIGEN_WEBSEARCH_ALLOW_LOOPBACK=1). Runs before every request.
func ssrfCheck(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("empty backend host")
	}
	var addrs []string
	if ip := net.ParseIP(host); ip != nil {
		addrs = []string{host}
	} else {
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
		}
		for _, ip := range ips {
			addrs = append(addrs, ip.String())
		}
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%q did not resolve to any address", host)
	}
	for _, a := range addrs {
		block := classifyIP(a)
		if block == "" {
			continue
		}
		if !ssrfOptedIn(block) {
			return fmt.Errorf("backend %q resolved to a blocked %s address (%s); set %s=1 to allow", host, block, a, ssrfEnvFor(block))
		}
	}
	return nil
}

// classifyIP returns a block class ("loopback"/"metadata"/"private"/
// "link-local"/"reserved") for an address, or "" if it's the safe public net.
func classifyIP(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return "reserved" // unparseable → blocked
	}
	if ip.IsLoopback() {
		return "loopback"
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		// 169.254.0.0/16 (incl. the 169.254.169.254 cloud metadata IP) + fe80::/10
		return "link-local"
	}
	if ip.IsUnspecified() {
		return "reserved"
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10:
			return "private"
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return "private"
		case v4[0] == 192 && v4[1] == 168:
			return "private"
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return "private" // CGNAT 100.64.0.0/10
		case v4[0] == 0:
			return "reserved"
		case addr == "255.255.255.255":
			return "reserved"
		}
		return ""
	}
	// IPv6: fc00::/7 unique-local.
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return "private"
	}
	return ""
}

// ssrfOptedIn reports whether the env opts into a blocked class.
func ssrfOptedIn(block string) bool {
	switch block {
	case "loopback":
		return envTrue("EIGEN_WEBSEARCH_ALLOW_LOOPBACK")
	case "private", "link-local":
		return envTrue("EIGEN_WEBSEARCH_ALLOW_PRIVATE")
	default:
		return false // reserved/metadata are never opt-in-able here
	}
}

func ssrfEnvFor(block string) string {
	if block == "loopback" {
		return "EIGEN_WEBSEARCH_ALLOW_LOOPBACK"
	}
	return "EIGEN_WEBSEARCH_ALLOW_PRIVATE"
}

// envTrue reports whether an env var is set to a truthy value.
func envTrue(key string) bool {
	v := strings.ToLower(strings.TrimSpace(getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// (getenv is a tiny indirection so tests can stub the env; defaults to os.Getenv.)
var getenv = os.Getenv

// searchClientTimeout is the overall per-search HTTP budget; the chain splits
// it so one slow engine can't starve the rest.
const searchClientTimeout = 12 * time.Second
