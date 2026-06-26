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

const stationPrompt = `You are eigen's WORKING-STATION reflection. eigen is the user's personal working station — not just a coding tool. You are given a point-in-time digest of the user's working life: upcoming calendar events, unread mail senders/subjects, project git state (uncommitted/unpushed/behind), scheduled jobs (crons), and machine health.

Produce a SHORT list of durable, ACTIONABLE awareness notes for the user's global memory — things a future eigen session should know to be a better working-station assistant. Examples of the KIND of thing (only if the digest supports it):
- A recurring commitment or deadline pattern ("standup daily 9am", "rent due monthly").
- A project that is consistently drifting (many unpushed commits / behind upstream) and likely needs attention.
- A standing scheduled job worth knowing about.
- A persistent machine-health concern (disk nearly full).

Strict rules:
- These are about the USER's working life, NOT a single coding session. Skip anything ephemeral (one meeting today, one unread email) unless it reveals a durable pattern or an obligation worth remembering.
- Minimum-signal gate: if the digest is routine and nothing is worth remembering beyond today, output NOTHING.
- Never store secrets, email bodies, or private content; keep it to high-level awareness ([REDACTED_SECRET] for anything sensitive).
- Do NOT repeat facts already in the existing global memory.

Output ONLY a bullet list ("- " lines), at most 5, most useful first. No headings, no commentary.`

// DistillStation reflects a working-station digest (calendar, mail, projects,
// crons, health) into durable life/project awareness notes for global memory.
// Returns new bullets (deduped against existing). Empty digest → nil.
func DistillStation(ctx context.Context, p llm.Provider, digest, existingGlobal string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("dream: nil provider")
	}
	if strings.TrimSpace(digest) == "" {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("Existing global memory:\n")
	if strings.TrimSpace(existingGlobal) == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(existingGlobal + "\n")
	}
	b.WriteString("\nWorking-station digest (now):\n")
	b.WriteString(digest)
	content := b.String()
	if len(content) > maxSummarizeInput {
		content = content[len(content)-maxSummarizeInput:]
	}
	resp, err := p.Complete(ctx, llm.Request{
		System:   stationPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: content}},
	})
	if err != nil {
		return nil, err
	}
	return dedupe(parseBullets(resp.Text), existingGlobal), nil
}

const demotePrompt = `You are auditing a coding agent's GLOBAL (cross-project) memory for notes that DON'T belong there because they are really specific to ONE project.

Global memory is for facts that apply EVERYWHERE: the user's working style, recurring preferences, hard rules, environment facts true across projects. A note that names a single repo's build command, a one-project file path, a gotcha that only bites in one codebase, etc. was misfiled and should move DOWN into that project's memory.

You are given the global memory and a list of known project names. For each global note that is actually project-specific AND you can confidently name which project it belongs to (from the project list), output one line:

PROJECT_NAME<TAB>the note text verbatim

Strict rules:
- Only flag a note when you are CONFIDENT which single project it belongs to. If a note is genuinely cross-project, or you can't tell which project, LEAVE IT (output nothing for it).
- Use a project name EXACTLY as it appears in the provided list. Never invent a project.
- Copy the note text verbatim (preserve commands/paths/quotes; keep [REDACTED_SECRET]).
- Be conservative: demoting a truly-global rule is worse than leaving a misfiled note. When unsure, output nothing.
- Output ONLY "PROJECT<TAB>note" lines, at most 6. No headings, no commentary. If nothing should move, output nothing.`

// Demotion is one proposed move of a misfiled global note down into a project.
type Demotion struct {
	Project string // project name as given in the candidate list
	Note    string // the note text to move
}

// ProposeDemotions asks the model which global notes are actually specific to a
// single named project and should move down into it. projectNames is the set of
// known project names (the candidates a note may be assigned to). Returns only
// confident, well-formed proposals whose project is in the candidate set; an
// empty/blank global memory or no candidates yields nil. Conservative by design
// — the prompt is told that leaving a note is safer than a wrong demotion.
func ProposeDemotions(ctx context.Context, p llm.Provider, globalMemory string, projectNames []string) ([]Demotion, error) {
	if p == nil {
		return nil, fmt.Errorf("dream: nil provider")
	}
	gm := strings.TrimSpace(globalMemory)
	if gm == "" || len(projectNames) == 0 {
		return nil, nil
	}
	valid := make(map[string]bool, len(projectNames))
	var b strings.Builder
	b.WriteString("Known projects:\n")
	for _, n := range projectNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		valid[n] = true
		fmt.Fprintf(&b, "- %s\n", n)
	}
	b.WriteString("\nGlobal memory:\n")
	b.WriteString(gm)
	content := b.String()
	if len(content) > maxSummarizeInput {
		content = content[len(content)-maxSummarizeInput:]
	}
	resp, err := p.Complete(ctx, llm.Request{
		System:   demotePrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: content}},
	})
	if err != nil {
		return nil, err
	}
	var out []Demotion
	for _, ln := range strings.Split(resp.Text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		// Accept TAB or the first run of spaces as the separator (models vary).
		proj, note := splitDemotion(ln)
		proj, note = strings.TrimSpace(proj), strings.TrimSpace(note)
		if proj == "" || note == "" || !valid[proj] {
			continue // unknown/invented project or malformed line — drop
		}
		out = append(out, Demotion{Project: proj, Note: note})
		if len(out) >= 6 {
			break
		}
	}
	return out, nil
}

// splitDemotion splits a "PROJECT<sep>note" line on the first TAB, falling back
// to the first whitespace run when the model didn't emit a literal tab.
func splitDemotion(ln string) (proj, note string) {
	if i := strings.IndexByte(ln, '\t'); i >= 0 {
		return ln[:i], ln[i+1:]
	}
	if i := strings.IndexFunc(ln, func(r rune) bool { return r == ' ' }); i >= 0 {
		return ln[:i], ln[i+1:]
	}
	return ln, ""
}
