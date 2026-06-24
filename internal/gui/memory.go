package gui

import (
	"fmt"
	"sort"
	"strings"

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
	Scope      string          `json:"scope"` // "project" | "global"
	Dir        string          `json:"dir"`
	Path       string          `json:"path"`
	Summary    string          `json:"summary"`
	HasSummary bool            `json:"hasSummary"`
	Notes      []MemoryNoteDTO `json:"notes"`
	NoteCount  int             `json:"noteCount"`
	Bans       string          `json:"bans"`
	BanList    []memory.Ban    `json:"banList"`           // structured title/rule blocks, for editing
	Profile        string      `json:"profile,omitempty"`        // global only (USER.md) — the user-editable section
	ProfileLearned string      `json:"profileLearned,omitempty"` // global only — the eigen-auto-maintained block (read-only in the GUI)
	AdHoc      []MemoryNoteDTO `json:"adHoc"`
	Backups    int             `json:"backups"`
	Bytes      int             `json:"bytes"`
}

// MemoryDTO is the full memory snapshot for the view: both scopes.
type MemoryDTO struct {
	Project *MemoryScopeDTO `json:"project"`
	Global  *MemoryScopeDTO `json:"global"`
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
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	sections := splitOnTopLevelHeadings(content)
	if sections == nil {
		// No "## " headings — fall back to blank-line chunks, dropping a leading
		// "# " file title.
		sections = splitOnBlankLines(content)
	}
	out := make([]MemoryNoteDTO, 0, len(sections))
	for i, s := range sections {
		out = append(out, MemoryNoteDTO{Index: i, Text: s})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitOnTopLevelHeadings splits content on lines that begin a top-level "## "
// section, returning each heading together with the body that follows it (up to
// the next "## " line). Any preamble before the first "## " — including a
// leading "# " file title — is dropped. Returns nil when content has no "## "
// heading line, so the caller can fall back to blank-line splitting.
func splitOnTopLevelHeadings(content string) []string {
	lines := strings.Split(content, "\n")
	var sections []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
			sections = append(sections, s)
		}
		cur = nil
	}
	started := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## ") {
			started = true
			flush() // close the prior section (or drop the preamble on first hit)
			cur = []string{ln}
			continue
		}
		if started {
			cur = append(cur, ln)
		}
	}
	if !started {
		return nil
	}
	flush()
	return sections
}

// splitOnBlankLines breaks content into blank-line-separated chunks, dropping
// empties and a leading top-level ATX "# " title. Used for un-consolidated
// stores that have no "## " section headings yet.
func splitOnBlankLines(content string) []string {
	chunks := strings.Split(content, "\n\n")
	out := make([]string, 0, len(chunks))
	skippedHeading := false
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// Drop a leading top-level heading: the first non-empty chunk when it is
		// solely a single ATX "# " line (the file's title), which would otherwise
		// render as a card whose only content is the heading.
		if !skippedHeading {
			skippedHeading = true
			if isTopLevelHeading(c) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// isTopLevelHeading reports whether chunk is solely a single top-level ATX
// heading line ("# ..."). A "## " (or deeper) heading, or a "# " line followed
// by body text, is not treated as the droppable file title.
func isTopLevelHeading(chunk string) bool {
	if strings.ContainsRune(chunk, '\n') {
		return false
	}
	return strings.HasPrefix(chunk, "# ")
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

// AppendMemory adds a manual note to the given scope ("project" | "global").
// Goes through Store.Append (not AddAdHocNote directly) so the save also
// enqueues consolidation + summary maintenance — the same path the agent's
// memory tool and the TUI use, which is what a "save to memory" action implies.
func (b *Bridge) AppendMemory(scope, note string) error {
	s, err := openScope(scope)
	if err != nil {
		return err
	}
	return s.Append(note)
}

// AddBan records (or updates, by title) a hard prohibition in the given scope's
// bans.md — the banthis layer, native in eigen, mirroring the TUI's /ban.
// Returns whether it replaced an existing ban of the same title.
func (b *Bridge) AddBan(scope, title, rule string) (bool, error) {
	s, err := openScope(scope)
	if err != nil {
		return false, err
	}
	return s.AddBan(title, rule)
}

// RemoveBan deletes a ban by title (case-insensitive) from the given scope,
// mirroring the TUI's /unban. Returns whether one was removed.
func (b *Bridge) RemoveBan(scope, title string) (bool, error) {
	s, err := openScope(scope)
	if err != nil {
		return false, err
	}
	return s.RemoveBan(title)
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
	s, err := openScope(scope)
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
