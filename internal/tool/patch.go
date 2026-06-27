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
		Description: "Apply a patch across one or more files. Prefer edit or multiedit for small, exact replacements — apply_patch needs context that matches the file byte-for-byte (read the file first). Accepts unified diffs (git/diff -u style) and *** Begin Patch / *** Update File agent format. Supports create/delete/rename. All hunks must apply or nothing is written.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "patch": { "type": "string", "description": "The patch text: either a unified diff or *** Begin Patch agent patch." }
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
	anchors  []string
	oldLines []string
	newLines []string
}

func (f filePatch) creating() bool { return f.oldPath == "/dev/null" }
func (f filePatch) deleting() bool { return f.newPath == "/dev/null" }
func (f filePatch) renaming() bool { return !f.creating() && !f.deleting() && f.oldPath != f.newPath }

// parsePatch parses a unified diff into per-file patches. It also accepts the
// Codex/OpenAI-style wrapper (*** Begin Patch / *** Update File / *** End
// Patch) because models frequently emit that syntax even when asked for a raw
// unified diff.
func parsePatch(patch string) ([]filePatch, error) {
	lines := strings.Split(patch, "\n")
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if ln == "*** Begin Patch" {
			return parseAgentPatch(lines)
		}
		break
	}

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

// parseAgentPatch parses the patch envelope used by Codex/OpenAI-style agents:
//
//	*** Begin Patch
//	*** Update File: path
//	@@
//	-old
//	+new
//	*** End Patch
//
// The body is hunk-like: context lines may be prefixed with a space or left
// bare. File actions are lowered into the same filePatch structure used for
// unified diffs, preserving all-or-nothing application semantics.
func parseAgentPatch(lines []string) ([]filePatch, error) {
	var files []filePatch
	var cur *filePatch
	var hunk *patchHunk
	mode := ""

	flushHunk := func() {
		if cur != nil && hunk != nil {
			if len(hunk.oldLines) > 0 || len(hunk.newLines) > 0 {
				cur.hunks = append(cur.hunks, *hunk)
			}
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
	startHunk := func() {
		if cur == nil {
			return
		}
		flushHunk()
		hunk = &patchHunk{oldStart: 1}
	}
	ensureHunk := func() {
		if hunk == nil {
			startHunk()
		}
	}
	appendContext := func(line string) {
		if cur == nil || mode == "delete" {
			return
		}
		ensureHunk()
		hunk.oldLines = append(hunk.oldLines, line)
		hunk.newLines = append(hunk.newLines, line)
	}
	appendOld := func(line string) {
		if cur == nil || mode != "update" {
			return
		}
		ensureHunk()
		hunk.oldLines = append(hunk.oldLines, line)
	}
	appendNew := func(line string) {
		if cur == nil || mode == "delete" {
			return
		}
		ensureHunk()
		hunk.newLines = append(hunk.newLines, line)
	}

	for _, ln := range lines {
		switch {
		case ln == "*** Begin Patch":
			continue
		case ln == "*** End Patch":
			flushFile()
			return files, nil
		case strings.HasPrefix(ln, "*** Update File:"):
			flushFile()
			p := strings.TrimSpace(strings.TrimPrefix(ln, "*** Update File:"))
			cur = &filePatch{oldPath: p, newPath: p}
			mode = "update"
		case strings.HasPrefix(ln, "*** Add File:"):
			flushFile()
			p := strings.TrimSpace(strings.TrimPrefix(ln, "*** Add File:"))
			cur = &filePatch{oldPath: "/dev/null", newPath: p}
			mode = "add"
			startHunk()
		case strings.HasPrefix(ln, "*** Delete File:"):
			flushFile()
			p := strings.TrimSpace(strings.TrimPrefix(ln, "*** Delete File:"))
			cur = &filePatch{oldPath: p, newPath: "/dev/null"}
			mode = "delete"
		case strings.HasPrefix(ln, "*** Move to:") || strings.HasPrefix(ln, "*** Rename to:"):
			if cur == nil || mode != "update" {
				return nil, fmt.Errorf("move/rename outside update file section")
			}
			if strings.HasPrefix(ln, "*** Move to:") {
				cur.newPath = strings.TrimSpace(strings.TrimPrefix(ln, "*** Move to:"))
			} else {
				cur.newPath = strings.TrimSpace(strings.TrimPrefix(ln, "*** Rename to:"))
			}
		case strings.HasPrefix(ln, "***"):
			return nil, fmt.Errorf("unsupported agent patch directive %q", ln)
		case strings.HasPrefix(ln, "@@"):
			if cur == nil {
				return nil, fmt.Errorf("hunk before any file header")
			}
			if hunk == nil {
				startHunk()
			}
			if n, err := parseHunkStart(ln); err == nil {
				hunk.oldStart = n
			}
			if anchor := strings.TrimSpace(strings.Trim(ln, "@")); anchor != "" && !strings.HasPrefix(anchor, "-") {
				hunk.anchors = append(hunk.anchors, anchor)
			}
		case strings.HasPrefix(ln, "\\"):
			// "\ No newline at end of file" — ignore.
		case ln == "" && cur == nil:
			continue
		case len(ln) > 0 && ln[0] == '+':
			appendNew(ln[1:])
		case len(ln) > 0 && ln[0] == '-':
			appendOld(ln[1:])
		case len(ln) > 0 && ln[0] == ' ':
			appendContext(ln[1:])
		default:
			// Agent-patch context lines are commonly bare (unlike unified diffs).
			appendContext(ln)
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
			if _, err := os.Stat(resolved); err != nil {
				return "", fmt.Errorf("apply_patch: %s: %w", target, err)
			}
			results = append(results, result{path: resolved, delete: true})
		case f.creating():
			if _, err := os.Stat(resolved); err == nil {
				return "", fmt.Errorf("apply_patch: %s already exists", target)
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("apply_patch: %s: %w", target, err)
			}
			var added []string
			for _, h := range f.hunks {
				added = append(added, h.newLines...)
			}
			results = append(results, result{path: resolved, content: strings.Join(added, "\n") + "\n"})
		default:
			// For a rename, read from the source (oldPath); the destination
			// (resolved = newPath) does not exist yet. The trailing os.Remove of
			// the old path below finishes the move.
			source := resolved
			if f.renaming() {
				source, err = policy.Resolve(f.oldPath)
				if err != nil {
					return "", err
				}
			}
			data, err := os.ReadFile(source)
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
	for _, f := range files {
		if !f.creating() && !f.deleting() && f.oldPath != f.newPath {
			oldResolved, err := policy.Resolve(f.oldPath)
			if err != nil {
				return "", err
			}
			newResolved, err := policy.Resolve(f.newPath)
			if err != nil {
				return "", err
			}
			if oldResolved != newResolved {
				_ = os.Remove(oldResolved)
			}
		}
	}
	return fmt.Sprintf("applied patch to %d file(s)", len(results)), nil
}

// applyHunks applies hunks to content, locating each by context match.
func applyHunks(content string, hunks []patchHunk) (string, error) {
	lines := strings.Split(content, "\n")
	for hi, h := range hunks {
		at := findHunk(lines, h)
		if at < 0 {
			return "", hunkApplyError(hi+1, h, lines)
		}
		tail := append([]string{}, lines[at+len(h.oldLines):]...)
		lines = append(lines[:at], append(append([]string{}, h.newLines...), tail...)...)
	}
	return strings.Join(lines, "\n"), nil
}

func findHunk(lines []string, h patchHunk) int {
	matches := findBlockMatches(lines, h.oldLines)
	if len(matches) == 0 {
		if len(h.oldLines) == 0 {
			return clampIndex(h.oldStart-1, len(lines))
		}
		return -1
	}
	hint := h.oldStart - 1
	if len(h.anchors) == 0 {
		for _, at := range matches {
			if at == hint {
				return at
			}
		}
		return matches[0]
	}
	best, bestScore := -1, -1
	for _, at := range matches {
		score := anchorScore(lines, h.anchors, at)
		if score > bestScore || (score == bestScore && closerToHint(at, best, hint)) {
			best, bestScore = at, score
		}
	}
	return best
}

func anchorScore(lines, anchors []string, at int) int {
	if len(anchors) == 0 {
		return 0
	}
	score := 0
	last := -1
	for _, anchor := range anchors {
		found := -1
		for i := 0; i <= at && i < len(lines); i++ {
			if lines[i] == anchor {
				found = i
			}
		}
		if found >= 0 {
			score++
			last = found
		}
	}
	if last >= 0 {
		// Prefer matches closer to the deepest anchor, but keep the primary score
		// as the number of anchors found.
		score = score*100000 - (at - last)
	}
	return score
}

func closerToHint(a, b, hint int) bool {
	if b < 0 {
		return true
	}
	if hint < 0 {
		return a < b
	}
	ad := a - hint
	if ad < 0 {
		ad = -ad
	}
	bd := b - hint
	if bd < 0 {
		bd = -bd
	}
	if ad == bd {
		return a < b
	}
	return ad < bd
}

func clampIndex(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}

func findBlockMatches(lines, old []string) []int {
	if len(old) == 0 {
		return []int{0}
	}
	var out []int
	for at := 0; at+len(old) <= len(lines); at++ {
		if blockMatchesAt(lines, old, at) {
			out = append(out, at)
		}
	}
	if len(out) > 0 {
		return out
	}
	// Second pass: tolerate trailing whitespace / CRLF drift (common when models
	// copy context from read output or Windows-checked-out files).
	for at := 0; at+len(old) <= len(lines); at++ {
		if blockMatchesAtRelaxed(lines, old, at) {
			out = append(out, at)
		}
	}
	return out
}

func blockMatchesAt(lines, old []string, at int) bool {
	for i := range old {
		if lines[at+i] != old[i] {
			return false
		}
	}
	return true
}

func blockMatchesAtRelaxed(lines, old []string, at int) bool {
	for i := range old {
		if normalizePatchLine(lines[at+i]) != normalizePatchLine(old[i]) {
			return false
		}
	}
	return true
}

func normalizePatchLine(s string) string {
	s = strings.TrimRight(s, "\r")
	return strings.TrimRight(s, " \t")
}

func hunkApplyError(n int, h patchHunk, lines []string) error {
	const maxShow = 3
	var b strings.Builder
	fmt.Fprintf(&b, "hunk %d does not apply (context not found)", n)
	if len(h.oldLines) > 0 {
		b.WriteString("; expected first line")
		if len(h.oldLines) > 1 {
			fmt.Fprintf(&b, "s (%d lines)", len(h.oldLines))
		}
		b.WriteString(" like:\n")
		for i := 0; i < len(h.oldLines) && i < maxShow; i++ {
			fmt.Fprintf(&b, "  | %s\n", h.oldLines[i])
		}
		if len(h.oldLines) > maxShow {
			b.WriteString("  | …\n")
		}
	}
	if hint := nearestPatchContext(lines, h.oldLines); hint != "" {
		b.WriteString("nearest file match:\n")
		b.WriteString(hint)
	}
	b.WriteString("tip: read the file and use edit/multiedit with exact old_string, or refresh patch context")
	return fmt.Errorf("%s", strings.TrimSpace(b.String()))
}

// nearestPatchContext finds a window in lines whose first oldLine appears and
// shows how the file differs (for agent-facing errors).
func nearestPatchContext(lines, old []string) string {
	if len(old) == 0 || len(lines) == 0 {
		return ""
	}
	want := normalizePatchLine(old[0])
	if want == "" {
		return ""
	}
	for i, ln := range lines {
		if normalizePatchLine(ln) != want {
			continue
		}
		var b strings.Builder
		end := i + len(old)
		if end > len(lines) {
			end = len(lines)
		}
		for j := i; j < end && j-i < 4; j++ {
			mark := " "
			if j-i < len(old) && normalizePatchLine(lines[j]) != normalizePatchLine(old[j-i]) {
				mark = "!"
			}
			fmt.Fprintf(&b, "  %s| %s\n", mark, lines[j])
		}
		return b.String()
	}
	return ""
}
