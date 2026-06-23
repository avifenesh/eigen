package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// Preview is the cheap, parse-free metadata for a session: the working
// directory it ran in (for project grouping), a title derived from the first
// user message, and an approximate message count. It reads only enough of the
// file to fill these — never the whole transcript.
type Preview struct {
	Cwd      string
	Title    string
	Messages int
}

// peekMaxBytes caps how much of a transcript Peek scans for the cwd + first
// user message. The cwd and first user turn are near the top of every format,
// so a small window suffices; the message COUNT is the file's line count
// (counted cheaply by a byte scan, not JSON parsing).
const peekMaxBytes = 256 << 10 // 256KB

// Peek extracts a Preview for a session without a full parse. src selects the
// format; origin is the file path (or, for OpenCode, the session id — which
// has no cheap peek, so it returns an empty Preview).
func Peek(src Source, origin string) Preview {
	switch src {
	case SourceClaude:
		return peekClaude(origin)
	case SourceCodex:
		return peekCodex(origin)
	case SourcePi:
		return peekPi(origin)
	case SourceHermes:
		return peekHermes(origin)
	case SourceEigen, "":
		return peekEigen(origin)
	}
	return Preview{}
}

// scanPeek reads a transcript once: it keeps the head lines (up to peekMaxBytes)
// for title/cwd extraction, and counts conversational TURNS over the WHOLE file
// using a per-source line classifier (mechanical — turns have a known structure,
// no model needed). countTurn returns true for a line that is one user or
// assistant message.
func scanPeek(path string, countTurn func(line []byte) bool) (lines []string, turns int) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	read := 0
	for sc.Scan() {
		b := sc.Bytes()
		if countTurn != nil && countTurn(b) {
			turns++
		}
		if read < peekMaxBytes {
			ln := string(b)
			read += len(ln) + 1
			lines = append(lines, ln)
		}
	}
	return lines, turns
}

// titleFrom turns a user message into a concise title, rejecting injected
// context (AGENTS.md/instructions, command output, XML/JSON blobs) so the title
// reflects the human's actual ask, not boilerplate the harness prepended.
func titleFrom(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Reject obvious non-asks: injected instructions, tags, structured blobs.
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "<"), strings.HasPrefix(s, "{"), strings.HasPrefix(s, "["):
		return ""
	case strings.HasPrefix(s, "#") && strings.Contains(low, "agents.md"):
		return ""
	case strings.HasPrefix(low, "<user_instructions"), strings.HasPrefix(low, "<environment"):
		return ""
	case strings.Contains(low, "instructions for") && strings.HasPrefix(s, "#"):
		return ""
	case strings.HasPrefix(s, "caveat:"), strings.HasPrefix(s, "[request interrupted"):
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace
	r := []rune(s)
	if len(r) > 72 {
		return strings.TrimSpace(string(r[:72])) + "…"
	}
	return s
}

func peekClaude(path string) Preview {
	lines, total := scanPeek(path, claudeTurn)
	p := Preview{Messages: total}
	// Fallback cwd from the folder name: -home-user-proj → /home/user/proj.
	p.Cwd = claudeDirFromPath(path)
	for _, ln := range lines {
		var rec struct {
			Type    string `json:"type"`
			Cwd     string `json:"cwd"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(ln), &rec) != nil {
			continue
		}
		if rec.Cwd != "" && p.Cwd == "" {
			p.Cwd = rec.Cwd
		} else if rec.Cwd != "" {
			p.Cwd = rec.Cwd // prefer the real cwd over the folder guess
		}
		if p.Title == "" && (rec.Type == "user" || rec.Message.Role == "user") {
			p.Title = titleFrom(claudeText(rec.Message.Content, rec.Content))
		}
		if p.Title != "" && p.Cwd != "" {
			break
		}
	}
	return p
}

// claudeText pulls plain text from a Claude content field (string or blocks).
func claudeText(blocks json.RawMessage, plain string) string {
	if plain != "" {
		return plain
	}
	var s string
	if json.Unmarshal(blocks, &s) == nil {
		return s
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(blocks, &arr) == nil {
		for _, b := range arr {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

// claudeDirFromPath recovers the cwd from Claude's project-folder name. Claude
// encodes the cwd by replacing every non-alphanumeric byte ('/', '.', '_', and
// even a literal '-') with a single '-', so the encoding is LOSSY: a naive
// ReplaceAll('-','/') turns "/home/u/.claude/action-graph" (stored as
// "-home-u--claude-action-graph") into "/home/u//claude/action/graph", grouping
// sessions under projects that do not exist.
//
// Since the original separators cannot be recovered from the string alone, we
// resolve against the real filesystem: walk the segments left-to-right, and at
// each '-' boundary prefer descending into a directory ('/'), falling back to
// re-joining the segment onto the previous component with '.', '_', or '-' when
// that is what exists on disk. If the name resolves to no existing directory we
// return "" rather than emit a wrong path — an empty Cwd is better than a
// phantom project. (The caller still prefers the real cwd recorded in the
// transcript body over this fallback.)
func claudeDirFromPath(path string) string {
	dir := filepath.Base(filepath.Dir(path))
	if !strings.HasPrefix(dir, "-") {
		return ""
	}
	return resolveClaudeDir(dir)
}

// resolveClaudeDir reconstructs an absolute path from a '-'-encoded Claude
// folder name by matching it against the filesystem. Because every '/', '.',
// '_', and literal '-' was flattened to a single '-', each '-' is ambiguous: it
// may be a path separator (a new component) or a char inside the current
// component. We resolve by DFS over the real directory tree — consume one or
// more segments to form the next existing child directory, descend, and recurse
// on the rest. The shortest child name is tried first (the common case where
// '-' is a separator); a fused name (e.g. ".claude" or "agent-sh") is only
// assumed when that directory actually exists. Returns "" if no reconstruction
// reaches an existing directory at the end, so a wrong guess never groups
// sessions under a phantom project.
func resolveClaudeDir(encoded string) string {
	segs := strings.Split(strings.TrimPrefix(encoded, "-"), "-")
	return resolveSegs("/", segs)
}

// claudeSep is the set of non-'/' separators Claude collapses to '-' inside a
// single name segment. The '/' (new component) case is handled separately and
// tried first, so the common multi-directory layout wins over a fused name.
var claudeSep = []string{".", "_", "-"}

// resolveSegs descends from base by consuming segs[0:] into the next existing
// directory component (one segment, or several fused by a separator), then
// recursing on the remainder. It returns the full path once segs is exhausted
// at an existing directory, or "" if no branch resolves.
func resolveSegs(base string, segs []string) string {
	if len(segs) == 0 {
		if dirExists(base) {
			return base
		}
		return ""
	}
	// Component = segs[0], optionally extended by fusing later segments with a
	// non-'/' separator. Try the shortest (descend on one segment) first.
	component := segs[0]
	if dirExists(filepath.Join(base, component)) {
		if cand := resolveSegs(filepath.Join(base, component), segs[1:]); cand != "" {
			return cand
		}
	}
	for i := 1; i < len(segs); i++ {
		for _, sep := range claudeSep {
			fused := component + sep + segs[i]
			if !dirExists(filepath.Join(base, fused)) {
				continue
			}
			if cand := resolveSegs(filepath.Join(base, fused), segs[i+1:]); cand != "" {
				return cand
			}
		}
		// Carry a literal-'-' fusion forward so a longer name (e.g. "agent-sh")
		// can still match a deeper component on the next iteration.
		component += "-" + segs[i]
	}
	return ""
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func peekCodex(path string) Preview {
	lines, total := scanPeek(path, codexTurn)
	p := Preview{Messages: total}
	for _, ln := range lines {
		var rec struct {
			Type    string `json:"type"`
			Payload struct {
				Cwd     string          `json:"cwd"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Text    string          `json:"text"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(ln), &rec) != nil {
			continue
		}
		if rec.Type == "session_meta" && rec.Payload.Cwd != "" {
			p.Cwd = rec.Payload.Cwd
		}
		if p.Title == "" && rec.Payload.Role == "user" {
			p.Title = titleFrom(codexText(rec.Payload.Content, rec.Payload.Text))
		}
		if p.Title != "" && p.Cwd != "" {
			break
		}
	}
	return p
}

func codexText(blocks json.RawMessage, plain string) string {
	if plain != "" {
		return plain
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(blocks, &arr) == nil {
		for _, b := range arr {
			if b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

func peekPi(path string) Preview {
	lines, total := scanPeek(path, piTurn)
	p := Preview{Messages: total}
	for _, ln := range lines {
		var rec struct {
			Type    string `json:"type"`
			Cwd     string `json:"cwd"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(ln), &rec) != nil {
			continue
		}
		if rec.Cwd != "" {
			p.Cwd = rec.Cwd
		}
		if p.Title == "" && rec.Message.Role == "user" {
			p.Title = titleFrom(claudeText(rec.Message.Content, ""))
		}
		if p.Title != "" && p.Cwd != "" {
			break
		}
	}
	return p
}

func peekHermes(path string) Preview {
	_, total := scanPeek(path, hermesTurn)
	return Preview{Messages: total}
}

func peekEigen(path string) Preview {
	lines, total := scanPeek(path, eigenTurn)
	p := Preview{Messages: total}
	// eigen records the cwd + an optional user-set title in the meta sidecar;
	// prefer them.
	if m, ok := LoadMeta(path); ok {
		if m.Dir != "" {
			p.Cwd = m.Dir
		}
		if m.Title != "" {
			p.Title = m.Title
		}
	}
	for _, ln := range lines {
		// eigen JSONL is a marshaled llm.Message (capitalized Role/Text).
		var msg llm.Message
		if json.Unmarshal([]byte(ln), &msg) != nil {
			continue
		}
		if msg.Role == llm.RoleUser && msg.Text != "" {
			if p.Title == "" { // a user-set title from the sidecar wins
				p.Title = titleFrom(msg.Text)
			}
			break
		}
	}
	return p
}

// --- turn classifiers: is this line one conversational (user/assistant) turn?
// Mechanical, per-source — turns have a known structure, no model needed.

func claudeTurn(line []byte) bool {
	var r struct {
		Type    string `json:"type"`
		Message struct {
			Role string `json:"role"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	role := r.Message.Role
	if role == "" {
		role = r.Type
	}
	return role == "user" || role == "assistant"
}

func codexTurn(line []byte) bool {
	var r struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Role string `json:"role"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	return r.Type == "response_item" && r.Payload.Type == "message" &&
		(r.Payload.Role == "user" || r.Payload.Role == "assistant")
}

func piTurn(line []byte) bool {
	var r struct {
		Type    string `json:"type"`
		Message struct {
			Role string `json:"role"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	return r.Type == "message" && (r.Message.Role == "user" || r.Message.Role == "assistant")
}

func hermesTurn(line []byte) bool {
	var r struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	return r.Role == "user" || r.Role == "assistant"
}

func eigenTurn(line []byte) bool {
	var m llm.Message
	if json.Unmarshal(line, &m) != nil {
		return false
	}
	return m.Role == llm.RoleUser || m.Role == llm.RoleAssistant
}
