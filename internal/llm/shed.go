package llm

// toolResultStub replaces an elided tool result's text. The tool CALL is kept
// intact so call/result pairing stays valid for strict providers (Converse);
// only the (usually large) result payload is dropped.
const toolResultStub = "[earlier tool result elided to save context]"

// ShedToolResults is the cheapest, lossiest-in-the-right-place form of
// compaction: it walks the history oldest→newest and replaces tool *results*
// that fall outside the most recent keepRounds rounds with a short stub,
// leaving the tool *calls* and all assistant/user text untouched. Tool output
// dominates a coding agent's context, and an old file dump or search result is
// rarely needed once the model has acted on it, so this frees large amounts of
// budget without a model call and without breaking call/result pairing.
//
// A "round" begins at a user message. Results in the last keepRounds rounds are
// preserved verbatim. Already-stubbed results and results marked ToolError are
// left as-is (errors are short and often still relevant). The returned slice is
// a copy; the input is not mutated. This is provider-agnostic and the portable
// equivalent of Anthropic's server-side clear_tool_uses strategy.
func ShedToolResults(msgs []Message, keepRounds int) []Message {
	if keepRounds < 0 {
		keepRounds = 0
	}

	// Find the index where the preserved recent window begins: the start of the
	// keepRounds-th user-led round counting from the end. keepRounds==0 means
	// stub every result (keepFrom stays at len(msgs)).
	starts := userStarts(msgs)
	keepFrom := len(msgs)
	if keepRounds > 0 {
		if len(starts) > keepRounds {
			keepFrom = starts[len(starts)-keepRounds]
		} else if len(starts) > 0 {
			keepFrom = starts[0] // fewer rounds than keepRounds: keep them all
		}
	}

	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := 0; i < keepFrom; i++ {
		m := &out[i]
		if m.Role != RoleTool || m.ToolError {
			continue
		}
		if m.Text == toolResultStub || m.Text == "" {
			continue
		}
		m.Text = toolResultStub
	}
	return out
}
