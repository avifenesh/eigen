package llm

import (
	"context"
	"fmt"
)

// reviewPrompt asks for a focused, critical review from the reviewing model.
const reviewPrompt = `You are reviewing work produced by a DIFFERENT AI model (%s). Be a rigorous, independent critic — your job is to catch what the author missed, not to praise.

%sARTIFACT TO REVIEW:
%s

Give a concise, concrete review:
- Correctness issues, bugs, or wrong assumptions (most important).
- Risks, edge cases, and missing considerations.
- Specific, actionable improvements.
If it is genuinely solid, say so briefly and note any residual risk. Do not rewrite it wholesale; critique it.`

// ReviewArtifact asks the reviewer model to critique an artifact authored by
// another model. author/reviewer are model ids (for the prompt framing). focus
// is optional. Independence is structural: the caller picks reviewer from the
// other vendor (see CrossReviewer).
func ReviewArtifact(ctx context.Context, reviewer Provider, reviewerID, authorID, artifact, focus string) (string, error) {
	if reviewer == nil {
		return "", fmt.Errorf("no reviewer model")
	}
	focusLine := ""
	if focus != "" {
		focusLine = "REVIEW FOCUS: " + focus + "\n\n"
	}
	resp, err := reviewer.Complete(ctx, Request{
		System: "You are a senior engineer giving a sharp, honest code/plan review. Independent and critical; concrete over vague.",
		Messages: []Message{{
			Role: RoleUser,
			Text: fmt.Sprintf(reviewPrompt, authorVendorLabel(authorID), focusLine, artifact),
		}},
	})
	if err != nil {
		return "", fmt.Errorf("review: %w", err)
	}
	return resp.Text, nil
}

// authorVendorLabel describes the author for the reviewer's framing.
func authorVendorLabel(authorID string) string {
	switch VendorOf(authorID) {
	case VendorAnthropic:
		return "Anthropic Claude"
	case VendorOpenAI:
		return "OpenAI GPT"
	case VendorXAI:
		return "xAI Grok"
	case VendorZhipu:
		return "Zhipu GLM"
	}
	return "another model"
}
