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
		Description: "Record, inspect, or relocate Eigen memory. action=\"add\" records a durable note for future sessions via Codex-shaped ad-hoc notes; action=\"list\"/\"read\"/\"search\" inspects the memory workspace when the injected summary is not enough; action=\"move\" relocates a fact between scopes — PROMOTE a project fact that's actually cross-cutting to global, DEMOTE a global note to a project, or move a fact from one project to ANOTHER (to=another project's name/key). scope=\"project\" (default) is this repo; scope=\"global\" applies across projects. Set kind=\"ban\" with a short title to record a HARD prohibition.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
	    "action": { "type": "string", "enum": ["add", "list", "read", "search", "move"], "description": "add (default) records memory; list/read/search inspect workspace files; move relocates a note between scopes." },
	    "note": { "type": "string", "description": "The fact to remember/move, or (kind=ban) the rule: what must never be done and why." },
	    "scope": { "type": "string", "enum": ["project", "global"], "description": "Where to store it (add), or the SOURCE scope (move): \"project\" (this repo, default) or \"global\"." },
	    "to": { "type": "string", "description": "For action=move: the DESTINATION scope — \"global\", \"project\", or ANOTHER project's name/on-disk key (e.g. \"eigen\"). Must differ from the source." },
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

// moveNote relocates a fact between scopes. "project"/"" and "global" resolve to
// the session's own stores; ANY other value is treated as another PROJECT — by
// on-disk store key (e.g. "eigen-3e739af1") or readable name (e.g. "eigen",
// matched against memory.ListProjectStores) — so a fact can move project→project,
// not only project↔global. The session's stores must be the concrete
// *memory.Store (the move primitive lives there).
func moveNote(project, global MemoryStore, from, to, note string) (string, error) {
	if strings.TrimSpace(note) == "" {
		return "", fmt.Errorf("a note is required for move")
	}
	if from == "" {
		from = "project"
	}
	if to == "" {
		return "", fmt.Errorf("move needs a destination scope (\"project\", \"global\", or another project's name/key)")
	}
	if from == to {
		return "", fmt.Errorf("source and destination scope are the same (%q)", from)
	}
	src, err := pickMoveScope(from, project, global)
	if err != nil {
		return "", err
	}
	dst, err := pickMoveScope(to, project, global)
	if err != nil {
		return "", err
	}
	if memory.StoreKey(src) == memory.StoreKey(dst) {
		return "", fmt.Errorf("source and destination resolve to the same scope")
	}
	if err := memory.MoveNote(src, dst, note); err != nil {
		return "", err
	}
	return fmt.Sprintf("moved note from %s to %s memory — the source copy is superseded and drops on the next consolidation", from, to), nil
}

// pickMoveScope resolves a move scope identifier to a concrete *memory.Store:
// "project"/"" → the session project, "global" → the session global, anything
// else → another project opened by on-disk key or readable name.
func pickMoveScope(scope string, project, global MemoryStore) (*memory.Store, error) {
	switch scope {
	case "project", "":
		s, ok := project.(*memory.Store)
		if !ok || s == nil {
			return nil, fmt.Errorf("project memory scope unavailable for move")
		}
		return s, nil
	case "global":
		s, ok := global.(*memory.Store)
		if !ok || s == nil {
			return nil, fmt.Errorf("global memory scope unavailable for move")
		}
		return s, nil
	}
	// Another project: try the exact on-disk key first, then a readable name.
	if s, err := memory.OpenByKey(scope); err == nil {
		return s, nil
	}
	refs, err := memory.ListProjectStores()
	if err != nil {
		return nil, fmt.Errorf("resolve project %q: %w", scope, err)
	}
	for _, r := range refs {
		if r.Name == scope || r.Key == scope {
			return memory.OpenByKey(r.Key)
		}
	}
	return nil, fmt.Errorf("unknown project scope %q (use its name or on-disk key; see memory action=list)", scope)
}
