package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Adversarial cross-vendor planning ("GPT×Claude planning"): for a hard task,
// one model AUTHORS a plan and a model from the OTHER vendor adversarially
// critiques it; the author revises; repeat until the adversary approves or the
// round budget runs out. Two vendors rarely share the same blind spot, so the
// converged plan is materially harder than either model's solo plan.
//
// Built on the same vendor logic as cross-vendor review (VendorOf / CrossReviewer
// / authorVendorLabel) — this is the iterative, plan-producing sibling of the
// one-shot review tool.

// CouncilConfig is the setup for a planning council.
type CouncilConfig struct {
	Author      Provider // drafts + revises the plan
	AuthorID    string
	Adversary   Provider // critiques the plan (ideally the other vendor)
	AdversaryID string
	// Fallbacks are alternate adversaries (other vendors), tried in order if the
	// primary Adversary fails to produce its FIRST critique. Each is a
	// provider+id pair.
	Fallbacks []AdversaryOption
	MaxRounds int // critique/revise rounds (default 3)
	// CallTimeout bounds each ADVERSARY model call (not the author's). A hanging
	// adversary (e.g. a stalled endpoint) is treated as a failure so the council
	// falls through to the next vendor instead of blocking forever. The author's
	// calls are unbounded — it's the main model doing the real work. 0 = default.
	CallTimeout time.Duration
}

// AdversaryOption is one candidate adversary (provider + model id).
type AdversaryOption struct {
	Provider Provider
	ID       string
}

// CouncilTurn is one contribution to the council transcript.
type CouncilTurn struct {
	Role  string // "author" or "adversary"
	Model string
	Round int
	Text  string
}

// CouncilResult is the outcome of a planning council.
type CouncilResult struct {
	Plan            string        // the final (hardened) plan
	Rounds          int           // critique/revise rounds actually run
	Converged       bool          // adversary approved (vs hit the round cap)
	CrossVendor     bool          // author and adversary were different vendors
	AdversaryFailed bool          // every adversary errored before a first critique
	AdversaryID     string        // the adversary that actually critiqued
	Dissent         string        // adversary's remaining objections if not converged
	Transcript      []CouncilTurn // full debate (author draft, critiques, revisions)
}

const councilAuthorDraft = `You are a senior engineer (%s). Produce a CONCRETE, step-by-step implementation plan for the task below. Be specific and actionable: the files/functions to touch, the order of work, how each part is tested/verified, the risks and how to de-risk them, and what is explicitly out of scope. Prefer the simplest design that fully solves the task. Do not write the final code — produce the PLAN.

TASK:
%s
%s`

const councilCritique = `You are a senior engineer (%s) doing an ADVERSARIAL review of another engineer's (%s) implementation plan. Your job is to find every REAL flaw before any code is written: wrong or unstated assumptions, missing steps, ignored edge cases, security/correctness/concurrency risks, over-engineering, and simpler alternatives. Be specific and tough but fair — cite the part of the plan you mean. Do not rewrite the plan; critique it.

End your reply with EXACTLY one line:
VERDICT: APPROVE   (if the plan is sound and ready to execute as-is)
VERDICT: REVISE    (if it needs changes — your critique says what)

PLAN UNDER REVIEW:
%s`

const councilRevise = `Below is an adversarial critique of YOUR plan from a reviewer at another vendor (%s). Revise your plan to address every VALID point. If you genuinely disagree with a point, keep your approach but briefly justify it in a "Defended:" note. Output the COMPLETE revised plan (not a diff), still concrete and step-by-step.

CRITIQUE:
%s

YOUR PREVIOUS PLAN:
%s`

// Council runs the adversarial planning loop and returns the hardened plan.
// taskContext is optional extra grounding (codebase facts, constraints); pass ""
// if none. A nil Adversary or one of the same vendor still works (degraded:
// CrossVendor=false) — a fresh adversarial pass is useful even same-vendor.
func Council(ctx context.Context, cfg CouncilConfig, task, taskContext string) (*CouncilResult, error) {
	if cfg.Author == nil {
		return nil, fmt.Errorf("council: no author model")
	}
	if strings.TrimSpace(task) == "" {
		return nil, fmt.Errorf("council: empty task")
	}
	rounds := cfg.MaxRounds
	if rounds <= 0 {
		rounds = 3
	}
	timeout := cfg.CallTimeout
	if timeout <= 0 {
		// Real adversary critiques finish in ~5-30s; 45s gives headroom while a
		// genuine hang (e.g. a stalled endpoint) fails fast and falls through to
		// the next vendor.
		timeout = 45 * time.Second
	}
	res := &CouncilResult{}

	ctxBlock := ""
	if strings.TrimSpace(taskContext) != "" {
		ctxBlock = "\nCONTEXT:\n" + strings.TrimSpace(taskContext) + "\n"
	}

	// Round 0: the author drafts the initial plan.
	plan, err := complete(ctx, 0, cfg.Author,
		"You write precise, pragmatic engineering plans.",
		fmt.Sprintf(councilAuthorDraft, authorVendorLabel(cfg.AuthorID), strings.TrimSpace(task), ctxBlock))
	if err != nil {
		return nil, fmt.Errorf("council: author draft: %w", err)
	}
	res.Plan = plan
	res.Transcript = append(res.Transcript, CouncilTurn{Role: "author", Model: cfg.AuthorID, Round: 0, Text: plan})

	// Build the ordered adversary list: primary first, then fallbacks.
	advs := []AdversaryOption{}
	if cfg.Adversary != nil {
		advs = append(advs, AdversaryOption{Provider: cfg.Adversary, ID: cfg.AdversaryID})
	}
	advs = append(advs, cfg.Fallbacks...)
	if len(advs) == 0 {
		return res, nil // solo draft — no adversary configured
	}

	// Pick the first adversary that produces an opening critique (some models —
	// e.g. a flaky endpoint — error immediately; fall through to the next
	// vendor rather than degrade to a solo plan).
	var adv AdversaryOption
	var firstCritique string
	picked := false
	for _, a := range advs {
		critique, err := complete(ctx, timeout, a.Provider,
			"You are an independent, critical senior reviewer. Concrete over vague.",
			fmt.Sprintf(councilCritique, authorVendorLabel(a.ID), authorVendorLabel(cfg.AuthorID), res.Plan))
		if err != nil {
			continue // this adversary is down; try the next vendor
		}
		adv, firstCritique, picked = a, critique, true
		break
	}
	if !picked {
		res.AdversaryFailed = true
		res.Dissent = "adversary unavailable (all cross-vendor candidates errored)"
		return res, nil
	}
	res.AdversaryID = adv.ID
	res.CrossVendor = VendorOf(cfg.AuthorID) != VendorOf(adv.ID)

	critique := firstCritique
	for round := 1; round <= rounds; round++ {
		if round > 1 {
			var err error
			critique, err = complete(ctx, timeout, adv.Provider,
				"You are an independent, critical senior reviewer. Concrete over vague.",
				fmt.Sprintf(councilCritique, authorVendorLabel(adv.ID), authorVendorLabel(cfg.AuthorID), res.Plan))
			if err != nil {
				res.Dissent = "adversary unavailable mid-loop: " + err.Error()
				res.Rounds = round - 1
				return res, nil
			}
		}
		res.Transcript = append(res.Transcript, CouncilTurn{Role: "adversary", Model: adv.ID, Round: round, Text: critique})

		if verdictApprove(critique) {
			res.Converged = true
			res.Rounds = round
			return res, nil
		}

		// Author revises to address the critique.
		revised, err := complete(ctx, 0, cfg.Author,
			"You write precise, pragmatic engineering plans and take critique seriously.",
			fmt.Sprintf(councilRevise, authorVendorLabel(adv.ID), critique, res.Plan))
		if err != nil {
			res.Dissent = stripVerdict(critique)
			res.Rounds = round
			return res, nil
		}
		res.Plan = revised
		res.Transcript = append(res.Transcript, CouncilTurn{Role: "author", Model: cfg.AuthorID, Round: round, Text: revised})
		res.Rounds = round

		// Last round and still revising → capture the standing dissent.
		if round == rounds {
			res.Dissent = stripVerdict(critique)
		}
	}
	return res, nil
}

// complete runs one single-shot completion, bounded by timeout so a hanging
// endpoint doesn't block the council (the caller treats the error as a failed
// model and falls through).
func complete(ctx context.Context, timeout time.Duration, p Provider, system, user string) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	resp, err := p.Complete(ctx, Request{
		System:   system,
		Messages: []Message{{Role: RoleUser, Text: user}},
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(resp.Text)
	if out == "" {
		return "", fmt.Errorf("empty response")
	}
	return out, nil
}

// verdictApprove reports whether the critique's final verdict is APPROVE.
func verdictApprove(critique string) bool {
	v := lastVerdict(critique)
	return strings.EqualFold(v, "APPROVE")
}

// lastVerdict extracts the verdict token from the last "VERDICT: X" line.
func lastVerdict(s string) string {
	verdict := ""
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if i := strings.Index(strings.ToUpper(t), "VERDICT:"); i >= 0 {
			verdict = strings.TrimSpace(t[i+len("VERDICT:"):])
		}
	}
	// Keep only the leading word (APPROVE/REVISE), drop trailing punctuation.
	fields := strings.Fields(verdict)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], ".*_`")
}

// stripVerdict removes the trailing VERDICT line for cleaner dissent display.
func stripVerdict(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" || strings.HasPrefix(strings.ToUpper(last), "VERDICT:") {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// FormatCouncil renders a council result for display: a one-line provenance
// header (who planned, who challenged, converged or not), the hardened plan,
// and any standing dissent.
func FormatCouncil(res *CouncilResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	switch {
	case res.AdversaryFailed:
		b.WriteString("Plan (solo — the cross-vendor adversary was unavailable, so this is unhardened):\n\n")
	case !res.CrossVendor && res.Rounds == 0:
		b.WriteString("Plan (solo — no cross-vendor adversary available):\n\n")
	case res.Converged:
		fmt.Fprintf(&b, "Plan (hardened over %d cross-vendor round(s); adversary APPROVED):\n\n", res.Rounds)
	default:
		fmt.Fprintf(&b, "Plan (hardened over %d cross-vendor round(s); adversary still has objections):\n\n", res.Rounds)
	}
	b.WriteString(strings.TrimSpace(res.Plan))
	if res.Dissent != "" && !res.AdversaryFailed {
		b.WriteString("\n\n---\nUnresolved adversary objections (judge before executing):\n")
		b.WriteString(strings.TrimSpace(res.Dissent))
	}
	return b.String()
}
