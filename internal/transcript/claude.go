package transcript

import (
	"encoding/json"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// parseClaude reads a Claude Code session JSONL. Each conversational line wraps
// an Anthropic Messages object; assistant content blocks (text + tool_use) are
// folded into one assistant message, tool_result blocks become tool messages.
func parseClaude(path string) ([]llm.Message, error) {
	return scanJSONL(path, func(line []byte, out *[]llm.Message) error {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec.Type != "user" && rec.Type != "assistant" {
			return nil
		}

		// content is either a plain string or a list of typed blocks.
		var asString string
		if json.Unmarshal(rec.Message.Content, &asString) == nil {
			if asString != "" {
				*out = append(*out, llm.Message{Role: role(rec.Message.Role), Text: asString})
			}
			return nil
		}

		var blocks []struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
			return nil
		}

		asst := llm.Message{Role: llm.RoleAssistant}
		haveAsst := false
		var asstText []string
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if rec.Message.Role == "assistant" {
					asstText = append(asstText, b.Text)
					haveAsst = true
				} else if b.Text != "" {
					*out = append(*out, llm.Message{Role: llm.RoleUser, Text: b.Text})
				}
			case "tool_use":
				asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: b.ID, Name: b.Name, Arguments: rawArgs(b.Input)})
				haveAsst = true
			case "tool_result":
				*out = append(*out, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: b.ToolUseID,
					Text:       claudeResultText(b.Content),
				})
			}
		}
		// Join assistant text blocks with a newline so consecutive blocks stay
		// separated rather than running together ("line one"+"line two").
		asst.Text = strings.Join(asstText, "\n")
		if haveAsst {
			*out = append(*out, asst)
		}
		return nil
	})
}

// claudeResultText flattens a tool_result content (string or list of text/image
// blocks) to plain text. Multiple text blocks are joined with a newline so that
// distinct blocks stay separated rather than running together; non-text blocks
// (e.g. image) carry no text and are skipped.
func claudeResultText(raw json.RawMessage) string {
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
		var parts []string
		for _, blk := range blocks {
			if blk.Type == "text" {
				parts = append(parts, blk.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func role(s string) llm.Role {
	switch s {
	case "user":
		return llm.RoleUser
	case "assistant":
		return llm.RoleAssistant
	case "tool", "toolResult":
		return llm.RoleTool
	default:
		return llm.Role(s)
	}
}
