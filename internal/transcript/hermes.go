package transcript

import (
	"encoding/json"

	"github.com/avifenesh/eigen/internal/llm"
)

// parseHermes reads a Hermes session JSONL: flat chat-completions messages, one
// per line keyed by role (user/assistant/tool); assistant tool_calls fold into
// the assistant message.
func parseHermes(path string) ([]llm.Message, error) {
	return scanJSONL(path, func(line []byte, out *[]llm.Message) error {
		var rec struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		switch rec.Role {
		case "user":
			if rec.Content != "" {
				*out = append(*out, llm.Message{Role: llm.RoleUser, Text: rec.Content})
			}
		case "assistant":
			asst := llm.Message{Role: llm.RoleAssistant, Text: rec.Content}
			for _, tc := range rec.ToolCalls {
				asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: rawArgsString(tc.Function.Arguments)})
			}
			if asst.Text != "" || len(asst.ToolCalls) > 0 {
				*out = append(*out, asst)
			}
		case "tool":
			*out = append(*out, llm.Message{Role: llm.RoleTool, ToolCallID: rec.ToolCallID, Text: rec.Content})
		}
		return nil
	})
}
