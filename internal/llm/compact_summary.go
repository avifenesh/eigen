package llm

import (
	"context"
	"fmt"
	"strings"
)

// Compactor turns old conversation into a compact summary. It is a Provider
// call under the hood; kept as an interface so compaction is testable without a
// live model.
type Compactor interface {
	Summarize(ctx context.Context, msgs []Message) (string, error)
}

// summaryPrompt instructs the model to produce a structured, intent-preserving
// summary of the older conversation. Sections follow the schema production
// agents (Claude Code) use: intent, decisions, files, state, next steps.
const summaryPrompt = `You are compacting a long agent conversation so it can continue without losing essential context. Produce a dense, factual summary of the conversation so far, using EXACTLY these sections:

1. Primary Request and Intent: what the user is ultimately trying to achieve (keep their intents, do not paraphrase them away).
2. Key Decisions and Rationale: architectural/design choices made and WHY.
3. Files and Code: files created/modified and their purpose; key symbols/APIs.
4. Current State: what works, what's verified, what's broken.
5. Pending and Next Steps: what remains, in order.
6. Important Constraints and Gotchas: anything that must not be forgotten.

Be specific and information-dense. Do not invent. Omit pleasantries. This summary REPLACES the omitted messages, so capture everything needed to continue correctly.`

// summaryReinjectPrefix frames the compacted summary as a handoff from a prior
// instance, so the model treats it as work already done to build on (and not
// repeat). Borrowed from Codex's compaction prefix.
const summaryReinjectPrefix = "[Context compacted to save space.] Another instance of you began this task and produced the summary below of its progress and findings. The earlier message history has been replaced by this summary. Build on the work already done — do not duplicate it — and continue the task from here."

// shedKeepRounds is how many of the most recent user-led rounds keep their tool
// results verbatim during microcompaction; older results are stubbed.
const shedKeepRounds = 3

// shedKeepToolResults is the in-turn safety valve: when the current/recent
// rounds themselves contain many tool payloads, round-based shedding cannot
// help. Keep only the freshest few result bodies; older ones stay paired but
// become stubs, and the model can re-read if needed.
const shedKeepToolResults = 4

// CompactWith compacts msgs to fit maxTokens. It preserves the most recent
// whole rounds verbatim (cut only at user boundaries, so no tool call is ever
// orphaned) and replaces older history with a single model-generated summary
// merged into the first retained user turn. If c is nil it falls back to the
// deterministic recency-window Compact. Safe to call live or on load.
func CompactWith(ctx context.Context, c Compactor, msgs []Message, maxTokens int) ([]Message, error) {
	if maxTokens <= 0 || EstimateTokens(msgs) <= maxTokens {
		return msgs, nil
	}

	// Microcompaction first: shed old tool-result payloads (keeping the calls)
	// before the expensive model summary. Tool output dominates a coding
	// agent's context, so stubbing results outside the recent window is the
	// cheapest, most-targeted token saver. If that alone brings us under
	// budget, we're done — no summary call needed.
	msgs = ShedToolResults(msgs, shedKeepRounds)
	if EstimateTokens(msgs) <= maxTokens {
		return msgs, nil
	}
	// A single long turn can still be too large after round-based shedding because
	// the whole current round is "recent". Shed older result bodies by count too,
	// before paying for a model summary.
	msgs = ShedOldToolResults(msgs, shedKeepToolResults)
	if EstimateTokens(msgs) <= maxTokens {
		return msgs, nil
	}

	if c == nil {
		return compactFit(msgs, maxTokens), nil
	}

	// Reserve ~45% of the budget for verbatim recent turns; the rest holds the
	// summary plus headroom for the next response.
	recentBudget := maxTokens * 45 / 100

	starts := userStarts(msgs)
	if len(starts) < 2 {
		return compactFit(msgs, maxTokens), nil // nothing meaningful to summarize
	}

	// Earliest recent-round start whose suffix fits recentBudget.
	keepFrom := starts[len(starts)-1]
	for _, s := range starts {
		if EstimateTokens(msgs[s:]) <= recentBudget {
			keepFrom = s
			break
		}
	}
	if keepFrom == 0 {
		keepFrom = starts[len(starts)-1]
	}

	older := msgs[:keepFrom]
	recent := msgs[keepFrom:]

	summary, err := c.Summarize(ctx, older)
	if err != nil {
		return compactFit(msgs, maxTokens), nil // degrade gracefully to v0
	}

	// Preserve the original task verbatim alongside the summary so the model's
	// north star is never paraphrased away by summarization. The framing is
	// third-person ("another instance produced this summary, build on it") —
	// borrowed from Codex's handoff prefix, which reads better to the model
	// than a first-person "I summarized myself" and discourages re-doing work.
	injected := summaryReinjectPrefix + "\n\n"
	if orig := firstUserText(older); orig != "" {
		injected += "Original task: " + orig + "\n\n"
	}
	injected += summary

	// Merge the summary into the first retained user turn rather than prepending
	// a separate synthetic user message: recent[0] is always a user round-start
	// (keepFrom comes from userStarts), so a standalone summary turn would put
	// two adjacent user turns on the wire, which Converse/Anthropic reject or
	// mishandle. This mirrors how the deterministic Compact prepends its note to
	// out[0].Text. Copy first so we never mutate the caller's slice.
	out := make([]Message, len(recent))
	copy(out, recent)
	out[0].Text = injected + "\n\n" + out[0].Text

	// If summary + recent still overflow, fall back to progressively stubbing
	// tool results and then the deterministic whole-round tail — never recurse
	// unbounded (a summary that won't shrink would otherwise loop forever).
	if EstimateTokens(out) > maxTokens {
		return compactFit(out, maxTokens), nil
	}
	return out, nil
}

// compactFit is the last-resort path: progressively shed tool-result payloads
// by count, then fall back to the deterministic whole-round tail. This is what
// makes compaction useful for a single enormous current turn, where there may be
// no older user round to summarize away.
func compactFit(msgs []Message, maxTokens int) []Message {
	if EstimateTokens(msgs) <= maxTokens {
		return msgs
	}
	for _, keep := range []int{2, 1, 0} {
		out := ShedOldToolResults(msgs, keep)
		if EstimateTokens(out) <= maxTokens {
			return out
		}
		msgs = out
	}
	return Compact(msgs, maxTokens)
}

// userStarts returns indices where a user message begins a round.
func userStarts(msgs []Message) []int {
	var s []int
	for i, m := range msgs {
		if m.Role == RoleUser {
			s = append(s, i)
		}
	}
	return s
}

// firstUserText returns the text of the first user message (the original task),
// trimmed to a bound so it never dominates the budget.
func firstUserText(msgs []Message) string {
	for _, m := range msgs {
		if m.Role == RoleUser && strings.TrimSpace(m.Text) != "" {
			t := m.Text
			if len(t) > 1000 {
				t = t[:1000] + "…"
			}
			return t
		}
	}
	return ""
}

// providerCompactor adapts a Provider into a Compactor via a summary call.
type providerCompactor struct{ p Provider }

// NewCompactor builds a Compactor that summarizes using the given provider.
func NewCompactor(p Provider) Compactor { return &providerCompactor{p: p} }

// CompactorChain tries each compactor in order, returning the first successful
// summary. Use it to aim compaction at a cheap small model with the main
// provider as runtime fallback (summarization is a task small models do well —
// per Anthropic's guidance — and the summary call happens at the worst moment,
// when the context is at its largest and most expensive).
func CompactorChain(cs ...Compactor) Compactor {
	var nonNil []Compactor
	for _, c := range cs {
		if c != nil {
			nonNil = append(nonNil, c)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	}
	return chainCompactor(nonNil)
}

type chainCompactor []Compactor

func (cc chainCompactor) Summarize(ctx context.Context, msgs []Message) (string, error) {
	var lastErr error
	for _, c := range cc {
		out, err := c.Summarize(ctx, msgs)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if ctx != nil && ctx.Err() != nil {
			break // canceled: don't burn the fallback on a dead context
		}
	}
	return "", lastErr
}

func (pc *providerCompactor) Summarize(ctx context.Context, msgs []Message) (string, error) {
	// Render the older conversation as plain text for the summarizer, so we
	// don't re-send tool schemas or risk provider-specific replay issues.
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			b.WriteString("USER: " + m.Text + "\n")
		case RoleAssistant:
			if m.Text != "" {
				b.WriteString("ASSISTANT: " + m.Text + "\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString(fmt.Sprintf("TOOL_CALL %s(%s)\n", tc.Name, string(tc.Arguments)))
			}
		case RoleTool:
			r := m.Text
			if len(r) > 2000 {
				r = r[:2000] + "…"
			}
			b.WriteString("TOOL_RESULT: " + r + "\n")
		}
	}
	resp, err := pc.p.Complete(ctx, Request{
		System:   summaryPrompt,
		Messages: []Message{{Role: RoleUser, Text: b.String()}},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.Text) == "" {
		return "", fmt.Errorf("empty summary")
	}
	return resp.Text, nil
}
