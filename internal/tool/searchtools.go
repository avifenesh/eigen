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
//	search_tools("chrome")            → the chrome server's tool NAMES (+1-liners)
//	search_tools("chrome navigate")   → the full schema for the matching tool(s),
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
		Description: "Discover and UNLOCK tools beyond the core set. Core tools (read/edit/grep/bash/…) are always available; more are grouped under servers listed in your instructions (browser & desktop automation, etc.). Call with a server name (e.g. \"chrome\", \"workspace\") to see that server's tool names, or a capability keyword (e.g. \"navigate\", \"screenshot\", \"click\") to get matching tools' full schemas and make them callable. Use it whenever a task needs something the core tools don't cover — don't assume a capability is missing without searching.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": { "type": "string", "description": "A server name to browse its tools, or a capability keyword to open matching tools. Empty lists the servers." }
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

			// Level 1: the query is exactly a group/server name → list its tool
			// names (cheap), don't dump every schema. The model then drills into
			// a specific tool.
			for _, g := range r.GroupNames() {
				if strings.EqualFold(g, q) {
					tools := r.GroupTools(g)
					var b strings.Builder
					fmt.Fprintf(&b, "%s server — %d tools (search_tools with a tool name or keyword to open its full schema + make it callable):\n- %s",
						g, len(tools), strings.Join(tools, "\n- "))
					return b.String(), nil
				}
			}

			// Level 2: keyword/tool match → full schemas + unlock.
			matches := r.MatchNiche(q)
			if len(matches) == 0 {
				return fmt.Sprintf("no tools match %q. Run search_tools with an empty query to list the groups.", q), nil
			}
			// Guard against a too-broad keyword opening an entire huge server:
			// if it resolves to one whole group, treat it as a Level-1 browse.
			if onlyGroup := singleGroup(matches); onlyGroup != "" && len(matches) > 12 {
				tools := r.GroupTools(onlyGroup)
				return fmt.Sprintf("%q matches the whole %s server (%d tools) — its tool names (search_tools with a specific one to open it):\n- %s",
					q, onlyGroup, len(tools), strings.Join(tools, "\n- ")), nil
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
		},
	}
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
