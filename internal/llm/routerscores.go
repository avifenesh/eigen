package llm

// Auto-router scoring. Each model carries a best-effort relative score so the
// router can pick, per task, the cheapest model that is still good enough —
// and, at equal cost, the stronger one; at equal cost and quality, the faster
// one. These numbers are approximations meant to be TUNED: they encode rough
// real-world positioning (frontier vs cheap, fast vs slow), not exact prices.
// A model absent from this table is treated as unknown (mid quality, unknown
// cost) and is only chosen when nothing scored fits.

// RouterScore is a model's relative routing profile.
type RouterScore struct {
	Quality int // 0–100 capability strength (higher = does harder tasks well)
	Cost    int // 1–100 relative cost per token (LOWER = cheaper)
	Speed   int // 0–100 relative throughput (higher = faster)
}

// routerScores maps catalog model IDs to their routing profile. Cost is
// calibrated from real published pricing (blended input+output $/Mtok scaled so
// the priciest, legacy Opus, ≈ 100; source: OpenRouter models API, fetched
// 2026-06). Quality/Speed are relative estimates. Tune freely; the router's
// behavior is fully determined by the ordering these induce.
var routerScores = map[string]RouterScore{
	// OpenAI GPT (mantle). Real $/Mtok: 5.5=5/30, 5.4=2.5/15.
	"openai.gpt-5.5": {Quality: 95, Cost: 39, Speed: 55},
	"openai.gpt-5.4": {Quality: 92, Cost: 19, Speed: 58},
	"openai.gpt-5":   {Quality: 88, Cost: 18, Speed: 60},

	// Anthropic (Bedrock). Real $/Mtok: fable=10/50, opus4.8=5/25,
	// sonnet=3/15, opus4.1=15/75 (legacy), haiku=1/5.
	"global.anthropic.claude-fable-5": {Quality: 96, Cost: 67, Speed: 45},
	"us.anthropic.claude-opus-4-8":    {Quality: 95, Cost: 33, Speed: 48},
	"us.anthropic.claude-sonnet-4-6":  {Quality: 88, Cost: 20, Speed: 74},
	"us.anthropic.claude-opus-4-1":    {Quality: 90, Cost: 100, Speed: 45},
	"us.anthropic.claude-3-5-sonnet":  {Quality: 80, Cost: 20, Speed: 74},
	"us.anthropic.claude-haiku-4-5":   {Quality: 70, Cost: 7, Speed: 92},

	// Anthropic (native API) — same pricing as the Bedrock twins.
	"claude-fable-5":             {Quality: 96, Cost: 67, Speed: 45},
	"claude-opus-4-1-20250805":   {Quality: 90, Cost: 100, Speed: 45},
	"claude-sonnet-4-5-20250929": {Quality: 88, Cost: 20, Speed: 74},
	"claude-opus-4-20250514":     {Quality: 89, Cost: 100, Speed: 45},

	// Local (free; modest quality).
	"local": {Quality: 50, Cost: 1, Speed: 60},

	// xAI Grok — cheap + fast. Real $/Mtok: grok-build=1/2, grok-4=1.25/2.5.
	"grok-build":             {Quality: 88, Cost: 3, Speed: 78},
	"grok-composer-2.5-fast": {Quality: 78, Cost: 3, Speed: 94},
	"grok-4":                 {Quality: 88, Cost: 4, Speed: 62},
	"grok-code-fast-1":       {Quality: 75, Cost: 2, Speed: 92},

	// Zhipu GLM — cheap. Real $/Mtok: glm-5.1=0.98/3.08, glm-5-turbo=1.2/4.
	"glm-5.1":     {Quality: 85, Cost: 5, Speed: 76},
	"glm-5":       {Quality: 83, Cost: 5, Speed: 76},
	"glm-5-turbo": {Quality: 78, Cost: 6, Speed: 90},
	"glm-4.7":     {Quality: 80, Cost: 4, Speed: 78},
	"glm-4.6":     {Quality: 78, Cost: 4, Speed: 80},
	"glm-4.5":     {Quality: 72, Cost: 3, Speed: 80},
	"glm-4.5-air": {Quality: 65, Cost: 2, Speed: 88},
}

// scoreFor returns a model's router score, or a neutral unknown profile.
func scoreFor(id string) RouterScore {
	if s, ok := routerScores[id]; ok {
		return s
	}
	// Unknown: mid quality, unknown (high) cost so it loses to scored peers,
	// mid speed. It only wins when nothing scored is capable.
	return RouterScore{Quality: 60, Cost: 100, Speed: 50}
}
