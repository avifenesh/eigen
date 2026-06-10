// Package session is eigen's session store: it discovers conversations across
// all sources (lazily, by date), records them in an index keyed by stable id
// (never by path), ingests each once into eigen-native JSONL so sources are
// never re-parsed, and titles untitled sessions with an async small model.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

// Meta is the indexed metadata for one session (no message bodies).
type Meta struct {
	ID          string            `json:"id"`
	Source      transcript.Source `json:"source"`
	Origin      string            `json:"origin"`     // file path, or opencode session id
	OriginMod   int64             `json:"origin_mod"` // mtime (cursor: re-ingest when it changes)
	Title       string            `json:"title"`
	Cwd         string            `json:"cwd,omitempty"` // working dir (project grouping)
	Messages    int               `json:"messages"`
	Updated     int64             `json:"updated"`            // for date ordering
	Ingested    bool              `json:"ingested"`           // copied into our JSONL
	PeekVer     int               `json:"peek_ver,omitempty"` // peekVersion when the cheap preview was last derived
	Fingerprint string            `json:"fingerprint"`        // for dedupe (set on ingest)
}

// Store is the on-disk session index + ingested copies under ~/.eigen.
type Store struct {
	dir   string // ~/.eigen
	mu    sync.Mutex
	metas map[string]*Meta
}

// Open loads (or creates) the session store under ~/.eigen.
func Open() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	s := &Store{dir: filepath.Join(home, ".eigen"), metas: map[string]*Meta{}}
	if err := os.MkdirAll(filepath.Join(s.dir, "store"), 0o755); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(s.indexPath()); err == nil {
		var list []*Meta
		if json.Unmarshal(b, &list) == nil {
			for _, m := range list {
				s.metas[m.ID] = m
			}
		}
	}
	return s, nil
}

func (s *Store) indexPath() string          { return filepath.Join(s.dir, "sessions.json") }
func (s *Store) eigenPath(id string) string { return filepath.Join(s.dir, "store", id+".jsonl") }

func (s *Store) save() error {
	list := make([]*Meta, 0, len(s.metas))
	for _, m := range s.metas {
		list = append(list, m)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.indexPath(), b, 0o644)
}

// Save persists the index.
func (s *Store) Save() error { s.mu.Lock(); defer s.mu.Unlock(); return s.save() }

// Get returns a meta by id.
func (s *Store) Get(id string) *Meta { s.mu.Lock(); defer s.mu.Unlock(); return s.metas[id] }

// List returns sessions newest-first, deduped by fingerprint (keeping the
// newest of each identical conversation).
func (s *Store) List() []*Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Meta, 0, len(s.metas))
	for _, m := range s.metas {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	seen := map[string]bool{}
	deduped := out[:0]
	for _, m := range out {
		if m.Fingerprint != "" {
			if seen[m.Fingerprint] {
				continue
			}
			seen[m.Fingerprint] = true
		}
		deduped = append(deduped, m)
	}
	return deduped
}

// peekVersion is bumped whenever the cheap-preview logic (title/cwd/turn-count
// extraction in transcript.Peek) changes, so existing indexed sessions are
// re-peeked once on the next discover. v2: turn counting (user+assistant
// messages) replaced raw line counting.
const peekVersion = 2

// peekBudget bounds how many sessions are previewed per discover, so a large
// backlog spreads its one-time scan over a few launches instead of a long
// startup freeze. Recent sessions go first.
const peekBudget = 400

// id derives a stable id from source+origin so re-discovery maps to the same
// session.
func id(src transcript.Source, origin string) string {
	h := sha256.Sum256([]byte(string(src) + "|" + origin))
	return "eig_" + hex.EncodeToString(h[:])[:10]
}

// sourceGlobs maps JSONL sources to their transcript file globs (relative to home).
var sourceGlobs = map[transcript.Source]string{
	transcript.SourceClaude: ".claude/projects/*/*.jsonl",
	transcript.SourceCodex:  ".codex/sessions/*/*/*/rollout-*.jsonl",
	transcript.SourcePi:     ".pi/agent/sessions/*/*.jsonl",
	transcript.SourceHermes: ".hermes/sessions/*.jsonl",
	transcript.SourceEigen:  ".eigen/sessions/*.eigen.jsonl",
}

// Discover scans every source cheaply (stat for files, one query for OpenCode),
// upserting metas for new or changed sessions. It does not parse message bodies.
func (s *Store) Discover() error {
	home, _ := os.UserHomeDir()
	s.mu.Lock()
	defer s.mu.Unlock()

	for src, glob := range sourceGlobs {
		matches, _ := filepath.Glob(filepath.Join(home, glob))
		for _, path := range matches {
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			s.upsert(src, path, fi.ModTime().UnixNano(), "")
		}
	}

	// OpenCode: titles come straight from its DB (no parse, no model needed).
	if ocs, err := transcript.ListOpenCodeSessions(""); err == nil {
		for _, oc := range ocs {
			s.upsert(transcript.SourceOpenCode, oc.ID, oc.Updated*int64(1e6), oc.Title)
		}
	}

	// Cheap preview pass: for file-based sessions not yet peeked (at the current
	// peekVersion), extract the working dir (project grouping), a title from the
	// first real user message, and a TURN count — mechanically, no model. To
	// keep startup snappy on a large backlog, this is bounded per discover and
	// prioritizes the most recently updated sessions; the rest fill in over the
	// next few launches. OpenCode has no file to peek (DB titles).
	pending := make([]*Meta, 0)
	for _, m := range s.metas {
		if m.PeekVer >= peekVersion || m.Source == transcript.SourceOpenCode {
			continue
		}
		pending = append(pending, m)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Updated > pending[j].Updated })
	if len(pending) > peekBudget {
		pending = pending[:peekBudget]
	}
	for _, m := range pending {
		pv := transcript.Peek(m.Source, m.Origin)
		if pv.Cwd != "" {
			m.Cwd = pv.Cwd
		}
		if m.Title == "" && pv.Title != "" {
			m.Title = pv.Title
		}
		if pv.Messages > 0 {
			m.Messages = pv.Messages
		}
		m.PeekVer = peekVersion
	}
	return s.save()
}

// upsert creates or updates a meta. A changed origin (mtime) clears Ingested so
// it is re-ingested on next load. Caller holds s.mu.
func (s *Store) upsert(src transcript.Source, origin string, mod int64, title string) {
	mid := id(src, origin)
	m := s.metas[mid]
	if m == nil {
		m = &Meta{ID: mid, Source: src, Origin: origin}
		s.metas[mid] = m
	}
	if m.OriginMod != mod {
		m.OriginMod = mod
		m.Ingested = false
		m.PeekVer = 0 // content changed: re-derive the cheap preview
	}
	if mod > m.Updated {
		m.Updated = mod
	}
	if title != "" && m.Title == "" {
		m.Title = title
	}
}

// Load returns the full conversation for a session, ingesting it into our JSONL
// on first use so the source is never parsed again.
func (s *Store) Load(mid string) ([]llm.Message, error) {
	m := s.Get(mid)
	if m == nil {
		return nil, os.ErrNotExist
	}
	path := s.eigenPath(mid)
	if m.Ingested {
		if msgs, err := transcript.Load(path); err == nil {
			return msgs, nil
		}
	}

	var msgs []llm.Message
	var err error
	if m.Source == transcript.SourceOpenCode {
		msgs, err = transcript.ImportOpenCode("", m.Origin)
	} else {
		msgs, err = transcript.ImportFrom(m.Source, m.Origin)
	}
	if err != nil {
		return nil, err
	}
	if err := transcript.Save(path, msgs); err != nil {
		return nil, err
	}
	s.mu.Lock()
	m.Ingested = true
	m.Messages = len(msgs)
	m.Fingerprint = fingerprint(msgs)
	s.save()
	s.mu.Unlock()
	return msgs, nil
}

// fingerprint identifies an (almost) identical conversation for dedupe.
func fingerprint(msgs []llm.Message) string {
	var first, last string
	for _, m := range msgs {
		if m.Role == llm.RoleUser {
			if first == "" {
				first = m.Text
			}
			last = m.Text
		}
	}
	h := sha256.Sum256([]byte(first + "\x00" + last))
	return hex.EncodeToString(h[:])[:12]
}

// Delete removes a session from the index and deletes our ingested JSONL copy
// (and, for eigen-native sessions, the original eigen file + meta sidecar). It
// never deletes a FOREIGN source file (claude/codex/pi/hermes/opencode) — only
// eigen's own copies — so dropping such a session just forgets it from the
// index; it will be re-discovered unless the source file is gone. Returns
// whether anything was removed.
func (s *Store) Delete(mid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.metas[mid]
	if m == nil {
		return false
	}
	// Remove our ingested copy.
	_ = os.Remove(s.eigenPath(mid))
	// For eigen-native sessions, the origin IS our file — remove it + sidecar.
	if m.Source == transcript.SourceEigen && strings.HasSuffix(m.Origin, ".jsonl") {
		_ = os.Remove(m.Origin)
		_ = os.Remove(m.Origin + ".meta.json")
	}
	delete(s.metas, mid)
	_ = s.save()
	return true
}

// Export writes a session's full transcript to destPath as eigen-native JSONL
// (loading/ingesting it first if needed). The caller chooses the path.
func (s *Store) Export(mid, destPath string) error {
	msgs, err := s.Load(mid)
	if err != nil {
		return err
	}
	return transcript.Save(destPath, msgs)
}
