package gui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/memory"
)

// Memory bridge layer. Memory lives on the local filesystem (~/.eigen/memory),
// and the GUI process has direct access — so these methods read/write the
// stores directly rather than going through the daemon. Two scopes: the current
// project (rooted at the GUI's cwd) and the cross-project global store.

// MemoryNoteDTO is one parsed memory note for the browser. Curated MEMORY.md is
// section-structured Markdown ("## Heading\n\n- bullets"); we split on top-level
// "## " heading boundaries — keeping each heading with its body — into
// renderable entries so the GUI can show them as cards rather than one wall of
// text (and so a heading is never severed from its bullets into an orphan card).
type MemoryNoteDTO struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// MemoryScopeDTO is one memory scope (project or global) with its parsed
// content. Summary is the distilled/injected view; Notes is the raw append-only
// MEMORY.md split into entries; Bans + Profile are the editable side files.
type MemoryScopeDTO struct {
	Scope          string          `json:"scope"` // "project" | "global"
	Dir            string          `json:"dir"`
	Path           string          `json:"path"`
	Summary        string          `json:"summary"`
	HasSummary     bool            `json:"hasSummary"`
	Notes          []MemoryNoteDTO `json:"notes"`
	NoteCount      int             `json:"noteCount"`
	Bans           string          `json:"bans"`
	BanList        []memory.Ban    `json:"banList"`                  // structured title/rule blocks, for editing
	Profile        string          `json:"profile,omitempty"`        // global only (USER.md) — the user-editable section
	ProfileLearned string          `json:"profileLearned,omitempty"` // global only — the eigen-auto-maintained block (read-only in the GUI)
	AdHoc          []MemoryNoteDTO `json:"adHoc"`
	Backups        int             `json:"backups"`
	Bytes          int             `json:"bytes"`
}

// MemoryDTO is the full memory snapshot for the view: both scopes.
type MemoryDTO struct {
	Project *MemoryScopeDTO `json:"project"`
	Global  *MemoryScopeDTO `json:"global"`
}

// MemoryScopeRefDTO is a LIGHTWEIGHT selectable scope for the scope picker — key
// + readable name + note count, NOT the full notes/bans/profile payload (that's
// MemoryScopeDTO, loaded on demand via MemoryForScope). Key is what to pass back
// to MemoryForScope: "global", a session-history absolute dir, or an on-disk
// store key (e.g. "eigen-3e739af1"). Dir is the absolute project path when known
// (from session history) or empty (an on-disk store whose path the hash can't
// recover) — purely for display.
type MemoryScopeRefDTO struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	NoteCount int    `json:"noteCount"`
	Current   bool   `json:"current,omitempty"` // the GUI's cwd project — the picker's default selection
}

// splitNotes breaks curated Markdown memory into renderable entries. Post
// consolidation MEMORY.md is section-structured ("## Heading\n\n- bullets"), so
// we split on "## " heading boundaries and keep each heading together with the
// body that follows it — otherwise blank-line splitting would sever a heading
// from its bullets into orphan "## Preferences" cards. A leading top-level ATX
// "# " title (and any preamble before the first "## ") is dropped, mirroring the
// old behavior. Content with no "## " headings (e.g. an un-consolidated,
// append-only store) falls back to blank-line splitting so it still renders as
// cards rather than one wall of text.
func splitNotes(content string) []MemoryNoteDTO {
	sections := memory.SplitNotes(content)
	if len(sections) == 0 {
		return nil
	}
	out := make([]MemoryNoteDTO, 0, len(sections))
	for i, s := range sections {
		out = append(out, MemoryNoteDTO{Index: i, Text: s})
	}
	return out
}

func scopeDTO(s *memory.Store, scope string) *MemoryScopeDTO {
	if s == nil {
		return &MemoryScopeDTO{Scope: scope}
	}
	raw := s.Read()
	summary := strings.TrimSpace(s.Injected())
	notes := splitNotes(raw)
	adhocRaw := s.AdHocNotes(50)
	adhoc := make([]MemoryNoteDTO, 0, len(adhocRaw))
	for i, n := range adhocRaw {
		if t := strings.TrimSpace(n); t != "" {
			adhoc = append(adhoc, MemoryNoteDTO{Index: i, Text: t})
		}
	}
	dto := &MemoryScopeDTO{
		Scope:      scope,
		Dir:        s.Dir(),
		Path:       s.MemoryPath(),
		Summary:    summary,
		HasSummary: summary != "" && summary != strings.TrimSpace(raw),
		Notes:      notes,
		NoteCount:  len(notes),
		Bans:       strings.TrimSpace(s.Bans()),
		BanList:    s.ListBans(),
		AdHoc:      adhoc,
		Backups:    len(s.Backups()),
		Bytes:      len(raw),
	}
	if scope == "global" {
		// Split USER.md: the editor binds the user-authored section (WriteUserProfile
		// preserves the learned block), and the learned block is shown read-only.
		dto.Profile = strings.TrimSpace(s.UserProfileUser())
		dto.ProfileLearned = strings.TrimSpace(s.UserProfileLearned())
	}
	return dto
}

// Memory returns the full project + global memory snapshot. A failure to open
// either scope is surfaced (not swallowed) so the frontend's catch can
// distinguish a real load failure from a legitimately-empty store rather than
// rendering an empty DTO built from a nil store.
func (b *Bridge) Memory() (*MemoryDTO, error) {
	proj, err := memory.Open("")
	if err != nil {
		return nil, fmt.Errorf("open project memory: %w", err)
	}
	glob, err := memory.OpenGlobal()
	if err != nil {
		return nil, fmt.Errorf("open global memory: %w", err)
	}
	return &MemoryDTO{
		Project: scopeDTO(proj, "project"),
		Global:  scopeDTO(glob, "global"),
	}, nil
}

// ListMemoryScopes returns the lightweight, selectable memory scopes for the
// scope picker: "global", plus every known project. The project universe is the
// UNION of (a) stores already materialized on disk under ~/.eigen/memory/ and
// (b) the session-history cwds the feed scans (b.dirs) — a project may appear in
// one, the other, or both. They're deduped by resolved store key (a session dir
// maps to the same on-disk key its store uses), preferring the session-dir entry
// when both exist because it carries the readable absolute path for display.
// Note counts come from opening each store cheaply (parsed MEMORY.md only). A
// failure to enumerate the on-disk stores is surfaced; a single project that
// won't open is skipped rather than failing the whole list.
func (b *Bridge) ListMemoryScopes() ([]MemoryScopeRefDTO, error) {
	refs := make([]MemoryScopeRefDTO, 0, 8)

	// Global first — it's always present and always selectable.
	if glob, err := memory.OpenGlobal(); err != nil {
		return nil, fmt.Errorf("open global memory: %w", err)
	} else {
		refs = append(refs, MemoryScopeRefDTO{
			Key:       "global",
			Name:      "Global",
			Dir:       glob.Dir(),
			NoteCount: noteCount(glob),
		})
	}

	// Resolve the cwd project's store key so we can mark it Current (the picker's
	// default selection) — matching by KEY, not by comparing a project path to a
	// store path (those never match: a store's Dir() is ~/.eigen/memory/<key>).
	currentKey := ""
	if cur, err := memory.Open(""); err == nil {
		currentKey = memory.StoreKey(cur)
	}

	// Index by resolved store key so on-disk stores and session dirs dedup.
	byKey := map[string]int{} // store key -> index in refs

	// (a) Session-history dirs: absolute paths, so they carry a real Dir for
	// display. Opening by dir re-derives the same key the store uses on disk.
	for _, dir := range b.projectDirs() {
		dir = strings.TrimSpace(dir)
		if dir == "" || isEphemeralDir(dir) {
			continue // skip throwaway temp/sandbox cwds (see isEphemeralDir)
		}
		s, err := memory.Open(dir)
		if err != nil {
			continue // a single bad dir shouldn't sink the whole picker
		}
		k := memory.StoreKey(s)
		if k == "" || k == "global" {
			continue
		}
		if _, seen := byKey[k]; seen {
			continue
		}
		// A session dir with no actual memory yet is noise UNLESS it's the cwd
		// project (which the user is in right now and may write to).
		n := noteCount(s)
		if n == 0 && k != currentKey {
			continue
		}
		byKey[k] = len(refs)
		refs = append(refs, MemoryScopeRefDTO{
			Key:       dir, // a session dir round-trips through memory.Open
			Name:      memory.StoreName(s),
			Dir:       dir,
			NoteCount: n,
			Current:   k == currentKey,
		})
	}

	// (b) On-disk stores the feed never surfaced (e.g. a project no longer in
	// session history). The hash can't recover the abs path, so Dir stays empty
	// and the key is the on-disk dir name, opened via memory.OpenByKey.
	stores, err := memory.ListProjectStores()
	if err != nil {
		return nil, fmt.Errorf("list project memory stores: %w", err)
	}
	for _, ref := range stores {
		if _, seen := byKey[ref.Key]; seen {
			continue
		}
		s, err := memory.OpenByKey(ref.Key)
		if err != nil {
			continue
		}
		// An on-disk store with nothing in it is residue (a session that touched
		// the dir but never wrote memory) — don't surface empties here.
		n := noteCount(s)
		if n == 0 && ref.Key != currentKey {
			continue
		}
		byKey[ref.Key] = len(refs)
		refs = append(refs, MemoryScopeRefDTO{
			Key:       ref.Key,
			Name:      ref.Name,
			Dir:       "",
			NoteCount: n,
			Current:   ref.Key == currentKey,
		})
	}
	return refs, nil
}

// isEphemeralDir reports whether a working dir is a throwaway sandbox/temp cwd
// that shouldn't appear as a real "project" in the scope/recents pickers —
// agent-workspace runs, /tmp scratch, and the like. These accumulate in session
// history but are not projects the user browses memory for.
func isEphemeralDir(dir string) bool {
	clean := filepath.Clean(dir)
	for _, pre := range []string{"/tmp/", "/var/tmp/", "/run/", "/dev/"} {
		if strings.HasPrefix(clean+"/", pre) {
			return true
		}
	}
	// agent-workspace ephemeral checkouts (…/agent-workspace-…/…, …-itch-… temp dirs)
	base := filepath.Base(clean)
	if strings.Contains(clean, "/agent-workspace") || strings.Contains(base, "-itch-") {
		return true
	}
	return false
}

// MemoryForScope opens an ARBITRARY project memory store and returns the same
// rich MemoryScopeDTO that Memory() builds. Scope is one of: "global" / "" / a
// session-history absolute dir / an on-disk store key (e.g. "eigen-3e739af1");
// "project" and "" both mean the GUI's cwd (back-compat with Memory()). A load
// failure is surfaced so the frontend can tell it apart from an empty store.
func (b *Bridge) MemoryForScope(scope string) (*MemoryScopeDTO, error) {
	s, label, err := b.openMemoryScope(scope)
	if err != nil {
		return nil, err
	}
	return scopeDTO(s, label), nil
}

// openMemoryScope routes a scope identifier to its store and the scope LABEL
// scopeDTO should stamp (and key off — "global" unlocks the USER.md profile).
//   - "global"            -> the cross-project store, label "global"
//   - "project" / ""      -> the GUI's cwd project store, label "project"
//   - an absolute path    -> that project's store via Open(dir), label = the dir
//   - anything else       -> an on-disk store key via OpenByKey, label = the key
func (b *Bridge) openMemoryScope(scope string) (*memory.Store, string, error) {
	scope = strings.TrimSpace(scope)
	switch {
	case scope == "global":
		s, err := memory.OpenGlobal()
		if err != nil {
			return nil, "", fmt.Errorf("open global memory: %w", err)
		}
		return s, "global", nil
	case scope == "" || scope == "project":
		s, err := memory.Open("")
		if err != nil {
			return nil, "", fmt.Errorf("open project memory: %w", err)
		}
		return s, "project", nil
	case filepath.IsAbs(scope):
		s, err := memory.Open(scope)
		if err != nil {
			return nil, "", fmt.Errorf("open project memory %q: %w", scope, err)
		}
		return s, scope, nil
	default:
		s, err := memory.OpenByKey(scope)
		if err != nil {
			return nil, "", fmt.Errorf("open project memory store %q: %w", scope, err)
		}
		return s, scope, nil
	}
}

// noteCount parses a store's curated MEMORY.md into renderable entries and
// returns how many — the lightweight count for the scope picker, without
// shipping the note bodies.
func noteCount(s *memory.Store) int {
	if s == nil {
		return 0
	}
	return len(splitNotes(s.Read()))
}

// AppendMemory adds a manual note to the given scope ("project" | "global").
// Goes through Store.Append (not AddAdHocNote directly) so the save also
// enqueues consolidation + summary maintenance — the same path the agent's
// memory tool and the TUI use, which is what a "save to memory" action implies.
func (b *Bridge) AppendMemory(scope, note string) error {
	s, _, err := b.openMemoryScope(scope)
	if err != nil {
		return err
	}
	return s.Append(note)
}

// MoveMemoryNote relocates a fact between ANY two scopes — promote a project
// note to global, demote a global note to a project, OR move between two
// projects. `from` and `to` are scope identifiers the picker uses: "global",
// "project" (cwd), an absolute project dir, or an on-disk store key. The note is
// recorded in the destination and a supersede tombstone is left in the source so
// the next consolidation drops the old copy. Human peer of the memory tool move.
func (b *Bridge) MoveMemoryNote(from, to, note string) error {
	src, srcLabel, err := b.openMemoryScope(from)
	if err != nil {
		return err
	}
	dst, _, err := b.openMemoryScope(to)
	if err != nil {
		return err
	}
	if memory.StoreKey(src) == memory.StoreKey(dst) {
		return fmt.Errorf("source and destination scope are the same (%s)", srcLabel)
	}
	return memory.MoveNote(src, dst, note)
}

// MergeMemoryScope folds the project scope with on-disk key srcKey into dstKey
// (both project scopes). The manual heal for fragmented memory the GUI surfaces
// in the scope picker — e.g. an orphan scope from a deleted worktree merged into
// the live project. Returns a short human summary of what moved.
func (b *Bridge) MergeMemoryScope(srcKey, dstKey string) (string, error) {
	res, err := memory.MergeByKey(srcKey, dstKey, time.Now())
	if err != nil {
		return "", err
	}
	if res == nil {
		return "nothing to merge", nil
	}
	return fmt.Sprintf("merged %s → %s: %d bytes of memory, %d ad-hoc notes, %d rollouts (source archived)",
		res.SrcKey, res.DstKey, res.MemoryBytes, res.AdHocCopied, res.RolloutCopied), nil
}

// AddBan records (or updates, by title) a hard prohibition in the given scope's
// bans.md — the banthis layer, native in eigen, mirroring the TUI's /ban.
// Returns whether it replaced an existing ban of the same title.
func (b *Bridge) AddBan(scope, title, rule string) (bool, error) {
	s, _, err := b.openMemoryScope(scope)
	if err != nil {
		return false, err
	}
	return s.AddBan(title, rule)
}

// RemoveBan deletes a ban by title (case-insensitive) from the given scope,
// mirroring the TUI's /unban. Returns whether one was removed.
func (b *Bridge) RemoveBan(scope, title string) (bool, error) {
	s, _, err := b.openMemoryScope(scope)
	if err != nil {
		return false, err
	}
	return s.RemoveBan(title)
}

// RemoveMemoryNote drops one curated MEMORY.md note card by index (same order as
// MemoryForScope notes). Rewrites with a snapshot backup.
func (b *Bridge) RemoveMemoryNote(scope string, index int) error {
	s, _, err := b.openMemoryScope(scope)
	if err != nil {
		return err
	}
	return s.RemoveCuratedNote(index)
}

// RemoveAdHocMemoryNote deletes one manual ad-hoc save by index.
func (b *Bridge) RemoveAdHocMemoryNote(scope string, index int) error {
	s, _, err := b.openMemoryScope(scope)
	if err != nil {
		return err
	}
	return s.RemoveAdHocNote(index)
}

// WriteUserProfile replaces the global editable user profile (USER.md).
func (b *Bridge) WriteUserProfile(content string) error {
	glob, err := memory.OpenGlobal()
	if err != nil {
		return err
	}
	return glob.WriteUserProfile(content)
}

// MemoryBackups lists backup snapshot paths for a scope, newest last.
func (b *Bridge) MemoryBackups(scope string) ([]string, error) {
	s, _, err := b.openMemoryScope(scope)
	if err != nil {
		return nil, err
	}
	baks := s.Backups()
	sort.Strings(baks)
	return baks, nil
}

func openScope(scope string) (*memory.Store, error) {
	if scope == "global" {
		return memory.OpenGlobal()
	}
	return memory.Open("")
}
