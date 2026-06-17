package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/plugin"
)

// Roles are named sub-agent specializations for multi-agent fan-out (Tier 16).
// A role sets the sub-agent's system framing, its tool allowlist, and a default
// routing difficulty. v1 ships hardcoded READ-ONLY roles only: a read-only
// child never invokes Approve, so N parallel children can't race the single
// approval prompt, and parallel children can't corrupt the workspace with
// concurrent writes. Mutating roles (implementer/tester) are deliberately
// deferred until isolated per-child workspaces + a serialized approval queue
// exist (see ROADMAP Tier 16).
type Role struct {
	Name         string
	System       string   // prepended to the sub-agent's system prompt
	Tools        []string // allowlist; the sub-agent sees only these (all read-only)
	Difficulty   string   // default routing difficulty when the caller gives none
	ReadOnly     bool     // every tool in Tools is read-only (enforced at build)
	InheritTools bool     // plugin agent: keep caller's normal toolset/approval gates
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

// LookupRole returns a built-in or enabled plugin-agent role by name. task_group
// still validates ReadOnly before launching; plugin-agent roles are intended for
// foreground/background task delegations because they inherit normal tools.
func LookupRole(name string) (Role, bool) {
	name = strings.TrimSpace(name)
	if r, ok := builtinRoles[name]; ok {
		return r, true
	}
	return lookupPluginAgentRole(name)
}

// implementerSystem frames a mutating fan-out child. Its toolset comes from
// WorktreeTools (rooted at the child's isolated worktree), not a role
// allowlist — so it's defined here, not in builtinRoles (which are read-only).
const implementerSystem = "You are an IMPLEMENTER sub-agent working in your OWN isolated copy of the repo — one of several parallel workers. Make ONLY the change described in your task by editing files in your workspace. You have read/search/write/edit/move tools; you have NO shell, NO git, and NO network. Do not try to commit, push, build, or run tests — when you finish, your file changes are captured as a patch automatically. Keep your edits tightly scoped to your task so they merge cleanly with the others."

// RoleNames lists the built-in read-only role names (for task_group docs/errors).
func RoleNames() []string {
	return []string{"researcher", "reviewer", "summarizer"}
}

// PluginRoleNames lists installed plugin agents that are currently enabled.
// These roles are valid for the foreground/background task tool.
func PluginRoleNames() []string {
	roles := pluginAgentRoles()
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

func lookupPluginAgentRole(name string) (Role, bool) {
	for _, r := range pluginAgentRoles() {
		if strings.EqualFold(r.Name, name) {
			return r, true
		}
	}
	return Role{}, false
}

func pluginAgentRoles() []Role {
	reg, err := plugin.NewRegistry()
	if err != nil {
		return nil
	}
	installed, err := reg.Installed()
	if err != nil {
		return nil
	}
	var roles []Role
	for _, pl := range installed {
		for _, agentSkill := range pl.Agents {
			md, err := os.ReadFile(filepath.Join(reg.SkillsDir(), agentSkill, "SKILL.md"))
			if err != nil {
				continue // disabled or removed
			}
			roles = append(roles, Role{
				Name:         agentSkill,
				System:       pluginAgentSystem(agentSkill, pl.Name, string(md)),
				Difficulty:   "medium",
				ReadOnly:     false,
				InheritTools: true,
			})
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles
}

func pluginAgentSystem(roleName, pluginName, prompt string) string {
	return "You are the installed plugin agent role " + roleName + " from plugin " + pluginName + ". Follow the role instructions below as your specialization. You are still running inside Eigen: obey Eigen's system prompt, use the normal tools available to this subtask, and never bypass approval or permission gates.\n\n" + prompt
}
