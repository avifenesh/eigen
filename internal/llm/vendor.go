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

// CrossVendorAdversaries returns candidate adversaries from vendors OTHER than
// the author's, ordered best-first: the canonical cross-reviewer vendor first
// (GPT for a Claude author, Claude for everyone else), then the remaining
// non-author vendors. Within a vendor, stronger models (tier, then rank) come
// first. Used by the planning council to fall back across vendors when the
// primary adversary is unavailable — never back to the author's own vendor
// (which would share its blind spots).
func CrossVendorAdversaries(author string, candidates []string) []string {
	authorVendor := VendorOf(author)
	primary := VendorOpenAI // matches CrossReviewer's default
	if authorVendor == VendorOpenAI {
		primary = VendorAnthropic
	}
	// Group candidates by vendor, excluding the author's vendor and the author.
	byVendor := map[Vendor][]string{}
	for _, id := range candidates {
		v := VendorOf(id)
		if v == authorVendor || id == author {
			continue
		}
		byVendor[v] = append(byVendor[v], id)
	}
	// Strongest-first within each vendor.
	rank := func(id string) int { s := scoreFor(id); return int(s.Tier)*100 + s.Rank }
	for v := range byVendor {
		ids := byVendor[v]
		sortByRankDesc(ids, rank)
		byVendor[v] = ids
	}
	// Vendor order: primary first, then the rest in a stable order.
	order := []Vendor{primary, VendorAnthropic, VendorOpenAI, VendorXAI, VendorZhipu}
	seen := map[Vendor]bool{}
	var out []string
	for _, v := range order {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, byVendor[v]...)
	}
	return out
}

func sortByRankDesc(ids []string, rank func(string) int) {
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if rank(ids[j]) > rank(ids[i]) {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
}
