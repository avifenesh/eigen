package llm

import "os"

// ProviderAvailable reports whether a provider has reachable credentials, so
// the router only ever picks models it can actually construct. It mirrors each
// provider's own credential resolution (cheaply — no network), and treats the
// mantle/converse Bedrock pair as available when AWS creds resolve.
func ProviderAvailable(provider string) bool {
	switch canonicalProvider(provider) {
	case "codex":
		// ChatGPT-account OAuth at ~/.codex/auth.json (EIGEN_CODEX_AUTH
		// overrides). Available when an access token is present (refresh on
		// 401 is handled at call time).
		p := codexAuthPath()
		if p == "" {
			return false
		}
		if a, err := readCodexAuth(p); err == nil {
			return a.Tokens.AccessToken != ""
		}
		return false
	case "mantle":
		// Bedrock mantle uses the same AWS creds as converse.
		return awsAvailable()
	case "converse":
		return awsAvailable()
	case "anthropic":
		if firstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("EIGEN_ANTHROPIC_API_KEY")) != "" {
			return true
		}
		_, err := claudeOAuthToken(firstNonEmpty(os.Getenv("EIGEN_CLAUDE_CREDENTIALS"), claudeCredentialsPath()))
		return err == nil
	case "grok":
		if firstNonEmpty(os.Getenv("XAI_API_KEY"), os.Getenv("EIGEN_GROK_API_KEY")) != "" {
			return true
		}
		tok, err := grokCLIToken()
		return err == nil && tok != ""
	case "glm":
		return firstNonEmpty(os.Getenv("GLM_API_KEY"), os.Getenv("ZHIPUAI_API_KEY"), os.Getenv("EIGEN_GLM_API_KEY")) != ""
	case "llama":
		return os.Getenv("EIGEN_LLAMA_BASE_URL") != ""
	}
	if p, ok := customProviderByName(provider); ok {
		return customProviderAvailable(p)
	}
	return false
}

func awsAvailable() bool {
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		return true
	}
	_, err := loadAWSCreds(firstNonEmpty(os.Getenv("AWS_PROFILE"), "default"))
	return err == nil
}

// RouteCandidates returns the catalog model IDs the router may choose from:
// every model whose provider is both AVAILABLE (has credentials) and ALLOWED.
// allowed is the user's provider allowlist (canonical names); an empty allowlist
// means "only the current provider" — cross-provider routing is opt-in. current
// is always included so routing can keep the active model.
func RouteCandidates(currentProvider string, allowed []string) []string {
	allow := map[string]bool{}
	for _, p := range allowed {
		allow[canonicalProvider(p)] = true
	}
	cur := canonicalProvider(currentProvider)

	avail := map[string]bool{} // cache per provider
	ok := func(p string) bool {
		cp := canonicalProvider(p)
		if cp != cur && !allow[cp] {
			return false // not the current provider and not allowlisted
		}
		v, seen := avail[cp]
		if !seen {
			v = ProviderAvailable(cp)
			avail[cp] = v
		}
		return v
	}

	var out []string
	for _, m := range Models() {
		if ok(m.Provider) {
			out = append(out, m.ID)
		}
	}
	return out
}

// AllCredentialedModels returns every catalog model on a provider that has
// reachable credentials, ignoring the route allowlist. Used where capability
// matters more than the route policy (e.g. cross-vendor review needs the other
// vendor even if it isn't in the routing allowlist).
func AllCredentialedModels() []string {
	avail := map[string]bool{}
	var out []string
	for _, m := range Models() {
		cp := canonicalProvider(m.Provider)
		v, seen := avail[cp]
		if !seen {
			v = ProviderAvailable(cp)
			avail[cp] = v
		}
		if v {
			out = append(out, m.ID)
		}
	}
	return out
}
