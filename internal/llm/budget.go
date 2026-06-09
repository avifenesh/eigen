package llm

// budgetHeadroomPct is the fraction of a model's context window usable as
// conversation budget; the remainder is headroom for the system prompt, tool
// schemas, and the next response.
const budgetHeadroomPct = 85

// ContextBudget returns the conversation token budget for a model:
// min(userMax, catalog window minus headroom). userMax is the optional user
// setting (flag/env/config; <=0 means unset). The model's actual context limit
// always caps the result — a user setting larger than the window cannot push
// the budget past what the model accepts. When the model is unknown to the
// catalog, the providerDefault (similarly headroom-adjusted by the caller's
// choice) is used in place of the window. Returns 0 only if both are unset.
func ContextBudget(userMax int, model string, providerDefault int) int {
	auto := 0
	if window := EffectiveContextWindow(model); window > 0 {
		auto = window * budgetHeadroomPct / 100
	} else if providerDefault > 0 {
		auto = providerDefault
	}
	switch {
	case userMax > 0 && auto > 0:
		if userMax < auto {
			return userMax
		}
		return auto
	case userMax > 0:
		return userMax
	default:
		return auto
	}
}
