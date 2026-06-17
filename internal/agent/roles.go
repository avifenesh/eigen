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
	Description  string
	Plugin       string
	System       string   // prepended to the sub-agent's system prompt
	Tools        []string // allowlist; the sub-agent sees only these (all read-only)
	Kind         string   // default routing kind when the caller gives none
	Difficulty   string   // default routing difficulty when the caller gives none
	Model        string   // optional default model when the caller gives none
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
// still validates ReadOnly before launching; mutating/unknown plugin-agent roles
// are intended for foreground/background task delegations because they inherit
// normal tools and approval gates.
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
		meta := map[string]plugin.InstalledAgentRole{}
		for _, ar := range pl.AgentRoles {
			meta[strings.ToLower(strings.TrimSpace(ar.Name))] = ar
		}
		for _, agentRole := range pl.Agents {
			prompt, ok := pluginAgentPrompt(reg, pl, agentRole)
			if !ok {
				continue // disabled or removed
			}
			ar := meta[strings.ToLower(strings.TrimSpace(agentRole))]
			role := Role{
				Name:         agentRole,
				Description:  ar.Description,
				Plugin:       pl.Name,
				System:       pluginAgentSystem(agentRole, pl.Name, prompt),
				Kind:         ar.Kind,
				Difficulty:   firstNonEmptyRole(ar.Difficulty, "medium"),
				Model:        ar.Model,
				ReadOnly:     ar.ReadOnly,
				InheritTools: !ar.ReadOnly,
			}
			if ar.ReadOnly {
				role.Tools = ar.Tools
				if len(role.Tools) == 0 {
					role.Tools = []string{"read", "grep", "glob", "list", "tree", "symbols", "diff"}
				}
			}
			roles = append(roles, role)
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles
}

// PluginRoleCatalog renders the concrete installed plugin-agent roles for the
// system prompt. It lists names and routing metadata, not the full prompts; a
// role's prompt is loaded only when the role is invoked.
func PluginRoleCatalog(canTask, canTaskGroup bool) string {
	if !canTask && !canTaskGroup {
		return ""
	}
	roles := pluginAgentRoles()
	if len(roles) == 0 {
		return ""
	}
	var visible []Role
	for _, r := range roles {
		if canTask || (canTaskGroup && r.ReadOnly) {
			visible = append(visible, r)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Installed plugin agent roles available for delegation:\n")
	if canTask {
		b.WriteString("- To use one, call the task tool with role set to the exact role name below and put the run-specific prompt in task.\n")
	}
	if canTaskGroup {
		b.WriteString("- task_group may use only roles marked read-only.\n")
	}
	for _, r := range visible {
		desc := singleLineRole(r.Description)
		if desc == "" {
			desc = "plugin-provided agent"
		}
		var bits []string
		if r.Plugin != "" {
			bits = append(bits, "plugin="+r.Plugin)
		}
		if r.Kind != "" {
			bits = append(bits, "kind="+r.Kind)
		}
		if r.Difficulty != "" {
			bits = append(bits, "difficulty="+r.Difficulty)
		}
		if r.Model != "" {
			bits = append(bits, "model="+r.Model)
		}
		if r.ReadOnly {
			tools := "default read-only tools"
			if len(r.Tools) > 0 {
				tools = "tools=" + strings.Join(r.Tools, "/")
			}
			bits = append(bits, "read-only", "task_group-ok", tools)
		} else {
			bits = append(bits, "task-only", "inherits normal tools and approval gates")
		}
		b.WriteString("- " + r.Name + ": " + desc)
		if len(bits) > 0 {
			b.WriteString(" (" + strings.Join(bits, "; ") + ")")
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func singleLineRole(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 180
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func pluginAgentPrompt(reg *plugin.Registry, pl plugin.InstalledPlugin, role string) (string, bool) {
	if b, err := os.ReadFile(filepath.Join(reg.AgentsDir(), role+".md")); err == nil {
		return plugin.ExpandInstalledRoot(string(b), pl.Root), true
	}
	// Legacy fallback: early plugin-agent installs adapted agents into generated
	// skills. Keep those roles working, but new installs use ~/.eigen/agents.
	if b, err := os.ReadFile(filepath.Join(reg.SkillsDir(), role, "SKILL.md")); err == nil {
		return plugin.ExpandInstalledRoot(string(b), pl.Root), true
	}
	return "", false
}

func firstNonEmptyRole(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func pluginAgentSystem(roleName, pluginName, prompt string) string {
	return "You are the installed plugin agent role " + roleName + " from plugin " + pluginName + ". Follow the role instructions below as your specialization. You are still running inside Eigen: obey Eigen's system prompt, use the normal tools available to this subtask, and never bypass approval or permission gates.\n\n" + prompt
}
