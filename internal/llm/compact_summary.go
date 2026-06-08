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

// CompactWith compacts msgs to fit maxTokens. It preserves the most recent
// whole rounds verbatim (cut only at user boundaries, so no tool call is ever
// orphaned) and replaces older history with a single model-generated summary
// injected as a synthetic user message. If c is nil it falls back to the
// deterministic recency-window Compact. Safe to call live or on load.
func CompactWith(ctx context.Context, c Compactor, msgs []Message, maxTokens int) ([]Message, error) {
	if maxTokens <= 0 || EstimateTokens(msgs) <= maxTokens {
		return msgs, nil
	}
	if c == nil {
		return Compact(msgs, maxTokens), nil
	}

	// Reserve ~45% of the budget for verbatim recent turns; the rest holds the
	// summary plus headroom for the next response.
	recentBudget := maxTokens * 45 / 100

	starts := userStarts(msgs)
	if len(starts) < 2 {
		return Compact(msgs, maxTokens), nil // nothing meaningful to summarize
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
		return Compact(msgs, maxTokens), nil // degrade gracefully to v0
	}

	out := make([]Message, 0, len(recent)+1)
	out = append(out, Message{
		Role: RoleUser,
		Text: "[Context compacted. Summary of the earlier conversation follows; continue from it.]\n\n" + summary,
	})
	out = append(out, recent...)

	// Recursive case: if summary + recent still overflow, recurse with a
	// smaller recent window.
	if EstimateTokens(out) > maxTokens && len(recent) > 2 {
		return CompactWith(ctx, c, out, maxTokens)
	}
	return out, nil
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

// providerCompactor adapts a Provider into a Compactor via a summary call.
type providerCompactor struct{ p Provider }

// NewCompactor builds a Compactor that summarizes using the given provider.
func NewCompactor(p Provider) Compactor { return &providerCompactor{p: p} }

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
