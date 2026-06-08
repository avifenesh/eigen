package transcript

import (
	"encoding/json"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// parsePi reads a Pi agent session JSONL. message lines carry a nested message
// with role user/assistant/toolResult; assistant content blocks (text +
// toolCall) fold into one assistant message.
func parsePi(path string) ([]llm.Message, error) {
	return scanJSONL(path, func(line []byte, out *[]llm.Message) error {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Type      string          `json:"type"`
					Text      string          `json:"text"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"content"`
				ToolCallID string `json:"toolCallId"`
				ToolName   string `json:"toolName"`
				IsError    bool   `json:"isError"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec.Type != "message" {
			return nil
		}
		m := rec.Message
		switch m.Role {
		case "user":
			var t strings.Builder
			for _, b := range m.Content {
				if b.Type == "text" {
					t.WriteString(b.Text)
				}
			}
			if t.Len() > 0 {
				*out = append(*out, llm.Message{Role: llm.RoleUser, Text: t.String()})
			}
		case "assistant":
			asst := llm.Message{Role: llm.RoleAssistant}
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					asst.Text += b.Text
				case "toolCall":
					asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: b.ID, Name: b.Name, Arguments: rawArgs(b.Arguments)})
				}
			}
			if asst.Text != "" || len(asst.ToolCalls) > 0 {
				*out = append(*out, asst)
			}
		case "toolResult":
			var t strings.Builder
			for _, b := range m.Content {
				if b.Type == "text" {
					t.WriteString(b.Text)
				}
			}
			*out = append(*out, llm.Message{Role: llm.RoleTool, ToolCallID: m.ToolCallID, ToolName: m.ToolName, Text: t.String(), ToolError: m.IsError})
		}
		return nil
	})
}
