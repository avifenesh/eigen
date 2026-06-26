package llm

// maxRetainedToolImages is how many of the most recent tool-result images
// (screenshots) survive in history. Images are far heavier than text — an old
// screenshot is rarely needed once the model has acted on it — so we keep only
// the freshest few and drop the rest, replacing the message text with a note.
const maxRetainedToolImages = 2

// imagePrunedStub marks a tool result whose screenshot was dropped to bound
// context/memory; the tool call and a note are kept.
const imagePrunedStub = "[earlier screenshot dropped from history to save context]"

// ShedToolImages walks history newest→oldest and strips Images from all but
// the most recent maxRetainedToolImages image-bearing tool results, so a long
// browser/computer-use session can't accumulate screenshot bytes without
// bound. A pruned result keeps its tool call (pairing stays valid) and gets a
// short note appended. Returns a copy; the input is not mutated.
func ShedToolImages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	kept := 0
	for i := len(out) - 1; i >= 0; i-- {
		m := &out[i]
		if m.Role != RoleTool || len(m.Images) == 0 {
			continue
		}
		if kept < maxRetainedToolImages {
			kept++
			continue
		}
		m.Images = nil
		if m.Text == "" || m.Text == toolResultStub {
			m.Text = imagePrunedStub
		} else {
			m.Text += "\n" + imagePrunedStub
		}
	}
	return out
}

// ShedOldToolResults is the same microcompaction primitive, but bounded by a
// COUNT of recent tool results instead of user-led rounds. It is used when a
// single long turn accumulates many large tool outputs: round-based shedding
// preserves the whole current round, so it cannot prevent the next model call
// from overflowing. Keeping only the freshest N successful tool payloads is
// safe for strict providers because the tool-result message remains paired with
// its tool call; the model can re-read anything older if it truly needs it.
//
// keepResults==0 stubs every non-error result. Already-stubbed results and
// errored results are left as-is. The returned slice is a copy; the input is not
// mutated.
func ShedOldToolResults(msgs []Message, keepResults int) []Message {
	if keepResults < 0 {
		keepResults = 0
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	kept := 0
	for i := len(out) - 1; i >= 0; i-- {
		m := &out[i]
		if m.Role != RoleTool || m.ToolError {
			continue
		}
		if m.Text == "" || isStub(m.Text) {
			continue
		}
		if kept < keepResults {
			kept++
			continue
		}
		m.Text = toolResultStub
	}
	return out
}

// toolResultStub replaces an elided tool result's text. The tool CALL is kept
// intact so call/result pairing stays valid for strict providers (Converse);
// only the (usually large) result payload is dropped.
const toolResultStub = "[earlier tool result elided to save context]"

// duplicateResultStub replaces an older tool result whose exact output appears
// again later in the conversation (e.g. the same file re-read unchanged). The
// newest occurrence is kept; older copies are pure token waste.
const duplicateResultStub = "[identical output re-returned by a later call; see the most recent occurrence]"

// dedupeMinChars is the minimum result size worth deduplicating. Stubbing an
// old message invalidates the prompt-cache prefix from that point, so tiny
// duplicates are not worth the cache hit.
const dedupeMinChars = 2000

// DedupeToolResults stubs older tool results whose (tool, output) exactly
// matches the result at index last in msgs, keeping the newest occurrence
// verbatim. It mutates msgs in place and returns how many older copies were
// stubbed. Call it right after appending a tool result, with last = its index.
// Results smaller than dedupeMinChars, error results, and different tools are
// left alone.
func DedupeToolResults(msgs []Message, last int) int {
	if last < 0 || last >= len(msgs) {
		return 0
	}
	cur := msgs[last]
	if cur.Role != RoleTool || cur.ToolError || len(cur.Text) < dedupeMinChars {
		return 0
	}
	n := 0
	for i := 0; i < last; i++ {
		m := &msgs[i]
		if m.Role != RoleTool || m.ToolError {
			continue
		}
		if m.ToolName == cur.ToolName && m.Text == cur.Text {
			m.Text = duplicateResultStub
			n++
		}
	}
	return n
}

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
		if m.Text == "" || isStub(m.Text) {
			continue
		}
		m.Text = toolResultStub
	}
	return out
}

// isStub reports whether text is already one of the compaction stubs, so a
// later shed pass won't overwrite a specific note (e.g. the dropped-screenshot
// note or the dedupe pointer) with the generic tool-result stub.
func isStub(text string) bool {
	switch text {
	case toolResultStub, imagePrunedStub, duplicateResultStub:
		return true
	}
	return false
}
