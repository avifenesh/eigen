package llm

import (
	"context"
	"errors"
	"strings"
)

// Per-rule fallback CHAIN: a single model role (primary / a subagent type /
// dreamer / judge) is configured as an ORDERED list of models, and the chain
// runs the first one that's reachable, falling through to the next on a
// quota/billing failure (freezing the drained provider for the day) until one
// answers — or, if every link is exhausted, the request genuinely fails ("we're
// down"). This generalizes the binary NewFallback into N links and is what the
// GUI's per-rule chain editor edits.
//
// Friendly shorthands (opus, glm, composer, …) are resolved to catalog ids by
// ResolveChainID so a user's chain reads naturally; unknown/uncredentialed/
// frozen links are skipped, not errored.

// chainAlias maps the user's friendly model shorthands to catalog ids. A name
// already a catalog id (or "provider:id" ref) passes through ResolveChainID
// unchanged; only these shorthands need translating. Kept small + explicit so
// the chain config reads like the user wrote it.
var chainAlias = map[string]string{
	"opus":       "us.anthropic.claude-opus-4-8",
	"opus-4.8":   "us.anthropic.claude-opus-4-8",
	"opus-4.1":   "claude-opus-4-1-20250805",
	"sonnet":     "us.anthropic.claude-sonnet-4-6",
	"sonnet-4.6": "us.anthropic.claude-sonnet-4-6",
	"sonnet-4.5": "claude-sonnet-4-5-20250929",
	"haiku":      "us.anthropic.claude-haiku-4-5-20251001-v1:0",
	"gpt-5.5":    "openai.gpt-5.5", // bedrock/mantle by default (credentialed via Bedrock)
	"gpt-5.4":    "openai.gpt-5.4",
	"gpt-5":      "openai.gpt-5",
	"glm":        "glm-5.2",
	"composer":   "grok-composer-2.5-fast",
	"grok":       "grok-build",
	"grok-code":  "grok-code-fast-1",
}

// ResolveChainID maps a friendly chain entry to a catalog model id. Order:
// exact catalog id → "provider:id" ref → known shorthand alias → "" (unknown;
// the chain skips it rather than erroring, so a chain can name models that
// aren't in this build without breaking).
func ResolveChainID(friendly string) string {
	s := strings.TrimSpace(friendly)
	if s == "" {
		return ""
	}
	if _, ok := Lookup(s); ok {
		return s // already a catalog id
	}
	if tag, _ := ParseRef(s); tag != "" {
		return s // explicit provider:id ref — pass through verbatim
	}
	if id, ok := chainAlias[strings.ToLower(s)]; ok {
		return id
	}
	return "" // unknown — caller skips
}

// chainProvider runs an ordered list of model ids, lazily building each only
// when reached and remembering the live one. On a quota/billing error it freezes
// that provider (process-wide) and advances to the next link; a non-quota error
// surfaces (the model is reachable, the request itself failed). Name/ModelID
// report whichever link is currently live (or the first, before any call).
type chainProvider struct {
	ids   []string // resolved catalog ids, in order (pre-filtered to non-empty)
	links []Provider
}

// NewChain builds a fallback chain from friendly model names. Each is resolved
// (ResolveChainID) and kept in order; unresolvable names are dropped. Returns
// nil when nothing resolves (caller treats as "no provider"). A single resolved
// id collapses to a plain provider (no chain overhead).
func NewChain(friendly ...string) Provider {
	var ids []string
	seen := map[string]bool{}
	for _, f := range friendly {
		id := ResolveChainID(f)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	return &chainProvider{ids: ids, links: make([]Provider, len(ids))}
}

// ChainBeyond reports whether p is a fallback chain that offers any link other
// than modelID — i.e. wrapping a primary whose id is modelID in p would add real
// failover (not just retry the same model). A non-chain provider, or a chain
// whose only link IS modelID, returns false. Used by the primary-model wrap so a
// default opus primary still gets the opus→gpt-5.5→glm→… tail (the chain skips
// its own opus link once the primary is frozen).
func ChainBeyond(p Provider, modelID string) bool {
	cp, ok := p.(*chainProvider)
	if !ok {
		return false
	}
	for _, id := range cp.ids {
		if id != modelID {
			return true
		}
	}
	return false
}

func (c *chainProvider) Name() string {
	return c.firstReachableID()
}

func (c *chainProvider) ModelID() string {
	return c.firstReachableID()
}

// firstReachableID is the headline model: the first link whose provider is
// credentialed + not frozen right now (what a call would use), or the first id
// when none look reachable.
func (c *chainProvider) firstReachableID() string {
	for _, id := range c.ids {
		if modelCredentialed(id) {
			return id
		}
	}
	if len(c.ids) > 0 {
		return c.ids[0]
	}
	return ""
}

// link lazily builds the provider for index i (cached).
func (c *chainProvider) link(i int) (Provider, error) {
	if c.links[i] != nil {
		return c.links[i], nil
	}
	p, err := New("", c.ids[i])
	if err != nil {
		return nil, err
	}
	c.links[i] = p
	return p, nil
}

func (c *chainProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	var lastErr error
	for i, id := range c.ids {
		if ctx.Err() != nil {
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			break
		}
		// Skip a link whose provider is uncredentialed or quota-frozen — don't
		// even build it. (modelCredentialed checks both.)
		if !modelCredentialed(id) {
			continue
		}
		p, err := c.link(i)
		if err != nil {
			lastErr = err
			continue // can't build (transient creds issue) — try the next link
		}
		resp, err := p.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if IsQuotaError(err) {
			// Drained account: freeze its provider for the day so this + every
			// other chain skips it, and advance to the next link.
			FreezeProvider(canonicalProvider(ResolveProvider("", id)))
			continue
		}
		// Non-quota error on a reachable model: the request itself failed (bad
		// input, provider 5xx after retries, context cancel). Surface it rather
		// than masking a real failure by silently trying a weaker model.
		return nil, err
	}
	if lastErr == nil {
		lastErr = errors.New("model chain exhausted: no credentialed model available")
	}
	return nil, lastErr
}
