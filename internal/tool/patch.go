package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Patch returns the apply_patch tool: apply a unified diff spanning one or more
// files. Hunks are located by matching their context (not just line numbers),
// so small drift is tolerated. It is all-or-nothing: if any hunk fails to
// apply, no file is changed. Mutating — gated mode requires approval.
func Patch(policy *Policy) Definition {
	return Definition{
		Name:        "apply_patch",
		Description: "Apply a unified diff (git/diff -u style) across one or more files. Supports creating (--- /dev/null) and deleting (+++ /dev/null) files. All hunks must apply or nothing is written.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "patch": { "type": "string", "description": "The unified diff text." }
  },
  "required": ["patch"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Patch string `json:"patch"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			files, err := parsePatch(in.Patch)
			if err != nil {
				return "", err
			}
			if len(files) == 0 {
				return "", fmt.Errorf("no file sections found in patch")
			}
			return applyPatch(policy, files)
		},
	}
}

// filePatch is the parsed diff for a single file.
type filePatch struct {
	oldPath string
	newPath string
	hunks   []patchHunk
}

// patchHunk is one @@ block: the lines it expects (context+removed) and the
// lines it produces (context+added), plus the 1-based old start for a hint.
type patchHunk struct {
	oldStart int
	oldLines []string
	newLines []string
}

func (f filePatch) creating() bool { return f.oldPath == "/dev/null" }
func (f filePatch) deleting() bool { return f.newPath == "/dev/null" }

// parsePatch parses a unified diff into per-file patches.
func parsePatch(patch string) ([]filePatch, error) {
	lines := strings.Split(patch, "\n")
	var files []filePatch
	var cur *filePatch
	var hunk *patchHunk

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.hunks = append(cur.hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		switch {
		case strings.HasPrefix(ln, "--- "):
			flushFile()
			cur = &filePatch{oldPath: patchPath(ln[4:])}
			// The next line should be the +++ header.
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ") {
				cur.newPath = patchPath(lines[i+1][4:])
				i++
			}
		case strings.HasPrefix(ln, "@@"):
			if cur == nil {
				return nil, fmt.Errorf("hunk before any file header")
			}
			flushHunk()
			start, err := parseHunkStart(ln)
			if err != nil {
				return nil, err
			}
			hunk = &patchHunk{oldStart: start}
		case hunk != nil && len(ln) > 0 && ln[0] == ' ':
			hunk.oldLines = append(hunk.oldLines, ln[1:])
			hunk.newLines = append(hunk.newLines, ln[1:])
		case hunk != nil && len(ln) > 0 && ln[0] == '-':
			hunk.oldLines = append(hunk.oldLines, ln[1:])
		case hunk != nil && len(ln) > 0 && ln[0] == '+':
			hunk.newLines = append(hunk.newLines, ln[1:])
		case strings.HasPrefix(ln, "\\"):
			// "\ No newline at end of file" — ignore.
		default:
			// diff/index/other metadata or a trailing blank: end any open hunk.
			flushHunk()
		}
	}
	flushFile()
	return files, nil
}

// patchPath strips a/ or b/ prefixes and any trailing tab-timestamp.
func patchPath(s string) string {
	s = strings.TrimSpace(s)
	if tab := strings.IndexByte(s, '\t'); tab >= 0 {
		s = s[:tab]
	}
	if s == "/dev/null" {
		return s
	}
	for _, p := range []string{"a/", "b/"} {
		if strings.HasPrefix(s, p) {
			return s[len(p):]
		}
	}
	return s
}

// parseHunkStart reads the old-file start line from "@@ -l,s +l,s @@".
func parseHunkStart(ln string) (int, error) {
	open := strings.Index(ln, "-")
	if open < 0 {
		return 0, fmt.Errorf("bad hunk header %q", ln)
	}
	rest := ln[open+1:]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		return 0, fmt.Errorf("bad hunk header %q", ln)
	}
	var n int
	if _, err := fmt.Sscanf(rest[:end], "%d", &n); err != nil {
		return 0, fmt.Errorf("bad hunk header %q", ln)
	}
	return n, nil
}

// applyPatch computes every file's new content first; only if all hunks apply
// does it write (so a partial patch never corrupts the tree).
func applyPatch(policy *Policy, files []filePatch) (string, error) {
	type result struct {
		path    string
		content string
		delete  bool
	}
	var results []result

	for _, f := range files {
		target := f.newPath
		if f.deleting() {
			target = f.oldPath
		}
		resolved, err := policy.Resolve(target)
		if err != nil {
			return "", err
		}
		switch {
		case f.deleting():
			results = append(results, result{path: resolved, delete: true})
		case f.creating():
			var added []string
			for _, h := range f.hunks {
				added = append(added, h.newLines...)
			}
			results = append(results, result{path: resolved, content: strings.Join(added, "\n") + "\n"})
		default:
			data, err := os.ReadFile(resolved)
			if err != nil {
				return "", fmt.Errorf("apply_patch: %s: %w", target, err)
			}
			updated, err := applyHunks(string(data), f.hunks)
			if err != nil {
				return "", fmt.Errorf("apply_patch: %s: %w", target, err)
			}
			results = append(results, result{path: resolved, content: updated})
		}
	}

	for _, r := range results {
		if r.delete {
			if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
				return "", err
			}
			continue
		}
		if err := atomicWrite(r.path, []byte(r.content)); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("applied patch to %d file(s)", len(results)), nil
}

// applyHunks applies hunks to content, locating each by context match.
func applyHunks(content string, hunks []patchHunk) (string, error) {
	lines := strings.Split(content, "\n")
	for hi, h := range hunks {
		at := findBlock(lines, h.oldLines, h.oldStart-1)
		if at < 0 {
			return "", fmt.Errorf("hunk %d does not apply (context not found)", hi+1)
		}
		tail := append([]string{}, lines[at+len(h.oldLines):]...)
		lines = append(lines[:at], append(append([]string{}, h.newLines...), tail...)...)
	}
	return strings.Join(lines, "\n"), nil
}

// findBlock returns the index where old occurs contiguously in lines, preferring
// the hint position; -1 if not found. An empty old block inserts at the hint.
func findBlock(lines, old []string, hint int) int {
	if len(old) == 0 {
		if hint < 0 {
			return 0
		}
		if hint > len(lines) {
			return len(lines)
		}
		return hint
	}
	match := func(at int) bool {
		if at < 0 || at+len(old) > len(lines) {
			return false
		}
		for i := range old {
			if lines[at+i] != old[i] {
				return false
			}
		}
		return true
	}
	if match(hint) {
		return hint
	}
	for at := 0; at+len(old) <= len(lines); at++ {
		if match(at) {
			return at
		}
	}
	return -1
}
