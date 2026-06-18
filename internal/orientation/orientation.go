// Package orientation is Eigen's native history/provenance harness. It indexes
// local Eigen transcripts into a small JSON graph under ~/.eigen/orientation and
// answers provenance/related-work questions without a Node/skill dependency.
package orientation

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const projectKeyVersion = "repo-worktree-v1"

var eigenEvents = []string{"turn_done", "session_stop", "note"}

type Paths struct {
	Home      string
	Data      string
	Allowlist string
	Hooks     string
}

func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".eigen", "orientation")
	return Paths{Home: base, Data: filepath.Join(base, "data"), Allowlist: filepath.Join(base, "projects.txt"), Hooks: filepath.Join(home, ".eigen", "hooks.json")}
}

func EnsureHome() error {
	p := DefaultPaths()
	if err := os.MkdirAll(p.Home, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(p.Home, 0o700)
	if _, err := os.Stat(p.Allowlist); os.IsNotExist(err) {
		const sample = "# Eigen orientation allowlist — one cwd PREFIX per line.\n# Empty/missing allowlist means hooks may index local Eigen sessions.\n# /home/you/projects\n"
		if err := os.WriteFile(p.Allowlist, []byte(sample), 0o600); err != nil {
			return err
		}
	} else {
		_ = os.Chmod(p.Allowlist, 0o600)
	}
	if err := os.MkdirAll(p.Data, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(p.Data, 0o700)
	return nil
}

type Identity struct {
	ProjectKey        string `json:"projectKey"`
	ProjectKeyVersion string `json:"projectKeyVersion"`
	LegacyProjectKey  string `json:"legacyProjectKey"`
	RepoKey           string `json:"repoKey"`
	RepoRoot          string `json:"repoRoot,omitempty"`
	GitRemote         string `json:"gitRemote,omitempty"`
	WorktreeCwd       string `json:"worktreeCwd,omitempty"`
	HeadSha           string `json:"headSha,omitempty"`
	CurrentBranch     string `json:"currentBranch,omitempty"`
}

type Manifest struct {
	Identity
	Cwd              string `json:"cwd,omitempty"`
	Records          int    `json:"records,omitempty"`
	LastCursorIngest string `json:"lastCursorIngest,omitempty"`
	LastFullRefresh  string `json:"lastFullRefresh,omitempty"`
	CursorIngest     bool   `json:"cursorIngest,omitempty"`
}

type Run struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

type Evidence struct {
	Source       string `json:"source,omitempty"`
	SourceLine   int    `json:"sourceLine,omitempty"`
	Runtime      string `json:"runtime,omitempty"`
	Session      string `json:"session,omitempty"`
	Kind         string `json:"kind,omitempty"`
	SourceOffset int64  `json:"sourceOffset,omitempty"`
}

type Episode struct {
	T                 string     `json:"t,omitempty"`
	Session           string     `json:"session,omitempty"`
	Runtime           string     `json:"runtime,omitempty"`
	Adapter           string     `json:"adapter,omitempty"`
	SourceKind        string     `json:"sourceKind,omitempty"`
	ID                string     `json:"id,omitempty"`
	GitBranch         string     `json:"gitBranch,omitempty"`
	GitBranchSource   string     `json:"gitBranchSource,omitempty"`
	ProjectKey        string     `json:"projectKey,omitempty"`
	ProjectKeyVersion string     `json:"projectKeyVersion,omitempty"`
	LegacyProjectKey  string     `json:"legacyProjectKey,omitempty"`
	RepoKey           string     `json:"repoKey,omitempty"`
	RepoRoot          string     `json:"repoRoot,omitempty"`
	GitRemote         string     `json:"gitRemote,omitempty"`
	WorktreeCwd       string     `json:"worktreeCwd,omitempty"`
	HeadSha           string     `json:"headSha,omitempty"`
	Intent            string     `json:"intent,omitempty"`
	Prose             string     `json:"prose,omitempty"`
	FilesTouched      []string   `json:"filesTouched,omitempty"`
	Runs              []Run      `json:"runs,omitempty"`
	Evidence          []Evidence `json:"evidence,omitempty"`
}

type EpisodesFile struct {
	Updated  string    `json:"updated"`
	Episodes []Episode `json:"episodes"`
}

type Graph struct {
	Updated string      `json:"updated"`
	Nodes   []GraphNode `json:"nodes"`
	Edges   []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label string `json:"label"`
	Count int    `json:"count,omitempty"`
}

type GraphEdge struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Rel    string   `json:"rel"`
	Weight int      `json:"weight,omitempty"`
	Via    []string `json:"via,omitempty"`
}

type transcriptRow struct {
	Role       string          `json:"Role"`
	Role2      string          `json:"role"`
	Text       string          `json:"Text"`
	Text2      string          `json:"text"`
	ToolCalls  []toolCall      `json:"ToolCalls"`
	ToolCallID string          `json:"ToolCallID"`
	ToolName   string          `json:"ToolName"`
	ToolError  bool            `json:"ToolError"`
	Timestamp  string          `json:"Timestamp"`
	Timestamp2 string          `json:"timestamp"`
	Raw        json.RawMessage `json:"-"`
}

type toolCall struct {
	ID        string          `json:"ID"`
	ID2       string          `json:"id"`
	Name      string          `json:"Name"`
	Name2     string          `json:"name"`
	Arguments json.RawMessage `json:"Arguments"`
	Args2     json.RawMessage `json:"arguments"`
}

func (r transcriptRow) role() string {
	if r.Role != "" {
		return r.Role
	}
	return r.Role2
}
func (r transcriptRow) text() string {
	if r.Text != "" {
		return r.Text
	}
	return r.Text2
}
func (r transcriptRow) timestamp() string {
	if r.Timestamp != "" {
		return r.Timestamp
	}
	return r.Timestamp2
}
func (c toolCall) id() string {
	if c.ID != "" {
		return c.ID
	}
	return c.ID2
}
func (c toolCall) name() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Name2
}
func (c toolCall) args() json.RawMessage {
	if len(c.Arguments) > 0 {
		return c.Arguments
	}
	return c.Args2
}

func shortHash(s string, n int) string {
	if s == "" {
		s = "unknown"
	}
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:n]
}

func inspectProject(cwd string) Identity {
	repoRoot := gitOut(cwd, "rev-parse", "--show-toplevel")
	remote := gitOut(cwd, "config", "--get", "remote.origin.url")
	head := gitOut(cwd, "rev-parse", "HEAD")
	branch := gitOut(cwd, "branch", "--show-current")
	repoID := remote
	if repoID == "" {
		repoID = repoRoot
	}
	if repoID == "" {
		repoID = cwd
	}
	repoKey := shortHash(repoID, 12)
	legacy := shortHash(cwd, 12)
	proj := shortHash(projectKeyVersion+"\x00"+repoKey+"\x00"+cwd, 12)
	return Identity{ProjectKey: proj, ProjectKeyVersion: projectKeyVersion, LegacyProjectKey: legacy, RepoKey: repoKey, RepoRoot: repoRoot, GitRemote: remote, WorktreeCwd: cwd, HeadSha: head, CurrentBranch: branch}
}

func projectKeyCandidates(cwd string) []string {
	id := inspectProject(cwd)
	if id.LegacyProjectKey == id.ProjectKey {
		return []string{id.ProjectKey}
	}
	return []string{id.ProjectKey, id.LegacyProjectKey}
}

func gitOut(cwd string, args ...string) string {
	if cwd == "" {
		return ""
	}
	// Avoid importing os/exec into callers by keeping git best-effort and tiny.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := commandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// commandContext is a var for tests.
var commandContext = defaultCommandContext

type execCmd interface{ Output() ([]byte, error) }

type osExecCmd struct {
	name string
	args []string
	ctx  context.Context
}

func defaultCommandContext(ctx context.Context, name string, args ...string) execCmd {
	return osExecCmd{name: name, args: args, ctx: ctx}
}
func (c osExecCmd) Output() ([]byte, error) { return runOutput(c.ctx, c.name, c.args...) }

func runOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	// Small indirection keeps tests simple while avoiding a package-level exec import
	// in generated docs. The implementation lives below.
	return osExecOutput(ctx, name, args...)
}

// osExecOutput is defined in exec.go.

func ReadAllowlist() []string {
	p := DefaultPaths().Allowlist
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(string(b), "\n") {
		s := strings.TrimSpace(ln)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, s)
	}
	return out
}

func Allowlisted(cwd string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return false
	}
	for _, p := range prefixes {
		p = filepath.Clean(p)
		c := filepath.Clean(cwd)
		if c == p || strings.HasPrefix(c, p+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func projectDir(cwd string) string {
	return filepath.Join(DefaultPaths().Data, inspectProject(cwd).ProjectKey)
}

func exactProjectDirs(cwd string) []string {
	paths := DefaultPaths()
	seen := map[string]bool{}
	var out []string
	for _, k := range projectKeyCandidates(cwd) {
		d := filepath.Join(paths.Data, k)
		if seen[d] {
			continue
		}
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, d)
			seen[d] = true
		}
	}
	return out
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func writeJSONAtomic(path string, v any, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func loadEpisodesForCWD(cwd string) ([]Episode, []string) {
	var eps []Episode
	var projects []string
	for _, dir := range exactProjectDirs(cwd) {
		var f EpisodesFile
		if readJSON(filepath.Join(dir, "episodes.json"), &f) {
			eps = append(eps, f.Episodes...)
			projects = append(projects, filepath.Base(dir))
		}
	}
	return eps, projects
}

func loadGraphForCWD(cwd string) *Graph {
	for _, dir := range exactProjectDirs(cwd) {
		var g Graph
		if readJSON(filepath.Join(dir, "graph.json"), &g) {
			return &g
		}
	}
	return nil
}

func readTranscript(path string) ([]transcriptRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []transcriptRow
	s := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	s.Buffer(buf, 32*1024*1024)
	for s.Scan() {
		line := append([]byte(nil), s.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var r transcriptRow
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		r.Raw = line
		rows = append(rows, r)
	}
	return rows, s.Err()
}

func IngestSource(source, cwd, runtime, sourceKind, branch, branchSource string, allowUnlisted bool) error {
	if source == "" {
		return errors.New("source is required")
	}
	if cwd == "" {
		cwd = inferCWDFromSource(source)
	}
	if cwd == "" {
		return nil
	}
	prefixes := ReadAllowlist()
	if !allowUnlisted && len(prefixes) > 0 && !Allowlisted(cwd, prefixes) {
		return nil
	}
	rows, err := readTranscript(source)
	if err != nil {
		return err
	}
	id := inspectProject(cwd)
	eps := episodesFromRows(rows, source, cwd, runtime, sourceKind, branch, branchSource, id)
	return mergeProjectEpisodes(cwd, source, id, eps)
}

func episodesFromRows(rows []transcriptRow, source, cwd, runtime, sourceKind, branch, branchSource string, id Identity) []Episode {
	if runtime == "" {
		runtime = "eigen"
	}
	if sourceKind == "" {
		sourceKind = "session"
	}
	session := sessionIDFromSource(source)
	var eps []Episode
	var cur *Episode
	finish := func() {
		if cur == nil || strings.TrimSpace(cur.Intent) == "" {
			cur = nil
			return
		}
		cur.FilesTouched = sortedUnique(cur.FilesTouched)
		cur.ID = shortHash(cur.Session+"\x00"+cur.T+"\x00"+cur.Intent+"\x00"+strings.Join(cur.FilesTouched, ","), 10)
		eps = append(eps, *cur)
		cur = nil
	}
	for i, row := range rows {
		role := row.role()
		t := row.timestamp()
		if t == "" {
			t = time.Now().UTC().Format(time.RFC3339)
		}
		switch role {
		case "user":
			intent := cleanText(row.text(), 300)
			if intent == "" {
				continue
			}
			finish()
			cur = &Episode{T: t, Session: session, Runtime: runtime, Adapter: "eigen-" + sourceKind, SourceKind: sourceKind, GitBranch: branch, GitBranchSource: branchSource, ProjectKey: id.ProjectKey, ProjectKeyVersion: id.ProjectKeyVersion, LegacyProjectKey: id.LegacyProjectKey, RepoKey: id.RepoKey, RepoRoot: id.RepoRoot, GitRemote: id.GitRemote, WorktreeCwd: cwd, HeadSha: id.HeadSha, Intent: intent, Evidence: []Evidence{{Source: source, SourceLine: i + 1, Runtime: runtime, Session: session, Kind: "intent"}}}
		case "assistant":
			if cur == nil {
				continue
			}
			if cur.Prose == "" {
				cur.Prose = cleanText(row.text(), 220)
			}
			for _, call := range row.ToolCalls {
				tool := baseToolName(call.name())
				args := parseArgs(call.args())
				for _, f := range filesFromTool(tool, args, cwd, id.RepoRoot) {
					cur.FilesTouched = append(cur.FilesTouched, f)
				}
				if run := runFromTool(tool, args); run != nil {
					cur.Runs = append(cur.Runs, *run)
				}
			}
		}
	}
	finish()
	return eps
}

func mergeProjectEpisodes(cwd, source string, id Identity, fresh []Episode) error {
	paths := DefaultPaths()
	dir := filepath.Join(paths.Data, id.ProjectKey)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var existing EpisodesFile
	_ = readJSON(filepath.Join(dir, "episodes.json"), &existing)
	byID := map[string]Episode{}
	for _, e := range existing.Episodes {
		if e.ID != "" {
			byID[e.ID] = e
		}
	}
	for _, e := range fresh {
		if e.ID != "" {
			byID[e.ID] = e
		}
	}
	merged := make([]Episode, 0, len(byID))
	for _, e := range byID {
		merged = append(merged, e)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].T < merged[j].T })
	now := time.Now().UTC().Format(time.RFC3339)
	mf := Manifest{Identity: id, Cwd: cwd, Records: len(merged), LastCursorIngest: now, CursorIngest: true}
	if err := writeJSONAtomic(filepath.Join(dir, ".manifest.json"), mf, 0o600); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(dir, "episodes.json"), EpisodesFile{Updated: now, Episodes: merged}, 0o600); err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(dir, "graph.json"), BuildGraph(merged), 0o600)
}

func parseArgs(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	if len(raw) == 0 {
		return m
	}
	_ = json.Unmarshal(raw, &m)
	return m
}

func argString(args map[string]any, names ...string) string {
	for _, n := range names {
		if v, ok := args[n]; ok {
			switch x := v.(type) {
			case string:
				return x
			case fmt.Stringer:
				return x.String()
			}
		}
	}
	return ""
}

func baseToolName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func filesFromTool(tool string, args map[string]any, cwd, repoRoot string) []string {
	var out []string
	add := func(p string) {
		if f := canonicalFile(p, cwd, repoRoot); f != "" {
			out = append(out, f)
		}
	}
	switch tool {
	case "write", "read", "edit", "multiedit", "glob":
		add(argString(args, "path", "file"))
	case "move":
		add(argString(args, "from"))
		add(argString(args, "to"))
	case "apply_patch":
		for _, f := range filesFromPatch(argString(args, "patch")) {
			add(f)
		}
	}
	return out
}

var patchFileRe = regexp.MustCompile(`(?m)^(?:\*\*\* (?:Update|Add|Delete) File:|--- |\+\+\+ )\s+([^\n\r]+)`)

func filesFromPatch(patch string) []string {
	var out []string
	for _, m := range patchFileRe.FindAllStringSubmatch(patch, -1) {
		p := strings.TrimSpace(m[1])
		p = strings.TrimPrefix(strings.TrimPrefix(p, "a/"), "b/")
		if p != "/dev/null" {
			out = append(out, p)
		}
	}
	return out
}

func runFromTool(tool string, args map[string]any) *Run {
	if tool != "bash" {
		return nil
	}
	cmd := argString(args, "command")
	low := strings.ToLower(cmd)
	if strings.Contains(low, "git commit") {
		return &Run{Kind: "commit", Text: cleanText(cmd, 160)}
	}
	if strings.Contains(low, "go test") || strings.Contains(low, "make gate") || strings.Contains(low, "cargo test") {
		return &Run{Kind: "test", Text: cleanText(cmd, 160)}
	}
	return nil
}

func canonicalFile(p, cwd, repoRoot string) string {
	p = strings.TrimSpace(p)
	if p == "" || strings.Contains(p, "*") {
		return ""
	}
	if filepath.IsAbs(p) {
		base := repoRoot
		if base == "" {
			base = cwd
		}
		if rel, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
	} else if cwd != "" {
		// keep relative paths as-is; they are usually project-relative in tool args.
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func cleanText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func sortedUnique(xs []string) []string {
	m := map[string]bool{}
	for _, x := range xs {
		if x != "" {
			m[x] = true
		}
	}
	out := make([]string, 0, len(m))
	for x := range m {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

func inferCWDFromSource(source string) string {
	base := strings.TrimSuffix(source, ".jsonl")
	for _, p := range []string{base + ".meta.json", strings.TrimSuffix(base, ".transcript") + ".meta.json"} {
		var meta struct {
			Dir string `json:"dir"`
			Cwd string `json:"cwd"`
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		_ = json.Unmarshal(b, &meta)
		if meta.Dir != "" {
			return meta.Dir
		}
		if meta.Cwd != "" {
			return meta.Cwd
		}
	}
	return ""
}

func sessionIDFromSource(source string) string {
	b := filepath.Base(source)
	b = strings.TrimSuffix(b, ".transcript.jsonl")
	b = strings.TrimSuffix(b, ".jsonl")
	if len(b) > 8 {
		return b[:8]
	}
	return b
}

func FindEigenSource(session string, task bool) (source, cwd, sourceKind string) {
	home, _ := os.UserHomeDir()
	var roots []string
	if task {
		matches, _ := filepath.Glob(filepath.Join(home, ".eigen", "tasks*"))
		roots = matches
		sourceKind = "task"
	} else {
		roots = append(roots, filepath.Join(home, ".eigen", "sessions"), filepath.Join(home, ".eigen", "daemon", "sessions"), filepath.Join(home, ".eigen", "daemon-dev", "sessions"))
		more, _ := filepath.Glob(filepath.Join(home, ".eigen", "daemon-*", "sessions"))
		roots = append(roots, more...)
		sourceKind = "session"
	}
	type cand struct {
		p  string
		mt time.Time
	}
	var cs []cand
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := filepath.Base(p)
			if task && !strings.HasSuffix(name, ".transcript.jsonl") {
				return nil
			}
			if !task && (!strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".meta.json")) {
				return nil
			}
			if session != "" && !strings.HasPrefix(name, session) && !strings.Contains(name, session) {
				return nil
			}
			if st, err := os.Stat(p); err == nil {
				cs = append(cs, cand{p, st.ModTime()})
			}
			return nil
		})
	}
	if len(cs) == 0 {
		return "", "", sourceKind
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].mt.After(cs[j].mt) })
	source = cs[0].p
	cwd = inferCWDFromSource(source)
	return source, cwd, sourceKind
}

func BuildGraph(eps []Episode) Graph {
	nodes := map[string]*GraphNode{}
	couple := map[string]int{}
	addNode := func(id, typ, label string) {
		n := nodes[id]
		if n == nil {
			n = &GraphNode{ID: id, Type: typ, Label: label}
			nodes[id] = n
		}
		n.Count++
	}
	var edges []GraphEdge
	for i, ep := range eps {
		for _, f := range ep.FilesTouched {
			addNode("file:"+f, "file", f)
		}
		if ep.Intent != "" {
			iid := "intent:" + strconv.Itoa(i)
			addNode(iid, "intent", ep.Intent)
			for _, f := range ep.FilesTouched {
				edges = append(edges, GraphEdge{From: iid, To: "file:" + f, Rel: "touched"})
			}
			out := "explored"
			for _, r := range ep.Runs {
				if r.Kind == "commit" {
					out = "committed"
					break
				}
			}
			edges = append(edges, GraphEdge{From: iid, To: "outcome:" + out, Rel: "outcome"})
		}
		for a := 0; a < len(ep.FilesTouched); a++ {
			for b := a + 1; b < len(ep.FilesTouched); b++ {
				x, y := ep.FilesTouched[a], ep.FilesTouched[b]
				if x > y {
					x, y = y, x
				}
				couple[x+"\x00"+y]++
			}
		}
	}
	for k, w := range couple {
		parts := strings.Split(k, "\x00")
		edges = append(edges, GraphEdge{From: "file:" + parts[0], To: "file:" + parts[1], Rel: "coupled", Weight: w})
	}
	outNodes := make([]GraphNode, 0, len(nodes))
	for _, n := range nodes {
		outNodes = append(outNodes, *n)
	}
	sort.Slice(outNodes, func(i, j int) bool { return outNodes[i].ID < outNodes[j].ID })
	return Graph{Updated: time.Now().UTC().Format(time.RFC3339), Nodes: outNodes, Edges: edges}
}

func makeMatcher(file string, known map[string]bool) func(string) bool {
	file = filepath.ToSlash(filepath.Clean(file))
	parts := strings.Split(file, "/")
	base := file
	if len(parts) >= 2 {
		base = strings.Join(parts[len(parts)-2:], "/")
	}
	bare := parts[len(parts)-1]
	exact := known[base]
	return func(f string) bool {
		f = filepath.ToSlash(f)
		if exact {
			return f == base
		}
		return f == file || f == base || f == bare || strings.HasSuffix(f, "/"+bare)
	}
}

func ms(t string) int64 {
	if tt, err := time.Parse(time.RFC3339, t); err == nil {
		return tt.UnixMilli()
	}
	return 0
}
func committed(e Episode) bool {
	for _, r := range e.Runs {
		if r.Kind == "commit" {
			return true
		}
	}
	return false
}

func fmtAge(delta int64) string {
	if delta < 0 {
		delta = 0
	}
	h := float64(delta) / 3600e3
	if h < 1 {
		m := int(h*60 + 0.5)
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%dmin ago", m)
	}
	if h < 48 {
		return fmt.Sprintf("%dh ago", int(h+0.5))
	}
	return fmt.Sprintf("%dd ago", int(h/24+0.5))
}

func Provenance(w io.Writer, cwd, file string) error {
	eps, _ := loadEpisodesForCWD(cwd)
	if len(eps) == 0 {
		fmt.Fprintln(w, "(no orientation history for this project — cannot judge; do not assume cruft)")
		return nil
	}
	known := map[string]bool{}
	for _, e := range eps {
		for _, f := range e.FilesTouched {
			known[f] = true
		}
	}
	match := makeMatcher(file, known)
	var goals []Episode
	var now, last int64
	for _, e := range eps {
		if t := ms(e.T); t > now {
			now = t
		}
		for _, f := range e.FilesTouched {
			if match(f) && e.Intent != "" {
				goals = append(goals, e)
				if t := ms(e.T); t > last {
					last = t
				}
				break
			}
		}
	}
	if len(goals) == 0 {
		fmt.Fprintf(w, "No recorded work touched %q. Orientation has no provenance — either pre-dates tracking or genuinely untouched. Judge on code alone.\n", file)
		return nil
	}
	comm := 0
	for _, e := range goals {
		if committed(e) {
			comm++
		}
	}
	fmt.Fprintf(w, "PROVENANCE: %s\n", file)
	fmt.Fprintf(w, "%d goal(s) touched it — %d committed, %d not committed. Last touched %s (relative to latest project activity).\n\n", len(goals), comm, len(goals)-comm, fmtAge(now-last))
	if now-last <= int64(48*time.Hour/time.Millisecond) {
		fmt.Fprintln(w, "⚠ ACTIVELY IN FLIGHT — recent work touched this file. Do not treat as dead/stale; continue or ask, don't scrap.")
	} else if comm > 0 {
		fmt.Fprintln(w, "⚠ DELIBERATE WORK — committed and not recently active. Built on purpose; do not delete as cruft without cause.")
	} else {
		fmt.Fprintln(w, "POSSIBLY STALE (uncertain) — verify with the user / real signals before treating as dead.")
	}
	fmt.Fprintln(w, "\nGoals that touched it (newest first):")
	sort.Slice(goals, func(i, j int) bool { return ms(goals[i].T) > ms(goals[j].T) })
	for i, e := range goals {
		if i >= 6 {
			break
		}
		mark := "·uncommitted"
		if committed(e) {
			mark = "✓committed"
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, cleanText(e.Intent, 76))
	}
	for _, line := range coupledLines(eps, match, 6) {
		fmt.Fprintln(w, line)
	}
	return nil
}

func Related(w io.Writer, cwd, file string) error {
	eps, _ := loadEpisodesForCWD(cwd)
	if len(eps) == 0 {
		fmt.Fprintln(w, "(no orientation history for this project — no prior-work signal; proceed, but search the code yourself)")
		return nil
	}
	known := map[string]bool{}
	for _, e := range eps {
		for _, f := range e.FilesTouched {
			known[f] = true
		}
	}
	match := makeMatcher(file, known)
	var goals []Episode
	for _, e := range eps {
		for _, f := range e.FilesTouched {
			if match(f) && e.Intent != "" {
				goals = append(goals, e)
				break
			}
		}
	}
	coupled := coupledPairs(eps, match)
	if len(goals) == 0 && len(coupled) == 0 {
		fmt.Fprintf(w, "No prior work recorded on %q or files coupled to it. Likely new ground — but still grep the codebase for similar logic before adding.\n", file)
		return nil
	}
	fmt.Fprintf(w, "RELATED PRIOR WORK: %s\n\n", file)
	if len(goals) > 0 {
		fmt.Fprintf(w, "%d goal(s) already built in this file — read before adding:\n", len(goals))
		sort.Slice(goals, func(i, j int) bool { return boolScore(committed(goals[j])) < boolScore(committed(goals[i])) })
		for i, e := range goals {
			if i >= 6 {
				break
			}
			mark := "·"
			if committed(e) {
				mark = "✓"
			}
			fmt.Fprintf(w, "  %s %s\n", mark, cleanText(e.Intent, 78))
		}
		fmt.Fprintln(w)
	}
	if len(coupled) > 0 {
		fmt.Fprintln(w, "Sibling files (edited alongside this one — similar/related logic likely lives here):")
		for i, p := range coupled {
			if i >= 6 {
				break
			}
			fmt.Fprintf(w, "  %dx  %s\n", p.Weight, p.File)
		}
		fmt.Fprintln(w, "\nGrep these + this file for the function/behavior you intend to add before writing it.")
	}
	return nil
}
func boolScore(b bool) int {
	if b {
		return 1
	}
	return 0
}

type fileWeight struct {
	File   string
	Weight int
}

func coupledPairs(eps []Episode, match func(string) bool) []fileWeight {
	m := map[string]int{}
	for _, e := range eps {
		hit := false
		for _, f := range e.FilesTouched {
			if match(f) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, f := range e.FilesTouched {
			if !match(f) {
				m[f]++
			}
		}
	}
	out := make([]fileWeight, 0, len(m))
	for f, w := range m {
		out = append(out, fileWeight{f, w})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight == out[j].Weight {
			return out[i].File < out[j].File
		}
		return out[i].Weight > out[j].Weight
	})
	return out
}
func coupledLines(eps []Episode, match func(string) bool, n int) []string {
	pairs := coupledPairs(eps, match)
	if len(pairs) == 0 {
		return nil
	}
	lines := []string{"", "Coupled neighbors (usually edited together — check before removing):"}
	for i, p := range pairs {
		if i >= n {
			break
		}
		lines = append(lines, fmt.Sprintf("  %dx  %s", p.Weight, p.File))
	}
	return lines
}

func Query(w io.Writer, cwd, q string) error {
	eps, _ := loadEpisodesForCWD(cwd)
	if len(eps) == 0 {
		fmt.Fprintln(w, "(no orientation history for this project yet)")
		return nil
	}
	q = strings.ToLower(q)
	var hits []Episode
	for _, e := range eps {
		if q == "" || strings.Contains(strings.ToLower(e.Intent+" "+e.Prose+" "+strings.Join(e.FilesTouched, " ")), q) {
			hits = append(hits, e)
		}
	}
	if len(hits) == 0 {
		fmt.Fprintf(w, "(no episodes match %q)\n", q)
		return nil
	}
	fmt.Fprintf(w, "%d episode(s) match %q:\n\n", len(hits), q)
	for i := len(hits) - 1; i >= 0; i-- {
		e := hits[i]
		fmt.Fprintf(w, "[%s] %s\n", firstN(e.T, 10), e.Intent)
		if len(e.FilesTouched) > 0 {
			fmt.Fprintf(w, "    files: %s\n", strings.Join(e.FilesTouched, ", "))
		}
	}
	return nil
}
func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func Coupled(w io.Writer, cwd, file string) error {
	eps, _ := loadEpisodesForCWD(cwd)
	known := map[string]bool{}
	for _, e := range eps {
		for _, f := range e.FilesTouched {
			known[f] = true
		}
	}
	match := makeMatcher(file, known)
	pairs := coupledPairs(eps, match)
	if len(pairs) == 0 {
		fmt.Fprintln(w, "(no coupled files recorded)")
		return nil
	}
	for _, p := range pairs {
		fmt.Fprintf(w, "%dx  %s\n", p.Weight, p.File)
	}
	return nil
}

func Threads(w io.Writer, cwd string) error {
	eps, _ := loadEpisodesForCWD(cwd)
	if len(eps) == 0 {
		fmt.Fprintln(w, "(no orientation history for this project yet)")
		return nil
	}
	for i, e := range eps {
		if i >= 20 {
			break
		}
		fmt.Fprintf(w, "[%s] %s\n", firstN(e.T, 10), e.Intent)
		if len(e.FilesTouched) > 0 {
			fmt.Fprintf(w, "    files: %s\n", strings.Join(e.FilesTouched, ", "))
		}
	}
	return nil
}

func Status(w io.Writer, cwd string) error {
	paths := DefaultPaths()
	id := inspectProject(cwd)
	prefixes := ReadAllowlist()
	eps, keys := loadEpisodesForCWD(cwd)
	fmt.Fprint(w, "orientation status\n\n")
	kv := func(k string, v any) { fmt.Fprintf(w, "%-18s %v\n", k, v) }
	kv("cwd", cwd)
	kv("state home", paths.Home)
	if len(prefixes) == 0 {
		kv("allowlisted", "no allowlist")
	} else if Allowlisted(cwd, prefixes) {
		kv("allowlisted", "yes")
	} else {
		kv("allowlisted", "no")
	}
	kv("project key", id.ProjectKey)
	if id.LegacyProjectKey != id.ProjectKey {
		kv("legacy key", id.LegacyProjectKey)
	}
	kv("repo key", id.RepoKey)
	if id.RepoRoot != "" {
		kv("repo root", id.RepoRoot)
	}
	if id.CurrentBranch != "" {
		kv("branch now", id.CurrentBranch+" (snapshot only)")
	}
	kv("indexed", map[bool]string{true: "yes", false: "no"}[len(eps) > 0])
	if len(keys) > 0 {
		kv("indexes", strings.Join(keys, ","))
	}
	if len(eps) > 0 {
		kv("episodes", len(eps))
		kv("last event", latestEpisodeTime(eps))
	}
	cursors, _ := filepath.Glob(filepath.Join(paths.Data, "_cursors", "*.json"))
	kv("cursor files", len(cursors))
	kv("lagging cursors", 0)
	return nil
}
func latestEpisodeTime(eps []Episode) string {
	var latest string
	for _, e := range eps {
		if e.T > latest {
			latest = e.T
		}
	}
	return latest
}

func Refresh(w io.Writer, cwd string) error {
	paths := DefaultPaths()
	count := 0
	for _, dir := range exactProjectDirs(cwd) {
		var ef EpisodesFile
		if readJSON(filepath.Join(dir, "episodes.json"), &ef) {
			if err := writeJSONAtomic(filepath.Join(dir, "graph.json"), BuildGraph(ef.Episodes), 0o600); err != nil {
				return err
			}
			count++
		}
	}
	fmt.Fprintf(w, "orientation refresh: rebuilt %d graph(s) under %s\n", count, paths.Home)
	return nil
}

func Hook(r io.Reader, w io.Writer, args []string) error {
	var input []byte
	if r != nil {
		input, _ = io.ReadAll(io.LimitReader(r, 1<<20))
	}
	var p map[string]any
	_ = json.Unmarshal(input, &p)
	session := stringField(p, "session", "sessionId", "session_id", "id")
	runtime := valueAfter(args, "--runtime")
	if runtime == "" {
		runtime = stringField(p, "runtime", "adapter")
	}
	if runtime == "" {
		runtime = "eigen"
	}
	event := stringField(p, "event", "hookEvent", "hook_event")
	if event == "note" {
		text := strings.ToLower(strings.TrimSpace(stringField(p, "text", "message", "note")))
		if text == "" || !(strings.Contains(text, "compact") || text == "interrupted" || strings.HasPrefix(text, "error:")) {
			return nil
		}
	}
	source := valueAfter(args, "--source")
	cwd := valueAfter(args, "--cwd")
	sourceKind := valueAfter(args, "--source-kind")
	if source == "" {
		source, cwd, sourceKind = FindEigenSource(session, false)
	}
	if source == "" {
		return nil
	}
	branch := valueAfter(args, "--branch")
	branchSource := valueAfter(args, "--branch-source")
	return IngestSource(source, cwd, runtime, sourceKind, branch, branchSource, true)
}

func stringField(m map[string]any, names ...string) string {
	for _, n := range names {
		if v, ok := m[n]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
func valueAfter(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

func InstallHooks(wrapper string) error {
	paths := DefaultPaths()
	if err := os.MkdirAll(filepath.Dir(paths.Hooks), 0o700); err != nil {
		return err
	}
	if wrapper == "" {
		home, _ := os.UserHomeDir()
		wrapper = filepath.Join(home, ".local", "bin", "orientation")
	}
	type spec struct {
		Event    string   `json:"event"`
		Command  []string `json:"command"`
		Disabled bool     `json:"disabled,omitempty"`
	}
	var cfg struct {
		Hooks []spec `json:"hooks"`
	}
	if b, err := os.ReadFile(paths.Hooks); err == nil {
		_ = json.Unmarshal(b, &cfg)
		var arr []spec
		if len(cfg.Hooks) == 0 && json.Unmarshal(b, &arr) == nil {
			cfg.Hooks = arr
		}
	}
	isOrient := func(s spec) bool {
		joined := strings.Join(s.Command, " ")
		return strings.Contains(joined, "orientation") || strings.Contains(joined, "action-graph") || strings.Contains(joined, "hook.js") || strings.Contains(joined, "ORIENTATION_ENGINE_DIR")
	}
	out := cfg.Hooks[:0]
	for _, h := range cfg.Hooks {
		if !contains(eigenEvents, h.Event) || !isOrient(h) {
			out = append(out, h)
		}
	}
	for _, ev := range eigenEvents {
		out = append(out, spec{Event: ev, Command: []string{wrapper, "hook", "--runtime", "eigen"}})
	}
	cfg.Hooks = out
	return writeJSONAtomic(paths.Hooks, cfg, 0o600)
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func HooksStatus(w io.Writer) error {
	paths := DefaultPaths()
	b, err := os.ReadFile(paths.Hooks)
	if err != nil {
		fmt.Fprintln(w, "eigen hooks missing")
		return nil
	}
	var cfg struct {
		Hooks []struct {
			Event   string   `json:"event"`
			Command []string `json:"command"`
		} `json:"hooks"`
	}
	_ = json.Unmarshal(b, &cfg)
	for _, ev := range eigenEvents {
		ok := false
		for _, h := range cfg.Hooks {
			if h.Event == ev && strings.Contains(strings.Join(h.Command, " "), "orientation") {
				ok = true
			}
		}
		if ok {
			fmt.Fprintf(w, "%-14s installed\n", ev)
		} else {
			fmt.Fprintf(w, "%-14s missing\n", ev)
		}
	}
	return nil
}

func RunCLI(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		PrintUsage(stdout)
		return nil
	}
	switch args[0] {
	case "status":
		cwd := argOrCwd(args, 1)
		return Status(stdout, cwd)
	case "provenance":
		if len(args) < 3 {
			return fmt.Errorf("usage: orientation provenance <cwd> <file>")
		}
		return Provenance(stdout, args[1], strings.Join(args[2:], " "))
	case "related":
		if len(args) < 3 {
			return fmt.Errorf("usage: orientation related <cwd> <file>")
		}
		return Related(stdout, args[1], strings.Join(args[2:], " "))
	case "query":
		if len(args) < 2 {
			return fmt.Errorf("usage: orientation query <cwd> [keyword]")
		}
		q := ""
		if len(args) > 2 {
			q = strings.Join(args[2:], " ")
		}
		return Query(stdout, args[1], q)
	case "coupled":
		if len(args) < 3 {
			return fmt.Errorf("usage: orientation coupled <cwd> <file>")
		}
		return Coupled(stdout, args[1], strings.Join(args[2:], " "))
	case "threads":
		cwd := argOrCwd(args, 1)
		return Threads(stdout, cwd)
	case "refresh":
		cwd := argOrCwd(args, 1)
		return Refresh(stdout, cwd)
	case "hook":
		return Hook(stdin, stdout, args[1:])
	case "ingest":
		source := valueAfter(args[1:], "--source")
		cwd := valueAfter(args[1:], "--cwd")
		runtime := valueAfter(args[1:], "--runtime")
		return IngestSource(source, cwd, runtime, valueAfter(args[1:], "--source-kind"), valueAfter(args[1:], "--branch"), valueAfter(args[1:], "--branch-source"), true)
	case "hooks":
		if len(args) > 1 && args[1] == "install" {
			home, _ := os.UserHomeDir()
			wrapper := filepath.Join(home, ".local", "bin", "orientation")
			if err := InstallHooks(wrapper); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "eigen hooks installed")
			return nil
		}
		return HooksStatus(stdout)
	default:
		return fmt.Errorf("unknown orientation command %q", args[0])
	}
}
func argOrCwd(args []string, i int) string {
	if len(args) > i && args[i] != "" {
		return args[i]
	}
	cwd, _ := os.Getwd()
	return cwd
}

func PrintUsage(w io.Writer) {
	fmt.Fprint(w, `orientation — built-in Eigen harness history lookup

usage:
  eigen orientation install                 install wrapper + hooks
  eigen orientation refresh [cwd]           rebuild graphs
  eigen orientation provenance <cwd> <file> history + verdict for a file
  eigen orientation related    <cwd> <file> prior work + sibling files
  eigen orientation query      <cwd> [text] search indexed episodes
  eigen orientation threads    [cwd]        recent indexed threads
  eigen orientation coupled    <cwd> <file> files co-edited with this one
  eigen orientation status     [cwd]        summarize indexed project
  eigen orientation hook [--runtime eigen]  run the turn/session hook
  eigen orientation hooks [install]         inspect/install Eigen hooks

State lives under ~/.eigen/orientation. Orientation is native Go inside Eigen.
`)
}
