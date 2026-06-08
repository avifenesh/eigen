// Package agent implements eigen's tool-use loop: drive a provider, execute the
// tool calls it returns, feed results back, and repeat until the model is done.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// Permission is the loop's autonomy posture.
type Permission string

const (
	// PermGated auto-runs read-only tools and asks before mutating ones.
	PermGated Permission = "gated"
	// PermAuto runs every tool without prompting.
	PermAuto Permission = "auto"
)

const systemPrompt = `You are eigen, a coding agent that works directly in the user's project.
Use the provided tools to inspect and modify files to accomplish the task.
Call tools as needed; when the task is complete, reply with a short summary.`

// Approver decides whether a mutating tool call may run in gated mode.
type Approver func(name string, args json.RawMessage) bool

// Agent drives a provider through the tool-use loop.
type Agent struct {
	Provider llm.Provider
	Tools    *tool.Registry
	Perm     Permission
	MaxSteps int
	Approve  Approver

	// OnStep, if set, is called once per loop step with the model's response,
	// for observability (logging which tools were chosen, etc.).
	OnStep func(step int, resp *llm.Response)
}

// maxToolOutput caps a single tool result fed back to the model, so a runaway
// tool (huge file, verbose command) can't blow up memory or the next request.
const maxToolOutput = 100_000

// Run executes the loop until the model stops calling tools or MaxSteps is hit.
func (a *Agent) Run(ctx context.Context, task string) (string, error) {
	if a.Provider == nil {
		return "", fmt.Errorf("agent: nil provider")
	}
	if a.Tools == nil {
		return "", fmt.Errorf("agent: nil tools")
	}
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}
	msgs := []llm.Message{{Role: llm.RoleUser, Text: task}}
	specs := a.Tools.Specs()

	for step := 0; step < maxSteps; step++ {
		resp, err := a.Provider.Complete(ctx, llm.Request{
			System:   systemPrompt,
			Messages: msgs,
			Tools:    specs,
		})
		if err != nil {
			return "", err
		}
		if a.OnStep != nil {
			a.OnStep(step, resp)
		}
		if len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}

		msgs = append(msgs, llm.Message{
			Role:      llm.RoleAssistant,
			Text:      resp.Text,
			ToolCalls: resp.ToolCalls,
		})
		for _, tc := range resp.ToolCalls {
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Text:       a.dispatch(ctx, tc),
			})
		}
	}
	return "", fmt.Errorf("reached MaxSteps (%d) without a final answer", maxSteps)
}

// dispatch runs one tool call, enforcing the permission posture, and returns the
// result (or an error string) to feed back to the model.
func (a *Agent) dispatch(ctx context.Context, tc llm.ToolCall) string {
	def, ok := a.Tools.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("Error: unknown tool %q", tc.Name)
	}
	if !def.ReadOnly {
		// Fail closed: a mutating tool runs only under an explicitly recognized
		// posture. Any unknown posture denies.
		switch a.Perm {
		case PermAuto:
			// allowed
		case PermGated:
			if a.Approve == nil || !a.Approve(tc.Name, tc.Arguments) {
				return fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name)
			}
		default:
			return fmt.Sprintf("Denied: tool %q blocked under unknown permission posture %q.", tc.Name, a.Perm)
		}
	}
	out, err := def.Run(ctx, tc.Arguments)
	if err != nil {
		return "Error: " + err.Error()
	}
	if len(out) > maxToolOutput {
		out = out[:maxToolOutput] + "\n[output truncated]"
	}
	return out
}
