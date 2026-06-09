package dream

import (
	"context"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// consolidatePrompt rewrites an append-only notes file into a smaller, current
// one. The rules encode what append-only cannot do: supersession (recency
// wins), contradiction resolution, dedup — with explicit conservatism so a
// small model can't silently destroy hard-won knowledge.
const consolidatePrompt = `You are consolidating an append-only project memory file for a coding agent. The file is a list of dated bullets ("- YYYY-MM-DD — note"). Over time it has accumulated duplicates, superseded facts, and contradictions. Rewrite it into a smaller, current, trustworthy file.

Rules (strict):
1. RECENCY WINS: when two notes contradict, keep the newer one's claim. Never merge contradicting notes into a blended claim. If unsure which is current, keep BOTH and mark the conflict: "(conflicting notes, verify)".
2. MERGE duplicates and near-duplicates into one bullet, keeping the most recent date and the most specific wording.
3. PRESERVE precision: exact commands with flags, file paths, error strings, identifiers, and short user quotes must survive verbatim. Do not paraphrase concrete wording into abstractions.
4. PRESERVE provenance signals: keep the date prefix of the most recent supporting note on each bullet.
5. USER-stated rules and corrections outrank inferred/derived claims. A note marked as a correction permanently overrides what it corrects — drop the corrected claim entirely.
6. DROP only: exact duplicates, claims explicitly superseded or corrected by newer notes, and transient state that is clearly no longer true (e.g. "X is currently broken" followed by "X works"). When in doubt, KEEP the note.
7. NEVER invent facts, never add advice, never store secrets (keep [REDACTED_SECRET] placeholders as-is).
8. Output ONLY the rewritten file: bullets in the same "- YYYY-MM-DD — note" format, most recent first. No headers, no commentary.`

// maxConsolidateInput bounds the memory text sent to the model.
const maxConsolidateInput = 120_000

// Consolidate asks the provider to rewrite the memory notes per the
// consolidation rules and returns the new content. It refuses results that
// look destructive (empty, or implausibly small relative to the input) so a
// bad model run degrades to a no-op instead of data loss.
func Consolidate(ctx context.Context, p llm.Provider, current string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("dream: nil provider")
	}
	cur := strings.TrimSpace(current)
	if cur == "" {
		return "", fmt.Errorf("memory is empty; nothing to consolidate")
	}
	in := cur
	if len(in) > maxConsolidateInput {
		in = in[len(in)-maxConsolidateInput:] // keep the most recent tail
	}
	resp, err := p.Complete(ctx, llm.Request{
		System:   consolidatePrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: in}},
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(resp.Text)
	// Sanity gates: the result must be a bullet list and must not have
	// destroyed the file. Consolidation shrinks, but losing >90% of the
	// content (or everything) means the model failed — fail closed.
	if out == "" {
		return "", fmt.Errorf("consolidation produced empty output; keeping current memory")
	}
	bullets := 0
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "- ") {
			bullets++
		}
	}
	if bullets == 0 {
		return "", fmt.Errorf("consolidation output is not a bullet list; keeping current memory")
	}
	if len(out) < len(cur)/10 {
		return "", fmt.Errorf("consolidation shrank memory by >90%% (%d → %d bytes); refusing as likely destructive", len(cur), len(out))
	}
	return out + "\n", nil
}
