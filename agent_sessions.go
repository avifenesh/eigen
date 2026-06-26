package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/transcript"
)

// sessionRef points at one discovered transcript across any agent. Path is the
// file path (for file-based sources) or the session id (for OpenCode); Source
// tags which agent wrote it so callers can pick the right parser; ModTime is
// the last-modified time used for newest-first ordering.
type sessionRef struct {
	Path    string
	Source  transcript.Source
	ModTime time.Time
}

// agentSessionGlobs maps each file-based agent source to its transcript glob,
// relative to $HOME. These mirror internal/session.sourceGlobs — the canonical
// on-disk layouts: eigen's own sessions plus Claude and Codex. OpenCode lives
// in a SQLite DB (handled separately in recentAgentSessions), not on a glob.
var agentSessionGlobs = map[transcript.Source]string{
	transcript.SourceEigen:  ".eigen/sessions/*.eigen.jsonl",
	transcript.SourceClaude: ".claude/projects/*/*.jsonl",
	transcript.SourceCodex:  ".codex/sessions/*/*/*/rollout-*.jsonl",
}

// recentAgentSessions returns up to n transcript references across ALL known
// agents (eigen + Claude + Codex, plus OpenCode when its DB is present),
// newest-first by mtime. It is the wide-span counterpart to
// recentEigenSessions: dreaming and the feed can reflect over every agent's
// recent work, not just eigen's own last session.
//
// Each agent's directory is guarded by os.Stat — a missing dir (the user
// doesn't run that agent) is simply skipped, never an error. Glob/Stat errors
// on individual entries are likewise skipped so one unreadable file never
// drops the whole span.
func recentAgentSessions(n int) []sessionRef {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var refs []sessionRef
	for src, glob := range agentSessionGlobs {
		// Guard on the agent's root dir so a missing agent is skipped cheaply,
		// before globbing. The first path segment of the glob is that root.
		root := filepath.Join(home, firstGlobSegment(glob))
		if _, statErr := os.Stat(root); statErr != nil {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(home, glob))
		for _, path := range matches {
			fi, e := os.Stat(path)
			if e != nil || fi.IsDir() {
				continue
			}
			refs = append(refs, sessionRef{Path: path, Source: src, ModTime: fi.ModTime()})
		}
	}

	// OpenCode is a SQLite DB, not a glob. ListOpenCodeSessions opens it
	// read-only and reads only session metadata (id/title/updated) — one cheap
	// query, no message parsing — so it is safe to fold in here. A missing DB
	// returns an error, which we treat as "OpenCode not installed" and skip.
	if ocs, ocErr := transcript.ListOpenCodeSessions(""); ocErr == nil {
		for _, oc := range ocs {
			// time_updated is epoch milliseconds; 0 means unknown — keep it but
			// let it sort to the back via the zero-ish time.
			refs = append(refs, sessionRef{
				Path:    oc.ID,
				Source:  transcript.SourceOpenCode,
				ModTime: time.UnixMilli(oc.Updated),
			})
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].ModTime.After(refs[j].ModTime)
	})
	if len(refs) > n {
		refs = refs[:n]
	}
	return refs
}

// firstGlobSegment returns the leading non-wildcard path segments of a glob,
// i.e. the fixed root directory to stat. For ".eigen/sessions/*.eigen.jsonl"
// that is ".eigen/sessions"; for ".claude/projects/*/*.jsonl" that is
// ".claude/projects". It stops at the first segment containing a wildcard.
func firstGlobSegment(glob string) string {
	parts := strings.Split(glob, "/")
	var fixed []string
	for _, p := range parts {
		if strings.ContainsAny(p, "*?[") {
			break
		}
		fixed = append(fixed, p)
	}
	return filepath.Join(fixed...)
}
