package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/memory"
)

// MemoryStore is the minimal write view of a project memory the memory tool
// needs. Optional list/read/search interfaces are discovered by type assertion.
type MemoryStore interface {
	Append(note string) error
	AddBan(title, rule string) (bool, error)
}

type memoryLister interface {
	ListFiles() ([]string, error)
}

type memoryReader interface {
	ReadRelative(path string) (string, error)
}

type memorySearcher interface {
	Search(query string, limit int) ([]memory.SearchHit, error)
}

// Memory returns the memory tool. The agent records a durable note for future
// sessions, choosing the scope: "project" (default — facts about THIS repo:
// build/test commands, architecture, gotchas) or "global" (cross-project facts:
// the user's working style, durable preferences, and rules that apply
// everywhere). It writes only to eigen's own memory store (not the user's
// project), so it is read-only with respect to the project and auto-runs.
// global may be nil when no global store is available (then any scope writes to
// project).
func Memory(project, global MemoryStore) Definition {
	return Definition{
		Name:        "memory",
		Description: "Record, inspect, or relocate Eigen memory. action=\"add\" records a durable note for future sessions via Codex-shaped ad-hoc notes; action=\"list\"/\"read\"/\"search\" inspects the memory workspace when the injected summary is not enough; action=\"move\" relocates a fact between scopes (PROMOTE a project fact that's actually cross-cutting to global, or DEMOTE a global note that only applies here to project). scope=\"project\" (default) is this repo; scope=\"global\" applies across projects. Set kind=\"ban\" with a short title to record a HARD prohibition.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
	    "action": { "type": "string", "enum": ["add", "list", "read", "search", "move"], "description": "add (default) records memory; list/read/search inspect workspace files; move relocates a note between scopes." },
	    "note": { "type": "string", "description": "The fact to remember/move, or (kind=ban) the rule: what must never be done and why." },
	    "scope": { "type": "string", "enum": ["project", "global"], "description": "Where to store it (add), or the SOURCE scope (move): \"project\" (this repo, default) or \"global\"." },
	    "to": { "type": "string", "enum": ["project", "global"], "description": "For action=move: the DESTINATION scope; must differ from scope." },
	    "kind": { "type": "string", "enum": ["note", "ban"], "description": "\"note\" (default) = a durable fact; \"ban\" = a hard prohibition (needs a title)." },
	    "title": { "type": "string", "description": "Short title for a ban (e.g. \"No hedging\"). Required when kind=ban." },
	    "path": { "type": "string", "description": "Relative memory workspace path for action=read." },
	    "query": { "type": "string", "description": "Search query for action=search." },
	    "limit": { "type": "integer", "description": "Maximum search hits; default 20." }
	  },
	  "additionalProperties": false
	}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Action string `json:"action"`
				Note   string `json:"note"`
				Scope  string `json:"scope"`
				To     string `json:"to"`
				Kind   string `json:"kind"`
				Title  string `json:"title"`
				Path   string `json:"path"`
				Query  string `json:"query"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			store := project
			where := "project"
			if in.Scope == "global" && global != nil {
				store = global
				where = "global"
			}
			if store == nil {
				return "", fmt.Errorf("no memory store available")
			}
			if in.Action == "" {
				in.Action = "add"
			}
			switch in.Action {
			case "move":
				return moveNote(project, global, in.Scope, in.To, in.Note)
			case "list":
				l, ok := store.(memoryLister)
				if !ok {
					return "", fmt.Errorf("memory store cannot list files")
				}
				files, err := l.ListFiles()
				if err != nil {
					return "", err
				}
				if len(files) == 0 {
					return "no memory files", nil
				}
				return strings.Join(files, "\n"), nil
			case "read":
				r, ok := store.(memoryReader)
				if !ok {
					return "", fmt.Errorf("memory store cannot read files")
				}
				if in.Path == "" {
					return "", fmt.Errorf("path is required for action=read")
				}
				return r.ReadRelative(in.Path)
			case "search":
				s, ok := store.(memorySearcher)
				if !ok {
					return "", fmt.Errorf("memory store cannot search files")
				}
				hits, err := s.Search(in.Query, in.Limit)
				if err != nil {
					return "", err
				}
				if len(hits) == 0 {
					return "no matches", nil
				}
				var out string
				for _, h := range hits {
					out += h.Path + ": " + h.Line + "\n"
				}
				return out, nil
			case "add":
			default:
				return "", fmt.Errorf("unsupported memory action %q", in.Action)
			}
			if in.Kind == "ban" {
				if in.Title == "" {
					return "", fmt.Errorf("a ban needs a title")
				}
				replaced, err := store.AddBan(in.Title, in.Note)
				if err != nil {
					return "", err
				}
				verb := "recorded"
				if replaced {
					verb = "updated"
				}
				return fmt.Sprintf("%s banned behavior %q (%s) — enforced as a hard rule in future sessions", verb, in.Title, where), nil
			}
			if strings.TrimSpace(in.Note) == "" {
				return "", fmt.Errorf("a note is required")
			}
			if err := store.Append(in.Note); err != nil {
				return "", err
			}
			return "noted for future sessions (" + where + " memory)", nil
		},
	}
}

// moveNote relocates a fact between the project and global scopes. Both stores
// must be the concrete *memory.Store (the move primitive lives there); a plugin
// store that only satisfies MemoryStore can't be moved between.
func moveNote(project, global MemoryStore, from, to, note string) (string, error) {
	if strings.TrimSpace(note) == "" {
		return "", fmt.Errorf("a note is required for move")
	}
	if from == "" {
		from = "project"
	}
	if to == "" {
		return "", fmt.Errorf("move needs a destination scope (to=\"project\" or \"global\")")
	}
	if from == to {
		return "", fmt.Errorf("source and destination scope are the same (%q)", from)
	}
	pick := func(scope string) (*memory.Store, error) {
		var ms MemoryStore
		if scope == "global" {
			ms = global
		} else {
			ms = project
		}
		s, ok := ms.(*memory.Store)
		if !ok || s == nil {
			return nil, fmt.Errorf("%s memory scope unavailable for move", scope)
		}
		return s, nil
	}
	src, err := pick(from)
	if err != nil {
		return "", err
	}
	dst, err := pick(to)
	if err != nil {
		return "", err
	}
	if err := memory.MoveNote(src, dst, note); err != nil {
		return "", err
	}
	return fmt.Sprintf("moved note from %s to %s memory — the source copy is superseded and drops on the next consolidation", from, to), nil
}
