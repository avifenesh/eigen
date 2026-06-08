package transcript

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
	_ "modernc.org/sqlite"
)

// openCodeDBPath resolves the OpenCode SQLite DB path. The keyword "opencode"
// or an empty path maps to the default location.
func openCodeDBPath(path string) string {
	if path == "" || path == "opencode" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	}
	return path
}

// ImportOpenCode imports a conversation from the OpenCode SQLite DB. If
// sessionID is empty, the most recently updated session is used. The DB is
// opened read-only so it is safe to read while OpenCode is running.
func ImportOpenCode(path, sessionID string) ([]llm.Message, error) {
	db, err := sql.Open("sqlite", "file:"+openCodeDBPath(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if sessionID == "" {
		if err := db.QueryRow("SELECT id FROM session ORDER BY time_updated DESC LIMIT 1").Scan(&sessionID); err != nil {
			return nil, fmt.Errorf("no opencode sessions: %w", err)
		}
	}

	// Messages in order; collect (id, role).
	type msgRow struct{ id, role string }
	var msgs []msgRow
	mrows, err := db.Query("SELECT id, data FROM message WHERE session_id=? ORDER BY time_created, id", sessionID)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var id, data string
		if err := mrows.Scan(&id, &data); err != nil {
			mrows.Close()
			return nil, err
		}
		var d struct {
			Role string `json:"role"`
		}
		json.Unmarshal([]byte(data), &d)
		msgs = append(msgs, msgRow{id: id, role: d.Role})
	}
	mrows.Close()

	// All parts for the session, grouped by message (id order preserves
	// within-message order).
	parts := map[string][]ocPart{}
	prows, err := db.Query("SELECT message_id, data FROM part WHERE session_id=? ORDER BY id", sessionID)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var mid, data string
		if err := prows.Scan(&mid, &data); err != nil {
			prows.Close()
			return nil, err
		}
		var p ocPart
		if json.Unmarshal([]byte(data), &p) == nil {
			parts[mid] = append(parts[mid], p)
		}
	}
	prows.Close()

	var out []llm.Message
	for _, m := range msgs {
		if m.role == "user" {
			var t strings.Builder
			for _, p := range parts[m.id] {
				if p.Type == "text" {
					t.WriteString(p.Text)
				}
			}
			if t.Len() > 0 {
				out = append(out, llm.Message{Role: llm.RoleUser, Text: t.String()})
			}
			continue
		}
		// assistant: fold text + tool calls into one message, then tool results
		asst := llm.Message{Role: llm.RoleAssistant}
		var results []llm.Message
		for _, p := range parts[m.id] {
			switch p.Type {
			case "text":
				asst.Text += p.Text
			case "tool":
				asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: p.CallID, Name: p.Tool, Arguments: rawArgs(p.State.Input)})
				res := p.State.Output
				isErr := p.State.Status == "error"
				if res == "" && isErr {
					res = p.State.Error
				}
				results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: p.CallID, ToolName: p.Tool, Text: res, ToolError: isErr})
			}
		}
		if asst.Text != "" || len(asst.ToolCalls) > 0 {
			out = append(out, asst)
		}
		out = append(out, results...)
	}
	return out, nil
}

type ocPart struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Tool   string `json:"tool"`
	CallID string `json:"callID"`
	State  struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output string          `json:"output"`
		Error  string          `json:"error"`
	} `json:"state"`
}
