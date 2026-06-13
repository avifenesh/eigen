package agent

// Roles are named sub-agent specializations for multi-agent fan-out (Tier 16).
// A role sets the sub-agent's system framing, its tool allowlist, and a default
// routing difficulty. v1 ships hardcoded READ-ONLY roles only: a read-only
// child never invokes Approve, so N parallel children can't race the single
// approval prompt, and parallel children can't corrupt the workspace with
// concurrent writes. Mutating roles (implementer/tester) are deliberately
// deferred until isolated per-child workspaces + a serialized approval queue
// exist (see ROADMAP Tier 16).
type Role struct {
	Name       string
	System     string   // prepended to the sub-agent's system prompt
	Tools      []string // allowlist; the sub-agent sees only these (all read-only)
	Difficulty string   // default routing difficulty when the caller gives none
	ReadOnly   bool     // every tool in Tools is read-only (enforced at build)
}

// builtinRoles are the v1 hardcoded roles. All are read-only — and crucially,
// every tool listed is a NO-APPROVAL (ReadOnly) tool, so a parallel child never
// blocks on the single-window approval prompt. Network tools (fetch/websearch)
// require approval in gated mode by design, so they are intentionally excluded
// from parallel roles; web research stays a foreground `task`.
var builtinRoles = map[string]Role{
	"researcher": {
		Name:       "researcher",
		System:     "You are a RESEARCHER sub-agent. Investigate and report findings — read code, search the tree, trace how things work. You have READ-ONLY local tools (no network, no edits, no commands). Return a concise, concrete findings report the orchestrator can act on: what you found, where (paths/symbols), and what it means. Do not speculate beyond the evidence.",
		Tools:      []string{"read", "grep", "glob", "list", "tree", "symbols", "skill"},
		Difficulty: "easy",
		ReadOnly:   true,
	},
	"reviewer": {
		Name:       "reviewer",
		System:     "You are a REVIEWER sub-agent. Critique the target (code, a diff, a design, an approach) for correctness, edge cases, security, and clarity. You have READ-ONLY tools plus the cross-vendor review tool. Return specific, actionable issues ranked by severity — not vague praise. Cite exact locations.",
		Tools:      []string{"read", "grep", "glob", "list", "tree", "symbols", "diff", "review"},
		Difficulty: "medium",
		ReadOnly:   true,
	},
	"summarizer": {
		Name:       "summarizer",
		System:     "You are a SUMMARIZER sub-agent. Read the named sources and produce a tight, faithful summary — no new claims, no tools beyond reading. Preserve the key facts, decisions, and open questions.",
		Tools:      []string{"read", "grep", "glob", "list", "tree"},
		Difficulty: "trivial",
		ReadOnly:   true,
	},
}

// LookupRole returns a built-in role by name. ok=false for an unknown role
// (callers fail closed — an unknown role must never silently get the full
// toolset).
func LookupRole(name string) (Role, bool) {
	r, ok := builtinRoles[name]
	return r, ok
}

// RoleNames lists the available role names (for tool docs / errors).
func RoleNames() []string {
	return []string{"researcher", "reviewer", "summarizer"}
}
