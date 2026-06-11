package app

import (
	"encoding/json"
	"fmt"
	"os"
)

// Extension enable/disable: flip the "disabled" field on one entry of an
// extension config file (mcp.json servers, plugins.json array, lsp.json
// servers, hooks.json array-or-wrapped), preserving every other field
// verbatim by editing the raw JSON tree rather than round-tripping typed
// structs.

// toggleDisabled flips entry idx (within the file's entry list for that
// kind) and writes the file back. Returns the new disabled state.
func toggleDisabled(path, kind string, idx int) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	entries, container, key, err := entryList(root, kind)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if idx < 0 || idx >= len(entries) {
		return false, fmt.Errorf("entry %d out of range (%d entries)", idx, len(entries))
	}
	entry, ok := entries[idx].(map[string]any)
	if !ok {
		return false, fmt.Errorf("entry %d is not an object", idx)
	}
	disabled, _ := entry["disabled"].(bool)
	if disabled {
		delete(entry, "disabled") // enabled is the absence of the marker
	} else {
		entry["disabled"] = true
	}
	entries[idx] = entry
	if container != nil {
		container[key] = entries
	} else {
		root = entries
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return false, err
	}
	return !disabled, os.Rename(tmp, path)
}

// entryList locates the list of entries for a kind inside the parsed JSON:
// mcp/lsp = {"servers":[…]}, plugins = top-level array, hooks = array or
// {"hooks":[…]}. Returns the list plus (container,key) when nested.
func entryList(root any, kind string) ([]any, map[string]any, string, error) {
	switch kind {
	case "mcp", "lsp":
		obj, ok := root.(map[string]any)
		if !ok {
			return nil, nil, "", fmt.Errorf("expected an object with servers")
		}
		arr, ok := obj["servers"].([]any)
		if !ok {
			return nil, nil, "", fmt.Errorf("no servers array")
		}
		return arr, obj, "servers", nil
	case "plugin":
		arr, ok := root.([]any)
		if !ok {
			return nil, nil, "", fmt.Errorf("expected a top-level array")
		}
		return arr, nil, "", nil
	case "hook":
		if arr, ok := root.([]any); ok {
			return arr, nil, "", nil
		}
		if obj, ok := root.(map[string]any); ok {
			if arr, ok := obj["hooks"].([]any); ok {
				return arr, obj, "hooks", nil
			}
		}
		return nil, nil, "", fmt.Errorf("expected an array or {hooks:[…]}")
	}
	return nil, nil, "", fmt.Errorf("unknown kind %q", kind)
}
