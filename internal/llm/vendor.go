package llm

import "strings"

// Vendor identifies a model's family for cross-vendor review: same-family
// models share training lineage and blind spots, so review/judging always
// crosses vendors (GPT reviews Claude, Claude reviews GPT).
type Vendor int

const (
	VendorUnknown Vendor = iota
	VendorAnthropic
	VendorOpenAI
	VendorXAI
	VendorZhipu
)

// VendorOf classifies a model id by its family.
func VendorOf(model string) Vendor {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude") || strings.Contains(m, "anthropic"):
		return VendorAnthropic
	case strings.Contains(m, "gpt") || strings.HasPrefix(m, "openai"):
		return VendorOpenAI
	case strings.Contains(m, "grok"):
		return VendorXAI
	case strings.Contains(m, "glm"):
		return VendorZhipu
	}
	return VendorUnknown
}

// CrossReviewer picks the reviewer model for an author model, per the user's
// rule: ALWAYS the other side of the Anthropic/GPT pair — GPT reviews Claude
// (strict correctness), Claude reviews GPT (design/clarity). Other vendors
// (grok/glm — the simple tier) get the strict GPT reviewer. The reviewer is
// chosen from candidates (credentialed models); returns "" when no
// cross-vendor reviewer is available.
func CrossReviewer(author string, candidates []string) string {
	want := VendorOpenAI // default reviewer: strict GPT (also for grok/glm/unknown)
	if VendorOf(author) == VendorOpenAI {
		want = VendorAnthropic // Claude reviews GPT
	}
	best := ""
	bestRank := -1
	for _, id := range candidates {
		if VendorOf(id) != want {
			continue
		}
		s := scoreFor(id)
		// Prefer the strongest reviewer available: tier first, then rank.
		r := int(s.Tier)*100 + s.Rank
		if r > bestRank {
			best, bestRank = id, r
		}
	}
	return best
}
