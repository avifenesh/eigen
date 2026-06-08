// Package agent implements eigen's tool-use loop: drive a provider, execute the
// tool calls it returns, feed results back, and repeat until the model is done.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// Approver decides whether a mutating tool call may run in gated mode. It is
// context-aware so a UI can cancel a pending prompt, and returns an error
// distinct from a plain "no".
type Approver func(ctx context.Context, name string, args json.RawMessage) (bool, error)

// EventKind classifies an agent event.
type EventKind int

const (
	EventTextDelta      EventKind = iota // streamed assistant text
	EventReasoningDelta                  // streamed reasoning summary
	EventToolStart                       // a tool call is about to run
	EventToolResult                      // a tool finished
	EventDone                            // the loop produced its final answer
)

// Event is a structured observation emitted during a run. A CLI prints it; a
// TUI renders it. It is the single seam between the loop and any front-end.
type Event struct {
	Kind     EventKind
	Step     int
	Text     string          // delta text, or final answer for EventDone
	ToolName string          // EventToolStart / EventToolResult
	ToolID   string          // EventToolStart / EventToolResult
	ToolArgs json.RawMessage // EventToolStart
	Result   string          // EventToolResult
	IsError  bool            // EventToolResult
}

// EventSink receives agent events. It must not block for long.
type EventSink func(Event)

// Agent drives a provider through the tool-use loop.
type Agent struct {
	Provider llm.Provider
	Tools    *tool.Registry
	Perm     Permission
	MaxSteps int
	Approve  Approver

	// OnEvent, if set, receives the structured event stream (deltas, tool
	// lifecycle, final answer). Streaming deltas only appear if the provider
	// implements llm.Streamer.
	OnEvent EventSink
}

// maxToolOutput caps a single tool result fed back to the model, so a runaway
// tool (huge file, verbose command) can't blow up memory or the next request.
const maxToolOutput = 100_000

// maxEmptyTurns bounds how many times we nudge the model after it returns a
// turn with neither tool calls nor text (e.g. a reasoning-only response),
// preventing both a premature empty exit and an infinite spin.
const maxEmptyTurns = 2

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
	emptyTurns := 0

	for step := 0; step < maxSteps; step++ {
		req := llm.Request{
			System:   systemPrompt,
			Messages: msgs,
			Tools:    specs,
		}
		var resp *llm.Response
		var err error
		if s, ok := a.Provider.(llm.Streamer); ok && a.OnEvent != nil {
			sink := func(c llm.StreamChunk) {
				kind := EventTextDelta
				if c.Kind == llm.ChunkReasoning {
					kind = EventReasoningDelta
				}
				a.emit(Event{Kind: kind, Step: step, Text: c.Text})
			}
			resp, err = s.Stream(ctx, req, sink)
		} else {
			resp, err = a.Provider.Complete(ctx, req)
		}
		if err != nil {
			return "", err
		}
		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Text) != "" {
				a.emit(Event{Kind: EventDone, Step: step, Text: resp.Text})
				return resp.Text, nil // final answer
			}
			// Empty turn (e.g. reasoning-only): nudge to act, bounded.
			emptyTurns++
			if emptyTurns > maxEmptyTurns {
				return "", fmt.Errorf("model returned no actionable output after %d empty turns", emptyTurns)
			}
			msgs = append(msgs, llm.Message{
				Role: llm.RoleUser,
				Text: "Continue: use a tool to make progress, or give your final answer.",
			})
			continue
		}
		emptyTurns = 0

		msgs = append(msgs, llm.Message{
			Role:        llm.RoleAssistant,
			Text:        resp.Text,
			Reasoning:   resp.Reasoning,
			ReasoningID: resp.ReasoningID,
			ToolCalls:   resp.ToolCalls,
		})
		// Tool calls are dispatched strictly in order, one at a time. This
		// in-order, non-concurrent execution is what makes write/edit (atomic
		// rename) and bash safe without per-path locking; add per-path mutexes
		// before ever parallelizing this loop.
		for _, tc := range resp.ToolCalls {
			a.emit(Event{Kind: EventToolStart, Step: step, ToolName: tc.Name, ToolID: tc.ID, ToolArgs: tc.Arguments})
			result, isErr := a.dispatch(ctx, tc)
			a.emit(Event{Kind: EventToolResult, Step: step, ToolName: tc.Name, ToolID: tc.ID, Result: result, IsError: isErr})
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Text:       result,
				ToolError:  isErr,
			})
		}
	}
	return "", fmt.Errorf("reached MaxSteps (%d) without a final answer", maxSteps)
}

// emit delivers an event to the sink if one is set.
func (a *Agent) emit(e Event) {
	if a.OnEvent != nil {
		a.OnEvent(e)
	}
}

// dispatch runs one tool call, enforcing the permission posture, and returns the
// result (or an error string) to feed back to the model plus whether it failed.
func (a *Agent) dispatch(ctx context.Context, tc llm.ToolCall) (string, bool) {
	def, ok := a.Tools.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("Error: unknown tool %q", tc.Name), true
	}
	if !def.ReadOnly {
		// Fail closed: a mutating tool runs only under an explicitly recognized
		// posture. Any unknown posture denies.
		switch a.Perm {
		case PermAuto:
			// allowed
		case PermGated:
			if a.Approve == nil {
				return fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name), true
			}
			ok, err := a.Approve(ctx, tc.Name, tc.Arguments)
			if err != nil {
				return fmt.Sprintf("Denied: approval failed for %q: %v", tc.Name, err), true
			}
			if !ok {
				return fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name), true
			}
		default:
			return fmt.Sprintf("Denied: tool %q blocked under unknown permission posture %q.", tc.Name, a.Perm), true
		}
	}
	out, err := def.Run(ctx, tc.Arguments)
	if err != nil {
		return "Error: " + err.Error(), true
	}
	if len(out) > maxToolOutput {
		out = tool.TruncateUTF8(out, maxToolOutput) + "\n[output truncated]"
	}
	return out, false
}
