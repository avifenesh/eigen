package transcript

import (
	"encoding/json"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// parseCodex reads a Codex rollout JSONL. The conversation lives in
// response_item lines (raw Responses-API items); assistant text and following
// function_call items are grouped into one assistant message, and *_output
// items become tool messages. Reasoning/event/meta lines are ignored.
func parseCodex(path string) ([]llm.Message, error) {
	var out []llm.Message
	asst := llm.Message{Role: llm.RoleAssistant}
	haveAsst := false
	flush := func() {
		if haveAsst {
			out = append(out, asst)
			asst = llm.Message{Role: llm.RoleAssistant}
			haveAsst = false
		}
	}

	msgs, err := scanJSONL(path, func(line []byte, _ *[]llm.Message) error {
		var rec struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Input     string `json:"input"`
				CallID    string `json:"call_id"`
				Output    string `json:"output"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec.Type != "response_item" {
			return nil
		}
		p := rec.Payload
		switch p.Type {
		case "message":
			var text strings.Builder
			for _, c := range p.Content {
				text.WriteString(c.Text)
			}
			switch p.Role {
			case "assistant":
				asst.Text += text.String()
				haveAsst = true
			case "user":
				flush()
				if text.Len() > 0 {
					out = append(out, llm.Message{Role: llm.RoleUser, Text: text.String()})
				}
			default: // developer/system
				flush()
			}
		case "function_call":
			asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: p.CallID, Name: p.Name, Arguments: rawArgsString(p.Arguments)})
			haveAsst = true
		case "custom_tool_call":
			asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: p.CallID, Name: p.Name, Arguments: rawArgsString(p.Input)})
			haveAsst = true
		case "function_call_output", "custom_tool_call_output":
			flush()
			out = append(out, llm.Message{Role: llm.RoleTool, ToolCallID: p.CallID, Text: p.Output})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = msgs // scanJSONL's accumulator is unused; we build `out` directly
	flush()
	return out, nil
}

// rawArgsString turns a tool argument string into a valid JSON object: used
// as-is if it is already JSON, otherwise wrapped as {"input": <string>}.
func rawArgsString(s string) json.RawMessage {
	t := strings.TrimSpace(s)
	if t == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(t)) {
		return json.RawMessage(t)
	}
	b, _ := json.Marshal(map[string]string{"input": s})
	return b
}
