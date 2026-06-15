package dream

import (
	"context"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// Summarize distills the full curated MEMORY.md into a SMALL summary — the only
// tier injected into prompts (codex's memory_summary.md). It keeps the user
// profile, durable preferences/rules, and the most reusable facts; it drops
// one-off details that live on in MEMORY.md. This is what keeps the prompt lean
// as memory grows.

const summarizePrompt = `You are producing the SMALL injected summary of a coding agent's project memory. You are given the full curated memory (dated bullets and/or structured sections). Output a compact summary that will be injected into EVERY future prompt, so it must be high-signal and short.

Keep:
- The user's durable PREFERENCES and RULES (how they want the agent to work) — these are the highest value.
- The most REUSABLE facts a future session will actually act on: build/test/run commands, key file locations, architecture decisions, recurring gotchas.
- Hard-won FAILURE lessons ("X does not work; do Y").

Drop:
- One-off narration, session-specific detail, anything superseded, anything a future session won't act on. The full detail still lives in MEMORY.md; this is the cheat-sheet.

Rules:
- Preserve exact commands/flags/paths/error strings/short user quotes verbatim — do not abstract them away.
- Keep [REDACTED_SECRET] placeholders; never store secrets.
- Group under short headings when it helps (## Preferences, ## Reusable, ## Gotchas). Bullets, terse.
- Aim for roughly a tenth of the input size or less; never exceed ~3000 words. If the memory is already small, the summary can be nearly the whole thing.
- Output ONLY the summary (markdown bullets/headings). No preamble.`

// maxSummarizeInput bounds the memory text sent to the model.
const maxSummarizeInput = 200_000

// Summarize distills curated memory into the small injected summary. Returns ""
// (with nil error) when there's nothing to summarize.
func Summarize(ctx context.Context, p llm.Provider, memory string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("dream: nil provider")
	}
	mem := strings.TrimSpace(memory)
	if mem == "" {
		return "", nil
	}
	in := mem
	if len(in) > maxSummarizeInput {
		in = in[len(in)-maxSummarizeInput:]
	}
	resp, err := p.Complete(ctx, llm.Request{
		System:   summarizePrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: in}},
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(resp.Text)
	if out == "" {
		return "", fmt.Errorf("summarize produced empty output")
	}
	// Safety: a "summary" that's BIGGER than the input means the model went off
	// the rails — refuse rather than bloat the injected tier.
	if len(out) > len(mem)+200 {
		return "", fmt.Errorf("summary is not smaller than the memory; refusing")
	}
	return out + "\n", nil
}

const globalProfilePrompt = `You are distilling a coding agent's CROSS-PROJECT user profile. You are given per-session memory summaries from MANY different projects (their Preferences and Reusable sections especially). Extract ONLY the durable, project-INDEPENDENT facts about the USER worth remembering everywhere:

- Working style and preferences that recur across projects (how they want the agent to operate, verify, commit, communicate).
- Hard rules / corrections the user states repeatedly.
- Durable cross-project facts about their environment/tooling/accounts that apply everywhere.

Strict rules:
- Project-SPECIFIC facts (this repo's build command, a file path, a one-project gotcha) do NOT belong here — drop them; they live in project memory.
- Only include something if it plausibly recurs across DIFFERENT projects or is a stated user rule.
- Preserve exact wording for rules and short user quotes; never store secrets ([REDACTED_SECRET]).
- Do NOT repeat facts already in the existing global memory; if a new fact supersedes one, say so.
- Minimum-signal gate: if nothing clears the cross-project bar, output nothing.

Output ONLY a bullet list ("- " lines), at most 6, most reusable first. No headings, no commentary.`

// DistillGlobal extracts cross-project user-profile facts (working style,
// recurring preferences, global rules) from per-project session summaries,
// skipping anything already in the existing global memory. Returns new bullets.
func DistillGlobal(ctx context.Context, p llm.Provider, summaries []string, existingGlobal string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("dream: nil provider")
	}
	if len(summaries) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("Existing global memory:\n")
	if strings.TrimSpace(existingGlobal) == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(existingGlobal + "\n")
	}
	b.WriteString("\nPer-session summaries from many projects:\n")
	for i, s := range summaries {
		fmt.Fprintf(&b, "--- %d ---\n%s\n", i+1, s)
	}
	content := b.String()
	if len(content) > maxSummarizeInput {
		content = content[len(content)-maxSummarizeInput:]
	}
	resp, err := p.Complete(ctx, llm.Request{
		System:   globalProfilePrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: content}},
	})
	if err != nil {
		return nil, err
	}
	return dedupe(parseBullets(resp.Text), existingGlobal), nil
}
