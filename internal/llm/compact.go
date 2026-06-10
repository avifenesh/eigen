package llm

import "fmt"

// EstimateTokens is a rough token count (~4 chars/token) for budget decisions.
func EstimateTokens(msgs []Message) int {
	chars := 0
	tokens := 0
	for _, m := range msgs {
		chars += len(m.Text) + len(m.Reasoning)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Name) + len(tc.Arguments)
		}
		chars += 16 // per-message overhead
		// Images are billed by area, not bytes; without decoding dimensions we
		// use a flat ~1.2k-token estimate per image (a typical screenshot is
		// ~1–1.6k), enough to keep the budget honest.
		tokens += len(m.Images) * 1200
	}
	return chars/4 + tokens
}

// Compact trims a conversation to fit maxTokens by keeping the most recent
// whole rounds (a round begins at a user message), so the result is a suffix of
// complete rounds — valid for every provider, including Converse's strict
// alternation and tool-call/result pairing. Dropped history is noted on the
// first retained user message. Deterministic; the same function is used live
// (as a session grows) and on load (resuming a large transcript).
func Compact(msgs []Message, maxTokens int) []Message {
	if maxTokens <= 0 || EstimateTokens(msgs) <= maxTokens {
		return msgs
	}

	var starts []int
	for i, m := range msgs {
		if m.Role == RoleUser {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return msgs // no user boundary to cut on
	}

	// Earliest round-start whose suffix fits the budget (largest fitting tail).
	keptStart := starts[len(starts)-1]
	for _, s := range starts {
		if EstimateTokens(msgs[s:]) <= maxTokens {
			keptStart = s
			break
		}
	}
	if keptStart == 0 {
		return msgs
	}

	out := make([]Message, len(msgs)-keptStart)
	copy(out, msgs[keptStart:])
	out[0].Text = fmt.Sprintf("[Earlier conversation compacted: %d messages omitted to fit context.]\n\n", keptStart) + out[0].Text
	return out
}
