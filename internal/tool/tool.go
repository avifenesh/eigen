// Package tool defines eigen's tool contract and registry. Tool argument
// schemas are hand-written JSON Schema (explicit, full control over the exact
// shape sent to each model), and each tool exposes a provider-neutral spec.
package tool

import (
	"context"
	"encoding/json"

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
	Run func(ctx context.Context, args json.RawMessage) (string, error)
}

// Spec returns the provider-neutral spec for this tool.
func (d Definition) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: d.Name, Description: d.Description, Parameters: d.Parameters}
}

// Registry is an ordered, name-keyed set of tools.
type Registry struct {
	order  []string
	byName map[string]Definition
}

// NewRegistry builds a registry; later duplicates of a name are ignored.
func NewRegistry(defs ...Definition) *Registry {
	r := &Registry{byName: make(map[string]Definition, len(defs))}
	for _, d := range defs {
		if _, dup := r.byName[d.Name]; dup {
			continue
		}
		r.order = append(r.order, d.Name)
		r.byName[d.Name] = d
	}
	return r
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
