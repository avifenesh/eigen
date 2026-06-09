package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultTreeDepth = 3
	maxTreeEntries   = 500
)

// treeSkip names directories never worth listing.
var treeSkip = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".eigen": true,
	"dist": true, "build": true, "target": true, ".next": true,
	".venv": true, "__pycache__": true,
}

// Tree returns the directory-tree tool: a bounded, indented view of a directory
// (depth-limited, VCS/build dirs skipped). Read-only, so it auto-runs.
func Tree(policy *Policy) Definition {
	return Definition{
		Name:        "tree",
		Description: "Show a directory tree (indented), depth-limited and skipping VCS/build dirs. Use to understand project layout.",
		ReadOnly:    true,
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "Directory to show (default: current directory)." },
    "depth": { "type": "integer", "description": "Max depth to descend (default 3)." }
  },
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path  string `json:"path"`
				Depth int    `json:"depth"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.Path == "" {
				in.Path = "."
			}
			depth := in.Depth
			if depth <= 0 {
				depth = defaultTreeDepth
			}
			root, err := policy.Resolve(in.Path)
			if err != nil {
				return "", err
			}
			return renderTree(root, depth)
		},
	}
}

// renderTree walks root to maxDepth, returning an indented listing.
func renderTree(root string, maxDepth int) (string, error) {
	var b strings.Builder
	b.WriteString(filepath.Base(root) + "/\n")
	count := 0
	truncated := false

	var walk func(dir string, depth int, prefix string) error
	walk = func(dir string, depth int, prefix string) error {
		if depth > maxDepth || truncated {
			return nil
		}
		entries, err := readDirSorted(dir)
		if err != nil {
			return nil // unreadable subdir: skip quietly
		}
		for _, e := range entries {
			if count >= maxTreeEntries {
				truncated = true
				return nil
			}
			name := e.Name()
			if e.IsDir() && treeSkip[name] {
				continue
			}
			if strings.HasPrefix(name, ".") && name != "." {
				continue // skip hidden entries
			}
			count++
			if e.IsDir() {
				b.WriteString(prefix + name + "/\n")
				if err := walk(filepath.Join(dir, name), depth+1, prefix+"  "); err != nil {
					return err
				}
			} else {
				b.WriteString(prefix + name + "\n")
			}
		}
		return nil
	}
	if err := walk(root, 1, "  "); err != nil {
		return "", err
	}
	if truncated {
		b.WriteString(fmt.Sprintf("[truncated at %d entries]\n", maxTreeEntries))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// readDirSorted lists a directory, directories first then files, each alpha.
func readDirSorted(dir string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di // dirs first
		}
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}
