package transcript

import (
	"encoding/json"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// parseCodex reads a Codex rollout JSONL. The conversation lives in
// response_item lines (raw Responses-API items); assistant text, reasoning, and
// following function_call items are grouped into one assistant message, and
// *_output items become tool messages. event/meta lines are ignored.
//
// Reasoning items carry summary text plus an encrypted_content blob bound to a
// specific item id. Codex runs store:false, so resuming a session MUST carry the
// blob back paired with its id or the server 404s on the reasoning item id. We
// fold a turn's reasoning into its assistant message (mirroring applyOutputItem
// in internal/llm/codex.go: last id+blob from the same item, summaries joined).
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
				Name      string          `json:"name"`
				Arguments string          `json:"arguments"`
				Input     string          `json:"input"`
				CallID    string          `json:"call_id"`
				Output    json.RawMessage `json:"output"`
				ID        string          `json:"id"`
				Summary   []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"summary"`
				Encrypted string `json:"encrypted_content"`
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
		case "reasoning":
			// Pair ReasoningID with the blob from the SAME item — the server
			// verifies they match (Encrypted content item_id mismatch = 400). A
			// turn can emit several reasoning items; keep the LAST one carrying a
			// blob, taking both id and blob from it together. Summaries from all
			// items are joined. Mirrors applyOutputItem in internal/llm/codex.go.
			if p.Encrypted != "" {
				asst.ReasoningEncrypted = p.Encrypted
				asst.ReasoningID = p.ID
			} else if asst.ReasoningID == "" {
				asst.ReasoningID = p.ID
			}
			for _, s := range p.Summary {
				if s.Text != "" {
					if asst.Reasoning != "" {
						asst.Reasoning += "\n"
					}
					asst.Reasoning += s.Text
				}
			}
			haveAsst = true
		case "function_call":
			asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: p.CallID, Name: p.Name, Arguments: rawArgsString(p.Arguments)})
			haveAsst = true
		case "custom_tool_call":
			asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: p.CallID, Name: p.Name, Arguments: rawArgsString(p.Input)})
			haveAsst = true
		case "function_call_output", "custom_tool_call_output":
			flush()
			out = append(out, llm.Message{Role: llm.RoleTool, ToolCallID: p.CallID, Text: codexOutputText(p.Output)})
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

// codexOutputText flattens a function_call_output's output (a JSON string or a
// list of Responses-API content blocks) to plain text. When the output is an
// array of {type,text} blocks the text fields are concatenated; non-text blocks
// (e.g. input_image) carry no text and are skipped. Mirrors claudeResultText.
func codexOutputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			b.WriteString(blk.Text)
		}
		return b.String()
	}
	return ""
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
