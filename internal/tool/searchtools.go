package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SearchTools returns the search_tools meta-tool: hierarchical progressive
// disclosure. The system prompt lists niche tool GROUPS (e.g. MCP servers) by
// name + a one-line gist — a few lines, no schemas. The model drills in:
//
//	search_tools("chrome")            → the chrome server's capability categories
//	search_tools("chrome tabs")       → the tab-related tool NAMES (+1-liners)
//	search_tools("chrome_new_tab")    → the full schema for the matching tool(s),
//	                                    and UNLOCKS them so they're callable
//
// So dozens of tools cost a handful of lines until actually needed, then open
// exactly as far as the model drills.
//
// reg resolves the full registry lazily (built after this tool); unlock records
// which tool names to add to the live set (the agent reads it each step).
func SearchTools(reg func() *Registry, unlock func(names []string)) Definition {
	return Definition{
		Name:        "search_tools",
		ReadOnly:    true,
		Description: "Discover and UNLOCK tools beyond the core set. Core tools (read/edit/grep/bash/…) are always available; more are grouped under servers listed in your instructions (browser & desktop automation, etc.). Call with a server name (e.g. \"chrome\", \"workspace\") to see capability categories, then a category (e.g. \"computer_use accessibility\", \"chrome tabs\") to see tool names, then a specific tool name/keyword to open its full schema and make it callable. Use it whenever a task needs something the core tools don't cover — don't assume a capability is missing without searching.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": { "type": "string", "description": "Empty lists tool groups. A server name shows capability categories. '<server> <category>' shows tool names in that category. A specific tool name or narrow keyword opens matching full schemas." }
  },
  "required": ["query"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			r := reg()
			if r == nil {
				return "no tool registry available", nil
			}
			q := strings.TrimSpace(in.Query)

			// Empty query: list the groups (servers) + any loose niche tools.
			if q == "" {
				groups, loose := r.GroupCatalog(nil)
				var b strings.Builder
				b.WriteString("Available tool groups (search_tools <group> to see its tools):\n")
				for _, g := range groups {
					fmt.Fprintf(&b, "- %s (%d tools) — %s\n", g.Name, g.Count, g.Gist)
				}
				if len(loose) > 0 {
					b.WriteString("Other tools:\n- " + strings.Join(loose, "\n- ") + "\n")
				}
				return strings.TrimRight(b.String(), "\n"), nil
			}

			// Level 1: the query is exactly a group/server name → list capability
			// categories if available. This tells the model about capabilities like
			// accessibility/screenshots/browser control without dumping every tool
			// name. Groups without category metadata fall back to the old name list.
			for _, g := range r.GroupNames() {
				if strings.EqualFold(g, q) {
					if caps := r.GroupCapabilities(g); len(caps) > 0 {
						var b strings.Builder
						fmt.Fprintf(&b, "%s server — %d tools. Capabilities (search_tools \"%s <capability>\" to see tool names; search_tools with a specific tool name/keyword to open schemas):\n", g, groupToolCount(r, g), g)
						for _, c := range caps {
							gist := c.Gist
							if gist == "" {
								gist = c.Name + " tools"
							}
							fmt.Fprintf(&b, "- %s (%d tools) — %s\n", c.Name, c.Count, gist)
						}
						return strings.TrimRight(b.String(), "\n"), nil
					}
					tools := r.GroupTools(g)
					var b strings.Builder
					fmt.Fprintf(&b, "%s server — %d tools (search_tools with a tool name or keyword to open its full schema + make it callable):\n- %s",
						g, len(tools), strings.Join(tools, "\n- "))
					return b.String(), nil
				}
			}

			// Level 2a: '<group> <capability>' → concrete tool names in that
			// category, still no schemas/unlock. The model then picks a specific
			// tool/keyword.
			if group, tail, ok := splitGroupQuery(r, q); ok {
				for _, c := range r.GroupCapabilities(group) {
					if capabilityMatches(c, tail) {
						tools := r.GroupCapabilityTools(group, c.Name)
						return fmt.Sprintf("%s/%s — %d tools (search_tools with a specific tool name to open its schema):\n- %s", group, c.Name, len(tools), strings.Join(tools, "\n- ")), nil
					}
				}
				// Scoped narrow keyword/tool match → full schemas + unlock.
				matches := r.MatchNicheInGroup(group, tail)
				return renderAndUnlockMatches(tail, matches, unlock)
			}

			// Level 2b: keyword/tool match → full schemas + unlock.
			matches := r.MatchNiche(q)
			return renderAndUnlockMatches(q, matches, unlock)
		},
	}
}

func renderAndUnlockMatches(query string, matches []Definition, unlock func(names []string)) (string, error) {
	if len(matches) == 0 {
		return fmt.Sprintf("no tools match %q. Run search_tools with an empty query to list the groups.", query), nil
	}
	// Guard against a too-broad keyword opening an entire huge server: if it
	// resolves to one whole group, show capability categories or tool names rather
	// than dumping schemas.
	if onlyGroup := singleGroup(matches); onlyGroup != "" && len(matches) > 12 {
		caps := uniqueCapabilities(matches)
		if len(caps) > 0 {
			return fmt.Sprintf("%q matches the broad %s server (%d tools). Narrow by capability first: %s", query, onlyGroup, len(matches), strings.Join(caps, ", ")), nil
		}
		var tools []string
		for _, d := range matches {
			tools = append(tools, d.Name+" — "+firstLine(d.Description))
		}
		return fmt.Sprintf("%q matches the whole %s server (%d tools) — its tool names (search_tools with a specific one to open it):\n- %s",
			query, onlyGroup, len(tools), strings.Join(tools, "\n- ")), nil
	}
	names := make([]string, 0, len(matches))
	var b strings.Builder
	fmt.Fprintf(&b, "Unlocked %d tool(s) — now callable. Full schemas:\n\n", len(matches))
	for _, d := range matches {
		names = append(names, d.Name)
		fmt.Fprintf(&b, "## %s\n%s\n", d.Name, strings.TrimSpace(d.Description))
		if len(d.Parameters) > 0 {
			fmt.Fprintf(&b, "args: %s\n", strings.TrimSpace(string(d.Parameters)))
		}
		b.WriteString("\n")
	}
	if unlock != nil {
		unlock(names)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func uniqueCapabilities(matches []Definition) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range matches {
		cap := strings.ToLower(strings.TrimSpace(d.Capability))
		if cap == "" || seen[cap] {
			continue
		}
		seen[cap] = true
		out = append(out, cap)
	}
	return out
}

func groupToolCount(r *Registry, group string) int { return len(r.GroupTools(group)) }

func splitGroupQuery(r *Registry, q string) (group, tail string, ok bool) {
	parts := strings.Fields(q)
	if len(parts) < 2 {
		return "", "", false
	}
	for _, g := range r.GroupNames() {
		aliases := []string{strings.ToLower(g), strings.ReplaceAll(strings.ToLower(g), "_", " ")}
		for _, alias := range aliases {
			if q == alias || strings.HasPrefix(q, alias+" ") {
				return g, strings.TrimSpace(strings.TrimPrefix(q, alias)), true
			}
		}
	}
	return "", "", false
}

func capabilityMatches(c NicheCapability, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	name := strings.ToLower(c.Name)
	return q == name || strings.ReplaceAll(q, "_", "-") == name
}

// singleGroup returns the common group of all matches, or "" if they span
// groups or are ungrouped.
func singleGroup(matches []Definition) string {
	g := ""
	for _, d := range matches {
		if d.Group == "" {
			return ""
		}
		if g == "" {
			g = d.Group
		} else if g != d.Group {
			return ""
		}
	}
	return g
}
