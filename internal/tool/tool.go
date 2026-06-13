// Package tool defines eigen's tool contract and registry. Tool argument
// schemas are hand-written JSON Schema (explicit, full control over the exact
// shape sent to each model), and each tool exposes a provider-neutral spec.
package tool

import (
	"context"
	"encoding/json"
	"fmt"

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

// Spec returns the provider-neutral spec for this tool.
func (d Definition) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: d.Name, Description: d.Description, Parameters: d.Parameters}
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
		if d.Name == "" {
			return nil, fmt.Errorf("tool with empty name")
		}
		if d.Run == nil && d.RunRich == nil {
			return nil, fmt.Errorf("tool %q has nil Run and RunRich", d.Name)
		}
		if _, dup := r.byName[d.Name]; dup {
			return nil, fmt.Errorf("duplicate tool %q", d.Name)
		}
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
