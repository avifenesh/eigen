// Package gui is the Wails v3 desktop bridge for Eigen. It binds the daemon
// session host (internal/daemon) to a Svelte frontend: a single long-lived
// control client for request/response RPCs, plus one dedicated streaming
// "pump" connection per subscribed session. The DTOs here are the wire shapes
// the frontend sees — JSON-friendly mirrors of the daemon/llm types, with the
// not-cleanly-bindable bits (raw image bytes, json.RawMessage tool args)
// reshaped into strings the TS bindings can consume.
package gui

import (
	"encoding/base64"
	"fmt"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
)

// maxImageBytes caps a single decoded inbound image, so a malformed or hostile
// data URL from the webview can't blow up the daemon's memory.
const maxImageBytes = 16 << 20

// ImageDTO carries an image to/from the frontend as base64 (raw bytes don't
// bind cleanly to TS).
type ImageDTO struct {
	MediaType string `json:"mediaType"`
	Data      string `json:"data"` // base64-encoded
}

// ToolCallDTO is an assistant tool invocation; Args is the raw JSON arguments
// as a string (json.RawMessage doesn't bind cleanly).
type ToolCallDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// MessageDTO mirrors llm.Message for the transcript seed (full history).
type MessageDTO struct {
	Role       string        `json:"role"`
	Text       string        `json:"text"`
	Reasoning  string        `json:"reasoning,omitempty"`
	ToolCalls  []ToolCallDTO `json:"toolCalls,omitempty"`
	ToolCallID string        `json:"toolCallId,omitempty"`
	ToolName   string        `json:"toolName,omitempty"`
	ToolError  bool          `json:"toolError,omitempty"`
	Images     []ImageDTO    `json:"images,omitempty"`
}

// WireEventDTO mirrors daemon.WireEvent (the streamed agent event) with
// ToolArgs flattened to a string.
type WireEventDTO struct {
	Kind      string `json:"kind"`
	Step      int    `json:"step,omitempty"`
	Text      string `json:"text,omitempty"`
	ToolName  string `json:"tool,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	ToolArgs  string `json:"toolArgs,omitempty"`
	Result    string `json:"result,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
	InTokens  int    `json:"inTokens,omitempty"`
	OutTokens int    `json:"outTokens,omitempty"`
	// EventDone attribution: which provider/model produced the turn, plus the
	// turn's prompt-cache hits/writes (cacheReadTokens vs inTokens is the hit rate).
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	CacheReadTokens  int    `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int    `json:"cacheWriteTokens,omitempty"`
}

// StreamEventDTO is the payload emitted on the per-session event channel.
type StreamEventDTO struct {
	Event  WireEventDTO `json:"event"`
	Replay bool         `json:"replay"`
}

// SessionInfoDTO mirrors daemon.SessionInfo for the session board.
type SessionInfoDTO struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Dir     string `json:"dir"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Turns   int    `json:"turns"`
	Views   int    `json:"views"`
	Updated int64  `json:"updated"`
}

// SessionStateDTO mirrors daemon.SessionState — the full snapshot the chat view
// needs to render history + status.
type SessionStateDTO struct {
	Messages  []MessageDTO          `json:"messages"`
	Tokens    int                   `json:"tokens"`
	Title     string                `json:"title,omitempty"`
	Model     string                `json:"model"`
	Provider  string                `json:"provider"`
	MaxTokens int                   `json:"maxTokens"`
	Perm      string                `json:"perm"`
	Goal      string                `json:"goal"`
	Effort    string                `json:"effort,omitempty"`
	Search    string                `json:"search,omitempty"`
	Fast      bool                  `json:"fast,omitempty"`
	FastOK    bool                  `json:"fastOk,omitempty"`
	Running   bool                  `json:"running,omitempty"`
	Tools     []daemon.ToolInfo     `json:"tools,omitempty"`
	Roots     []string              `json:"roots,omitempty"`
	Shells    []daemon.ShellInfo    `json:"shells,omitempty"`
	Pending   []daemon.ApprovalInfo `json:"pending,omitempty"`
}

// CompactResultDTO reports a compaction's message-count delta.
type CompactResultDTO struct {
	Before int `json:"before"`
	After  int `json:"after"`
}

// HealthDTO is the daemon-connection health signal pushed to the frontend.
type HealthDTO struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func toImageDTOs(in []llm.Image) []ImageDTO {
	if len(in) == 0 {
		return nil
	}
	out := make([]ImageDTO, 0, len(in))
	for _, im := range in {
		out = append(out, ImageDTO{MediaType: im.MediaType, Data: base64.StdEncoding.EncodeToString(im.Data)})
	}
	return out
}

func fromImageDTOs(in []ImageDTO) ([]llm.Image, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]llm.Image, 0, len(in))
	for _, d := range in {
		raw, err := base64.StdEncoding.DecodeString(d.Data)
		if err != nil {
			return nil, fmt.Errorf("bad image data: %w", err)
		}
		if len(raw) > maxImageBytes {
			return nil, fmt.Errorf("image too large (%d bytes, max %d)", len(raw), maxImageBytes)
		}
		out = append(out, llm.Image{MediaType: d.MediaType, Data: raw})
	}
	return out, nil
}

func toMessageDTO(m llm.Message) MessageDTO {
	var tcs []ToolCallDTO
	if len(m.ToolCalls) > 0 {
		tcs = make([]ToolCallDTO, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			tcs = append(tcs, ToolCallDTO{ID: tc.ID, Name: tc.Name, Args: string(tc.Arguments)})
		}
	}
	return MessageDTO{
		Role: string(m.Role), Text: m.Text, Reasoning: m.Reasoning,
		ToolCalls: tcs, ToolCallID: m.ToolCallID, ToolName: m.ToolName,
		ToolError: m.ToolError, Images: toImageDTOs(m.Images),
	}
}

func toWireEventDTO(e daemon.WireEvent) WireEventDTO {
	return WireEventDTO{
		Kind: e.Kind, Step: e.Step, Text: e.Text, ToolName: e.ToolName,
		ToolID: e.ToolID, ToolArgs: string(e.ToolArgs), Result: e.Result,
		IsError: e.IsError, InTokens: e.InTokens, OutTokens: e.OutTokens,
		Provider: e.Provider, Model: e.Model,
		CacheReadTokens: e.CacheReadTokens, CacheWriteTokens: e.CacheWriteTokens,
	}
}

func toSessionInfoDTO(s daemon.SessionInfo) SessionInfoDTO {
	return SessionInfoDTO{
		ID: s.ID, Title: s.Title, Dir: s.Dir, Model: s.Model,
		Status: string(s.Status), Turns: s.Turns, Views: s.Views, Updated: s.Updated,
	}
}

func toSessionStateDTO(s *daemon.SessionState) *SessionStateDTO {
	msgs := make([]MessageDTO, 0, len(s.Messages))
	for _, m := range s.Messages {
		msgs = append(msgs, toMessageDTO(m))
	}
	return &SessionStateDTO{
		Messages: msgs, Tokens: s.Tokens, Title: s.Title, Model: s.Model,
		Provider: s.Provider, MaxTokens: s.MaxTokens, Perm: s.Perm, Goal: s.Goal,
		Effort: s.Effort, Search: s.Search, Fast: s.Fast, FastOK: s.FastOK,
		Running: s.Running, Tools: s.Tools, Roots: s.Roots, Shells: s.Shells,
		Pending: s.Pending,
	}
}
