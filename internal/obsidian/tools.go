package obsidian

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/tool"
)

// Tools returns the Obsidian note tools (niche-grouped "obsidian"). They no-op
// with a clear error until a vault exists, so registration is always safe.
func Tools() []tool.Definition {
	const group = "obsidian"
	const gist = "the user's Obsidian vault — read, search, and write markdown notes"
	return []tool.Definition{
		{
			Name:        "obsidian_search",
			Description: "Search the user's Obsidian vault. Args: {\"query\":\"text\",\"limit\":15} — matches note titles + content (case-insensitive); blank query lists recent notes. Returns vault-relative paths + titles.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}}}`),
			ReadOnly:    true,
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "notes", CapabilityDesc: "read, search, and write Obsidian notes",
			Run: func(_ context.Context, args json.RawMessage) (string, error) { return runSearch(args) },
		},
		{
			Name:        "obsidian_read",
			Description: "Read an Obsidian note's full markdown. Args: {\"path\":\"folder/Note.md\"} — vault-relative path (from obsidian_search).",
			Parameters:  json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
			ReadOnly:    true,
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "notes", CapabilityDesc: "read, search, and write Obsidian notes",
			Run: func(_ context.Context, args json.RawMessage) (string, error) { return runRead(args) },
		},
		{
			Name:        "obsidian_write",
			Description: "Create or update an Obsidian note. Args: {\"path\":\"Inbox/Idea.md\",\"content\":\"# ...\",\"append\":false} — append:true adds to the end (creating if absent), else overwrites. Use for capturing ideas/notes into the vault.",
			Parameters:  json.RawMessage(`{"type":"object","required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string"},"append":{"type":"boolean"}}}`),
			Niche:       true, Group: group, GroupDesc: gist,
			Capability: "notes", CapabilityDesc: "read, search, and write Obsidian notes",
			Run: func(_ context.Context, args json.RawMessage) (string, error) { return runWrite(args) },
		},
	}
}

func runSearch(args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(args, &in)
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 15
	}
	notes, err := Search(in.Query, in.Limit)
	if err != nil {
		return "", err
	}
	if len(notes) == 0 {
		return "No matching notes.", nil
	}
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "- %s  (%s)\n", n.Title, n.Path)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func runRead(args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	return Read(in.Path)
}

func runWrite(args json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	var (
		rel string
		err error
	)
	if in.Append {
		rel, err = Append(in.Path, in.Content)
	} else {
		rel, err = Write(in.Path, in.Content)
	}
	if err != nil {
		return "", err
	}
	verb := "wrote"
	if in.Append {
		verb = "appended to"
	}
	return fmt.Sprintf("%s note %s", verb, rel), nil
}

// Status is the connector card view for the GUI.
type Status struct {
	Available bool   `json:"available"`
	Vault     string `json:"vault"`
}

// CurrentStatus reports vault availability + path.
func CurrentStatus() Status {
	return Status{Available: Available(), Vault: VaultPath()}
}
