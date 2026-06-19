// Package tool defines eigen's tool contract and registry. Tool argument
// schemas are hand-written JSON Schema (explicit, full control over the exact
// shape sent to each model), and each tool exposes a provider-neutral spec.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// Definition is a single tool eigen can expose to a model.
type Definition struct {
	Name        string
	Description string

	// Parameters is a hand-written JSON Schema object for the tool's arguments.
	Parameters json.RawMessage

	// ReadOnly marks tools that never mutate state; they auto-run even in gated mode.
	ReadOnly bool

	// Disabled marks a tool constructed for this session but intentionally omitted
	// from the registry (for example, git-only tools when the root is not a git
	// worktree). A disabled tool is not advertised and cannot be dispatched.
	Disabled bool

	// Niche marks a tool whose full JSON schema is withheld from the model by
	// default (progressive disclosure): it's listed by name+description in the
	// system prompt, and the model unlocks its full spec on demand via the
	// search_tools meta-tool. This keeps dozens of occasional tools (e.g. the
	// MCP workspace/chrome servers) from spending ~10k tokens of schema on
	// EVERY request. Core tools (read/edit/grep/bash/…) are never niche.
	Niche bool

	// Group namespaces a niche tool (e.g. its MCP server: "workspace",
	// "chrome"). Disclosure is hierarchical: the prompt lists GROUPS (one line
	// each), search_tools <group> reveals that group's tool NAMES, and
	// search_tools <tool/keyword> reveals full schemas. Empty = ungrouped niche.
	Group string

	// GroupDesc is the group's one-line "what is this" shown at Level 0 (e.g.
	// the MCP server's description/instructions). All tools in a group should
	// carry the same GroupDesc; the first non-empty one wins.
	GroupDesc string

	// Capability is an optional Level-1 category inside a niche group. It lets
	// search_tools show what a server can do (accessibility, windows, screenshots,
	// browser navigation, terminals, etc.) without dumping every concrete tool
	// name or schema at once.
	Capability     string
	CapabilityDesc string

	// Run executes the tool with raw JSON arguments and returns its textual result.
	// Text-only tools (the vast majority) implement this.
	Run func(ctx context.Context, args json.RawMessage) (string, error)

	// RunRich is the optional image-capable variant: a tool that returns visual
	// output (a screenshot, a rendered page) implements this instead of Run.
	// When set it takes precedence over Run. Keeping both lets the ~all
	// text-only tools stay unchanged while computer-use / browser tools return
	// images the agent threads into the tool-result message.
	RunRich func(ctx context.Context, args json.RawMessage) (Result, error)
}

// Result is a tool's output: text plus optional images (screenshots, renders).
// Image-returning tools (browser, computer-use) populate Images; the agent
// carries them into the RoleTool message, and each provider serializes them
// per its capabilities (image-in-tool_result for Anthropic/Bedrock, a
// synthetic follow-up user image for OpenAI).
type Result struct {
	Text   string
	Images []llm.Image
}

// Spec returns the provider-neutral spec for this tool. Parameters is already
// in canonical compact form — schemas are normalized once at registration
// (NewRegistry), not here in the per-step prompt-assembly path. Pretty-printing
// a schema for humans is a render-time concern (see internal/tui/jsonview).
func (d Definition) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: d.Name, Description: d.Description, Parameters: d.Parameters}
}

// compactJSON returns raw with insignificant whitespace removed (canonical
// compact form). On any error (or empty input) it returns the input unchanged —
// normalization must never corrupt a schema.
func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return json.RawMessage(buf.Bytes())
}

// Invoke runs the tool, normalizing the text-only Run and the image-capable
// RunRich into one Result. RunRich wins when both are set.
func (d Definition) Invoke(ctx context.Context, args json.RawMessage) (Result, error) {
	if d.RunRich != nil {
		return d.RunRich(ctx, args)
	}
	text, err := d.Run(ctx, args)
	return Result{Text: text}, err
}

// Registry is an ordered, name-keyed set of tools.
type Registry struct {
	order  []string
	byName map[string]Definition
}

// NewRegistry builds a registry, validating that every tool has a non-empty
// unique name and a non-nil Run.
func NewRegistry(defs ...Definition) (*Registry, error) {
	r := &Registry{byName: make(map[string]Definition, len(defs))}
	for _, d := range defs {
		if d.Disabled {
			continue
		}
		if d.Name == "" {
			return nil, fmt.Errorf("tool with empty name")
		}
		if d.Run == nil && d.RunRich == nil {
			return nil, fmt.Errorf("tool %q has nil Run and RunRich", d.Name)
		}
		if _, dup := r.byName[d.Name]; dup {
			return nil, fmt.Errorf("duplicate tool %q", d.Name)
		}
		// Normalize the JSON Schema to its canonical compact form ONCE, here at
		// the authoring→runtime boundary. Source literals may be pretty-printed
		// for readability; the prompt/data plane always carries compact JSON
		// (no indentation/newlines re-sent to the model every turn). Human
		// pretty-printing is a render-time concern (internal/tui/jsonview).
		d.Parameters = compactJSON(d.Parameters)
		r.order = append(r.order, d.Name)
		r.byName[d.Name] = d
	}
	return r, nil
}

// Specs returns the provider-neutral specs in registration order.
func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		specs = append(specs, r.byName[name].Spec())
	}
	return specs
}

// CoreSpecs returns specs for the non-niche tools plus any niche tools in the
// unlocked set — the actual tool list sent to the model under progressive
// disclosure. unlocked may be nil.
func (r *Registry) CoreSpecs(unlocked map[string]bool) []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		d := r.byName[name]
		if d.Niche && !unlocked[name] {
			continue
		}
		specs = append(specs, d.Spec())
	}
	return specs
}

// HasNiche reports whether any tool is niche (so the disclosure machinery — the
// catalog line + search_tools — is worth wiring).
func (r *Registry) HasNiche() bool {
	for _, name := range r.order {
		if r.byName[name].Niche {
			return true
		}
	}
	return false
}

// NicheGroup summarizes a namespace of niche tools (an MCP server) for the
// top-level catalog: the group name, how many tools, and a one-line gist.
type NicheGroup struct {
	Name  string
	Count int
	Gist  string
}

// NicheCapability summarizes a Level-1 category inside a niche group. It is a
// middle disclosure layer: enough for the model to know capabilities exist,
// cheap enough not to expose every concrete tool/schema in the prompt.
type NicheCapability struct {
	Name  string
	Count int
	Gist  string
}

// GroupCatalog returns the hierarchical Level-0 catalog: one entry per niche
// GROUP (e.g. an MCP server) plus any ungrouped niche tools listed by name.
// unlocked tools/groups are omitted. This is what rides in the system prompt —
// a handful of lines regardless of how many tools each group holds.
func (r *Registry) GroupCatalog(unlocked map[string]bool) (groups []NicheGroup, loose []string) {
	seen := map[string]int{}
	gist := map[string]string{}
	for _, name := range r.order {
		d := r.byName[name]
		if !d.Niche {
			continue
		}
		if d.Group == "" {
			if !unlocked[name] {
				loose = append(loose, name+" — "+firstLine(d.Description))
			}
			continue
		}
		seen[d.Group]++
		if gist[d.Group] == "" && d.GroupDesc != "" {
			gist[d.Group] = d.GroupDesc
		}
	}
	// Stable order: by first appearance.
	added := map[string]bool{}
	for _, name := range r.order {
		d := r.byName[name]
		if d.Group == "" || added[d.Group] {
			continue
		}
		added[d.Group] = true
		g := gist[d.Group]
		if g == "" {
			g = d.Group + " tools"
		}
		groups = append(groups, NicheGroup{Name: d.Group, Count: seen[d.Group], Gist: g})
	}
	return groups, loose
}

// GroupCapabilities returns the Level-1 capability categories for a niche
// group. Empty means the group has no category metadata and should fall back to
// listing concrete tool names.
func (r *Registry) GroupCapabilities(group string) []NicheCapability {
	g := strings.ToLower(strings.TrimSpace(group))
	count := map[string]int{}
	gist := map[string]string{}
	order := []string{}
	seen := map[string]bool{}
	for _, name := range r.order {
		d := r.byName[name]
		if !d.Niche || strings.ToLower(d.Group) != g || d.Capability == "" {
			continue
		}
		cap := strings.ToLower(strings.TrimSpace(d.Capability))
		if cap == "" {
			continue
		}
		if !seen[cap] {
			seen[cap] = true
			order = append(order, cap)
		}
		count[cap]++
		if gist[cap] == "" && d.CapabilityDesc != "" {
			gist[cap] = d.CapabilityDesc
		}
	}
	out := make([]NicheCapability, 0, len(order))
	for _, cap := range order {
		out = append(out, NicheCapability{Name: cap, Count: count[cap], Gist: gist[cap]})
	}
	return out
}

// GroupCapabilityTools returns "name — first line" for every niche tool in a
// group/capability pair. Empty if unknown.
func (r *Registry) GroupCapabilityTools(group, capability string) []string {
	g := strings.ToLower(strings.TrimSpace(group))
	cap := strings.ToLower(strings.TrimSpace(capability))
	var out []string
	for _, name := range r.order {
		d := r.byName[name]
		if d.Niche && strings.ToLower(d.Group) == g && strings.ToLower(d.Capability) == cap {
			out = append(out, d.Name+" — "+firstLine(d.Description))
		}
	}
	return out
}

// GroupTools returns "name — first line" for every niche tool in a group. It is
// now a fallback when the group has no capability categories.
func (r *Registry) GroupTools(group string) []string {
	g := strings.ToLower(strings.TrimSpace(group))
	var out []string
	for _, name := range r.order {
		d := r.byName[name]
		if d.Niche && strings.ToLower(d.Group) == g {
			out = append(out, d.Name+" — "+firstLine(d.Description))
		}
	}
	return out
}

// GroupNames returns the distinct niche group names.
func (r *Registry) GroupNames() []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range r.order {
		d := r.byName[name]
		if d.Niche && d.Group != "" && !seen[d.Group] {
			seen[d.Group] = true
			out = append(out, d.Group)
		}
	}
	return out
}

// MatchNiche returns the niche tools whose name or description matches query
// (case-insensitive substring; empty query matches all niche tools). A query
// equal to a GROUP name matches that whole group. Used by search_tools.
func (r *Registry) MatchNiche(query string) []Definition {
	return r.matchNiche(query, "")
}

// MatchNicheInGroup is a scoped variant for queries such as
// `search_tools "computer_use screenshot"`: only tools in the named group are
// eligible, so the model can drill down without accidentally opening a similar
// tool from another server.
func (r *Registry) MatchNicheInGroup(group, query string) []Definition {
	return r.matchNiche(query, strings.ToLower(strings.TrimSpace(group)))
}

func (r *Registry) matchNiche(query, group string) []Definition {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Definition
	for _, name := range r.order {
		d := r.byName[name]
		if !d.Niche {
			continue
		}
		if group != "" && strings.ToLower(d.Group) != group {
			continue
		}
		if q == "" ||
			strings.Contains(strings.ToLower(d.Name), q) ||
			strings.Contains(strings.ToLower(d.Description), q) ||
			(d.Group != "" && strings.Contains(strings.ToLower(d.Group), q)) ||
			(d.Capability != "" && strings.Contains(strings.ToLower(d.Capability), q)) ||
			(d.CapabilityDesc != "" && strings.Contains(strings.ToLower(d.CapabilityDesc), q)) {
			out = append(out, d)
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 140 {
		s = s[:137] + "…"
	}
	return s
}

// Get looks up a tool by name.
func (r *Registry) Get(name string) (Definition, bool) {
	d, ok := r.byName[name]
	return d, ok
}

// Definitions returns all tools in registration order (for catalogs/listing).
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byName[name])
	}
	return out
}

// Subset returns a NEW registry containing only the named tools that exist in
// r, preserving registration order. It never mutates r — sub-agents (e.g.
// parallel task_group children with a role allowlist) get their own immutable
// registry sharing the same underlying tool Definitions, which are safe to
// invoke concurrently. Names not present in r are skipped.
func (r *Registry) Subset(names ...string) *Registry {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	sub := &Registry{byName: make(map[string]Definition)}
	for _, name := range r.order {
		if want[name] {
			sub.order = append(sub.order, name)
			sub.byName[name] = r.byName[name]
		}
	}
	return sub
}

// AllReadOnly reports whether every tool in r is ReadOnly. Parallel fan-out
// (task_group) requires this: a read-only child never calls Approve, so N
// concurrent children can't race the single-window approval prompt.
func (r *Registry) AllReadOnly() bool {
	for _, name := range r.order {
		if !r.byName[name].ReadOnly {
			return false
		}
	}
	return true
}
