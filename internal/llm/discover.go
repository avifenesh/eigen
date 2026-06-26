package llm

// Model auto-discovery: probe each provider the user has credentials for and
// report models the catalog doesn't know yet. Read-only — discovery informs
// the user (eigen models); it never mutates the catalog (curated entries carry
// capability metadata a listing can't provide). Providers without a listing
// endpoint or credentials are skipped silently.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Discovered is one provider's listing: models the provider offers, split by
// whether the catalog already knows them.
type Discovered struct {
	Provider string
	Known    []string // in the catalog
	New      []string // offered by the provider but not in the catalog
	Err      error    // listing failed (no creds, network, etc.)
}

// Discover probes all providers with reachable credentials. Order is stable.
func Discover(ctx context.Context) []Discovered {
	type prober struct {
		name string
		list func(context.Context) ([]string, error)
	}
	probers := []prober{
		{"anthropic", listAnthropicModels},
		{"converse", listBedrockModels},
		{"grok", listGrokModels},
		{"glm", listGLMModels},
		{"llama", listLlamaModels},
	}
	var out []Discovered
	for _, p := range probers {
		ids, err := p.list(ctx)
		if err != nil {
			// No credentials/endpoint: skip quietly; real errors are reported.
			if isSkippable(err) {
				continue
			}
			out = append(out, Discovered{Provider: p.name, Err: err})
			continue
		}
		d := Discovered{Provider: p.name}
		for _, id := range ids {
			if _, ok := Lookup(id); ok {
				d.Known = append(d.Known, id)
			} else {
				d.New = append(d.New, id)
			}
		}
		sort.Strings(d.Known)
		sort.Strings(d.New)
		out = append(out, d)
	}
	return out
}

// errSkip marks "this provider isn't configured here" — not an error worth
// showing, just absence.
type errSkip struct{ reason string }

func (e errSkip) Error() string { return e.reason }

func isSkippable(err error) bool {
	if _, ok := err.(errSkip); ok {
		return true
	}
	// A local endpoint that isn't running (connection refused) is "not
	// configured here", not a real error worth reporting.
	s := err.Error()
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no such host")
}

// discoverClient is a short-timeout client for listing calls.
var discoverClient = &http.Client{Timeout: 15 * time.Second}

// listJSON fetches url with headers and decodes {"data":[{"id":...}]} or
// {"models":[{"name"|"id"|"modelId":...}]} style listings leniently.
func listJSON(ctx context.Context, url string, headers map[string]string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := discoverClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			ModelID string `json:"modelId"`
		} `json:"models"`
		ModelSummaries []struct {
			ModelID string `json:"modelId"`
		} `json:"modelSummaries"`
	}
	raw, err := readAll(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncErr(raw))
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("decode listing: %w", err)
	}
	var ids []string
	for _, d := range body.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	for _, m := range body.Models {
		id := firstNonEmpty(m.ID, m.ModelID, m.Name)
		if id != "" {
			ids = append(ids, id)
		}
	}
	for _, m := range body.ModelSummaries {
		if m.ModelID != "" {
			ids = append(ids, m.ModelID)
		}
	}
	return ids, nil
}

func readAll(resp *http.Response) ([]byte, error) {
	const maxList = 2 << 20 // 2MB is plenty for a model listing
	buf := make([]byte, 0, 64<<10)
	tmp := make([]byte, 32<<10)
	for len(buf) < maxList {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

func truncErr(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// --- per-provider listings ---------------------------------------------------

// listAnthropicModels uses GET /v1/models with the same auth as the chat path
// (ANTHROPIC_API_KEY or the Claude Code OAuth token).
func listAnthropicModels(ctx context.Context) ([]string, error) {
	headers := map[string]string{"anthropic-version": anthropicVersion}
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		headers["x-api-key"] = k
	} else if tok, err := claudeOAuthToken(firstNonEmpty(os.Getenv("EIGEN_CLAUDE_CREDENTIALS"), claudeCredentialsPath())); err == nil && tok != "" {
		headers["Authorization"] = "Bearer " + tok
		headers["anthropic-beta"] = strings.Join(anthropicBetas, ",")
	} else {
		return nil, errSkip{"no anthropic credentials"}
	}
	return listJSON(ctx, "https://api.anthropic.com/v1/models?limit=100", headers)
}

// bedrockDiscoverProfile resolves the AWS profile for Bedrock discovery the
// same way NewConverse does, so discovery probes the account chat actually
// uses: EIGEN_CONVERSE_PROFILE, then AWS_PROFILE, then "aviary".
func bedrockDiscoverProfile() string {
	return firstNonEmpty(os.Getenv("EIGEN_CONVERSE_PROFILE"), os.Getenv("AWS_PROFILE"), "aviary")
}

// listBedrockModels uses the Bedrock control-plane ListInferenceProfiles.
// Inference profiles are what eigen actually invokes (us./global. prefixes).
// Auth mirrors NewConverse: prefer the Bedrock bearer token
// (AWS_BEARER_TOKEN_BEDROCK) via an Authorization header, else SigV4 from the
// converse profile (EIGEN_CONVERSE_PROFILE / AWS_PROFILE / aviary). Using the
// same profile/token as chat means discovery probes the account the user
// actually talks to, not a stray "default".
func listBedrockModels(ctx context.Context) ([]string, error) {
	region := firstNonEmpty(os.Getenv("EIGEN_CONVERSE_REGION"), os.Getenv("AWS_REGION"), "us-east-2")
	bearer := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	var creds awsCreds
	if bearer == "" {
		var err error
		creds, err = loadAWSCreds(bedrockDiscoverProfile())
		if err != nil {
			return nil, errSkip{"no aws credentials"}
		}
	}
	get := func(url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		} else {
			signV4(req, nil, creds, "bedrock", region, time.Now())
		}
		resp, err := discoverClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		raw, _ := readAll(resp)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncErr(raw))
		}
		return raw, nil
	}
	var ids []string
	// Inference profiles (the invokable ids).
	if raw, err := get(fmt.Sprintf("https://bedrock.%s.amazonaws.com/inference-profiles?maxResults=100", region)); err == nil {
		var body struct {
			Summaries []struct {
				ID string `json:"inferenceProfileId"`
			} `json:"inferenceProfileSummaries"`
		}
		if json.Unmarshal(raw, &body) == nil {
			for _, s := range body.Summaries {
				if s.ID != "" {
					ids = append(ids, s.ID)
				}
			}
		}
	} else {
		return nil, err
	}
	return ids, nil
}

// listGrokModels uses the OpenAI-compatible /v1/models with XAI auth (public
// API only — the CLI proxy doesn't expose a listing).
func listGrokModels(ctx context.Context) ([]string, error) {
	key := firstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("EIGEN_GROK_API_KEY"))
	if key == "" {
		return nil, errSkip{"no xai api key"}
	}
	base := firstNonEmpty(os.Getenv("EIGEN_GROK_BASE_URL"), grokDefaultBaseURL)
	return listJSON(ctx, strings.TrimRight(base, "/")+"/models", map[string]string{"Authorization": "Bearer " + key})
}

// listGLMModels uses the OpenAI-compatible /models on Zhipu's API root.
func listGLMModels(ctx context.Context) ([]string, error) {
	key := firstNonEmpty(os.Getenv("GLM_API_KEY"), os.Getenv("ZHIPUAI_API_KEY"))
	if key == "" {
		return nil, errSkip{"no glm api key"}
	}
	base := firstNonEmpty(os.Getenv("EIGEN_GLM_BASE_URL"), glmDefaultBaseURL)
	return listJSON(ctx, strings.TrimRight(base, "/")+"/models", map[string]string{"Authorization": "Bearer " + key})
}

// listLlamaModels lists the local llama-server's models (only when configured).
func listLlamaModels(ctx context.Context) ([]string, error) {
	base := os.Getenv("EIGEN_LLAMA_BASE_URL")
	if base == "" {
		return nil, errSkip{"no local llama configured"}
	}
	headers := map[string]string{}
	if k := os.Getenv("EIGEN_LLAMA_API_KEY"); k != "" {
		headers["Authorization"] = "Bearer " + k
	}
	return listJSON(ctx, strings.TrimRight(base, "/")+"/models", headers)
}
