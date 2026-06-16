// Package llm defines eigen's provider contract: the minimal surface every
// model backend (Bedrock mantle, Bedrock Converse, local llama) implements,
// normalized so the agent loop never sees provider-specific shapes.
package llm

import (
	"context"
	"encoding/json"
)

// Role identifies the author of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolSpec is a provider-neutral tool description. Parameters is a JSON Schema
// object describing the tool's arguments; each provider wraps this into its own
// wire format (Responses function tool, Converse toolSpec, etc.).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is a model's request to invoke a tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Message is one turn in the conversation.
type Message struct {
	Role Role
	Text string

	// Images are visual inputs attached to a user message (vision models only).
	// Providers that support vision serialize them as native image blocks;
	// others ignore them. Only meaningful on RoleUser.
	Images []Image

	// Reasoning is the model's concise reasoning summary for this turn; it is
	// carried back across turns to preserve the chain of thought through tool
	// calls. ReasoningID is the provider's id for that reasoning item, if any.
	Reasoning   string
	ReasoningID string

	// ToolCalls is set on an assistant turn that invokes tools.
	ToolCalls []ToolCall

	// ToolCallID and ToolName identify which call a RoleTool message answers.
	ToolCallID string
	ToolName   string

	// ToolError marks a RoleTool result as a failure, so providers with a
	// native tool-result status (e.g. Converse) can signal it to the model.
	ToolError bool
}

// Image is a visual input: raw bytes plus the IANA media type. Providers
// base64-encode Data into their native image-block format.
type Image struct {
	MediaType string // e.g. "image/png", "image/jpeg", "image/webp", "image/gif"
	Data      []byte // raw (un-encoded) image bytes
}

// Request is a single completion request, normalized across providers.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
}

// Response is a normalized completion result. Either Text, ToolCalls, or both
// may be set; an empty ToolCalls slice signals the model is done.
type Response struct {
	Text        string
	Reasoning   string
	ReasoningID string
	ToolCalls   []ToolCall

	// Usage is the provider-reported token accounting for THIS request
	// (zero when the provider doesn't return it — callers fall back to the
	// ~chars/4 estimate).
	Usage Usage
}

// Usage is real token accounting from a provider response.
type Usage struct {
	InputTokens  int
	OutputTokens int

	// CacheReadTokens is the count of input tokens served from the prompt cache
	// (a HIT — billed at a large discount). CacheWriteTokens is input tokens
	// written INTO the cache this request (a miss/creation — billed at a small
	// premium). Both are 0 when the provider doesn't report caching or it's off.
	// Together with InputTokens they let us see the prompt-cache hit rate, which
	// is the single biggest lever on input-token cost for an always-on agent.
	CacheReadTokens  int
	CacheWriteTokens int
}

// CachedInputTokens returns the share of input tokens served from cache (0..1),
// or 0 when there were no input tokens or no caching. The denominator includes
// cache reads, since providers report fresh input and cache reads separately.
func (u Usage) CacheHitRate() float64 {
	denom := u.InputTokens + u.CacheReadTokens
	if denom == 0 {
		return 0
	}
	return float64(u.CacheReadTokens) / float64(denom)
}

// Provider is any model backend eigen can drive.
type Provider interface {
	// Name is a human-readable label for logs and the model picker, e.g.
	// "claude-opus-4-8 (bedrock converse)".
	Name() string
	// ModelID is the raw, resolvable model id (no provider suffix), e.g.
	// "us.anthropic.claude-opus-4-8". This is what llm.New accepts — so a
	// live /model switch carried across the daemon socket round-trips
	// correctly (Name() does NOT, its suffix breaks reconstruction).
	ModelID() string
	// Complete runs a non-streaming completion.
	Complete(ctx context.Context, req Request) (*Response, error)
}

// ChunkKind classifies a streamed delta.
type ChunkKind int

const (
	ChunkText ChunkKind = iota
	ChunkReasoning
)

// StreamChunk is an incremental delta emitted while a response streams.
type StreamChunk struct {
	Kind ChunkKind
	Text string
}

// StreamSink receives streamed deltas. It must not block for long.
type StreamSink func(StreamChunk)

// Streamer is an optional capability: providers that can stream implement it.
// The final assembled Response is still returned, so callers that don't care
// about deltas can ignore the sink. The agent uses it when a chunk sink is set.
type Streamer interface {
	Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error)
}

// EffortLevels is the global fallback set of reasoning-effort labels, lowest
// to highest, for models without a per-model catalog set. Catalog entries
// (ModelInfo.EffortLevels) take precedence — they list exactly what each model
// accepts (verified live: mantle GPT = none|low..xhigh, adaptive Claude =
// low..xhigh|max, budget Claude = off|low..xhigh). "off"/"minimal"/"none"
// disable or minimize thinking depending on the backend.
var EffortLevels = []string{"off", "minimal", "none", "low", "medium", "high", "xhigh", "max"}

// EffortSetter is an optional capability: providers whose reasoning effort can
// be changed at runtime implement it, so the TUI can switch effort without
// rebuilding the provider. Returns false if the level is unsupported.
type EffortSetter interface {
	SetEffort(level string) bool
	Effort() string
}

// Searcher is an optional capability: providers with a server-side live-search
// toggle (xAI Grok Live Search) implement it, so the TUI can turn web/X search
// off|auto|on at runtime. Returns false for an unknown mode.
type Searcher interface {
	SetSearch(mode string) bool
	SearchMode() string
}

// FastModer is an optional capability: providers with a fast/low-latency
// service tier (Codex "priority") implement it, so the TUI (/fast) and the
// subtask router can turn the fast path on/off without rebuilding the provider.
type FastModer interface {
	SetFast(on bool) bool
	FastMode() bool
}

// ValidEffort reports whether level is one of EffortLevels.
func ValidEffort(level string) bool {
	for _, l := range EffortLevels {
		if l == level {
			return true
		}
	}
	return false
}

// ModelEffortLevels returns the closed set of valid effort values for a model.
// For reasoning models with a catalog entry it returns the per-model set;
// for unknown or non-reasoning models it returns nil (effort not supported).
func ModelEffortLevels(modelID string) []string {
	if info, ok := Lookup(modelID); ok {
		if len(info.EffortLevels) > 0 {
			return info.EffortLevels
		}
		if info.Reasoning {
			// Reasoning model but no per-model set: return the global list as
			// a safe fallback (old entries not yet annotated).
			return EffortLevels
		}
	}
	return nil
}

// effortSupported reports whether level is in a model's accepted set. An empty
// set (non-reasoning / uncataloged) accepts nothing — the caller keeps its
// default. Used to ignore a global EIGEN_REASONING_EFFORT a given model can't
// honor (e.g. "max" on a GPT model, which is Anthropic-only).
func effortSupported(level string, levels []string) bool {
	for _, l := range levels {
		if l == level {
			return true
		}
	}
	return false
}
