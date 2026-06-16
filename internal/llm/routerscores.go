package llm

// Auto-router scoring — a QUALITY-TIER ladder, not a price search. The user's
// accounts are flat pre-paid, so per-token dollars are irrelevant; what matters
// is (a) quality is paramount — harder work gets a stronger model, never the
// reverse — and (b) sparing the employer-paid Bedrock account when a model on
// the user's own accounts is in the same tier.
//
// The cheap, fast models (grok / composer / glm) are trusted ONLY for simple
// work, regardless of their nominal benchmark scores. Tiers, per the user:
//
//	Tier 1 (simple)     grok, composer, glm (≤5.1), haiku, local
//	Tier 2 (simple-med) sonnet, glm-5.2
//	Tier 3 (med)        opus, gpt-5.x — all hard/frontier work lands here

// Tier is a model's quality class (1 simple … 4 frontier). Higher = stronger.
type Tier int

const (
	TierSimple    Tier = 1 // grok/composer/glm/haiku/local — simple tasks
	TierSimpleMed Tier = 2 // sonnet
	TierMed       Tier = 3 // opus
	TierFrontier  Tier = 4 // reserved: unknown-model fallback target
)

// RouterScore is a model's routing profile: its quality tier, a within-tier
// quality Rank, a relative speed, and two affinity flags that order the tier-3
// pool: Strict marks the more-correct/disciplined model preferred for general
// work (gpt-5.5 over opus); Design marks the better frontend/design model
// (opus over gpt-5.5) preferred when the task is frontend.
//
// Rank orders quality WITHIN a tier (higher = better, e.g. gpt-5.5 over
// gpt-5.4). Bedrock-avoidance only ever breaks TRUE ties — equal tier,
// affinity, and rank (the same-model-on-two-accounts case) — never at the cost
// of quality.
type RouterScore struct {
	Tier   Tier
	Rank   int  // within-tier quality (higher = better; 0 default)
	Speed  int  // 0–100 relative throughput (higher = faster)
	Strict bool // more strict/correct — wins general within its tier
	Design bool // better at frontend/design — wins frontend within its tier
}

// routerScores maps catalog model IDs to their tier + speed. Tiers reflect the
// user's TRUST (grok/glm are "simple only" even at high benchmark scores), not
// leaderboard numbers. Tune freely.
var routerScores = map[string]RouterScore{
	// Tier 3 — med (opus + GPT family). This is the tier for hard work.
	// gpt-5.5 (codex) is the default main agent now (xhigh effort + fast
	// service tier) and ranks highest here (Rank 5); opus is the strong
	// general fallback (Rank 4, and the design-affinity pick). The codex and
	// mantle gpt-5.5 ids are distinct catalog entries but score the same.
	"gpt-5.5":                      {Tier: TierMed, Rank: 5, Speed: 50, Strict: true},
	"openai.gpt-5.5":               {Tier: TierMed, Rank: 5, Speed: 50, Strict: true},
	"gpt-5.4":                      {Tier: TierMed, Rank: 2, Speed: 58},
	"openai.gpt-5.4":               {Tier: TierMed, Rank: 2, Speed: 58},
	"openai.gpt-5":                 {Tier: TierMed, Rank: 1, Speed: 60},
	"us.anthropic.claude-opus-4-8": {Tier: TierMed, Rank: 4, Speed: 48, Design: true},

	// Tier 2 — simple-med (sonnet + glm-5.2). glm-5.2 is the user's flagship GLM
	// and ranks ABOVE sonnet-4-6 here (Rank 4 > 3) — it wins a simple-med tie.
	"glm-5.2":                        {Tier: TierSimpleMed, Rank: 4, Speed: 76},
	"us.anthropic.claude-sonnet-4-6": {Tier: TierSimpleMed, Rank: 3, Speed: 74},
	"us.anthropic.claude-3-5-sonnet": {Tier: TierSimpleMed, Rank: 1, Speed: 74},

	// Tier 1 — simple (cheap/fast; grok/composer/glm/haiku/local).
	"us.anthropic.claude-haiku-4-5-20251001-v1:0": {Tier: TierSimple, Speed: 92},
	"local":                  {Tier: TierSimple, Speed: 60},
	"grok-build":             {Tier: TierSimple, Speed: 78},
	"grok-composer-2.5-fast": {Tier: TierSimple, Speed: 94},
	"grok-4":                 {Tier: TierSimple, Speed: 62},
	"grok-code-fast-1":       {Tier: TierSimple, Speed: 92},
	"glm-5.1":                {Tier: TierSimple, Speed: 76},
	"glm-5":                  {Tier: TierSimple, Speed: 76},
	"glm-5-turbo":            {Tier: TierSimple, Speed: 90},
	"glm-4.7":                {Tier: TierSimple, Speed: 78},
	"glm-4.6":                {Tier: TierSimple, Speed: 80},
	"glm-4.5":                {Tier: TierSimple, Speed: 80},
	"glm-4.5-air":            {Tier: TierSimple, Speed: 88},
}

// scoreFor returns a model's router score, or a neutral unknown profile. An
// unknown model is treated as frontier (tier 4) so the router never silently
// downgrades a model it doesn't recognize.
func scoreFor(id string) RouterScore {
	if s, ok := routerScores[id]; ok {
		return s
	}
	return RouterScore{Tier: TierFrontier, Speed: 50}
}
