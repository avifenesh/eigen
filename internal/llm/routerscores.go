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

// routerScores maps catalog model IDs to their routing profile. Tune freely;
// the router's behavior is fully determined by the ordering these induce.
var routerScores = map[string]RouterScore{
	// Frontier (top quality, expensive, slower).
	"openai.gpt-5.5": {Quality: 95, Cost: 80, Speed: 55},
	"openai.gpt-5.4": {Quality: 92, Cost: 78, Speed: 55},
	"openai.gpt-5":   {Quality: 88, Cost: 75, Speed: 58},

	"global.anthropic.claude-fable-5": {Quality: 96, Cost: 90, Speed: 45},
	"us.anthropic.claude-opus-4-8":    {Quality: 95, Cost: 90, Speed: 45},
	"us.anthropic.claude-sonnet-4-6":  {Quality: 88, Cost: 50, Speed: 72},
	"us.anthropic.claude-opus-4-1":    {Quality: 90, Cost: 70, Speed: 50},
	"us.anthropic.claude-3-5-sonnet":  {Quality: 80, Cost: 45, Speed: 72},
	"us.anthropic.claude-haiku-4-5":   {Quality: 70, Cost: 12, Speed: 90},

	"claude-fable-5":             {Quality: 96, Cost: 90, Speed: 45},
	"claude-opus-4-1-20250805":   {Quality: 90, Cost: 70, Speed: 50},
	"claude-sonnet-4-5-20250929": {Quality: 88, Cost: 50, Speed: 72},
	"claude-opus-4-20250514":     {Quality: 89, Cost: 72, Speed: 48},

	// Local (free-ish, modest quality, variable speed).
	"local": {Quality: 50, Cost: 1, Speed: 60},

	// xAI Grok (search-capable; composer/code are fast + cheap).
	"grok-build":             {Quality: 88, Cost: 40, Speed: 70},
	"grok-composer-2.5-fast": {Quality: 78, Cost: 20, Speed: 92},
	"grok-4":                 {Quality: 88, Cost: 50, Speed: 60},
	"grok-code-fast-1":       {Quality: 75, Cost: 15, Speed: 90},

	// Zhipu GLM (search-capable; cheap; turbo is fast).
	"glm-5.1":     {Quality: 85, Cost: 18, Speed: 75},
	"glm-5":       {Quality: 83, Cost: 16, Speed: 75},
	"glm-5-turbo": {Quality: 78, Cost: 12, Speed: 88},
	"glm-4.7":     {Quality: 80, Cost: 14, Speed: 76},
	"glm-4.6":     {Quality: 78, Cost: 12, Speed: 78},
	"glm-4.5":     {Quality: 72, Cost: 10, Speed: 78},
	"glm-4.5-air": {Quality: 65, Cost: 8, Speed: 85},
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
