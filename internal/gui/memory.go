package gui

import (
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/memory"
)

// Memory bridge layer. Memory lives on the local filesystem (~/.eigen/memory),
// and the GUI process has direct access — so these methods read/write the
// stores directly rather than going through the daemon. Two scopes: the current
// project (rooted at the GUI's cwd) and the cross-project global store.

// MemoryNoteDTO is one parsed memory note for the browser. Memory files are
// append-only Markdown; we split on blank-line boundaries into renderable
// entries so the GUI can show them as cards rather than one wall of text.
type MemoryNoteDTO struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// MemoryScopeDTO is one memory scope (project or global) with its parsed
// content. Summary is the distilled/injected view; Notes is the raw append-only
// MEMORY.md split into entries; Bans + Profile are the editable side files.
type MemoryScopeDTO struct {
	Scope     string          `json:"scope"` // "project" | "global"
	Dir       string          `json:"dir"`
	Path      string          `json:"path"`
	Summary   string          `json:"summary"`
	HasSummary bool           `json:"hasSummary"`
	Notes     []MemoryNoteDTO `json:"notes"`
	NoteCount int             `json:"noteCount"`
	Bans      string          `json:"bans"`
	Profile   string          `json:"profile,omitempty"` // global only (USER.md)
	AdHoc     []MemoryNoteDTO `json:"adHoc"`
	Backups   int             `json:"backups"`
	Bytes     int             `json:"bytes"`
}

// MemoryDTO is the full memory snapshot for the view: both scopes.
type MemoryDTO struct {
	Project *MemoryScopeDTO `json:"project"`
	Global  *MemoryScopeDTO `json:"global"`
}

// splitNotes breaks append-only Markdown memory into entries on blank-line
// boundaries, dropping empties and a leading top-level heading.
func splitNotes(content string) []MemoryNoteDTO {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	chunks := strings.Split(content, "\n\n")
	out := make([]MemoryNoteDTO, 0, len(chunks))
	i := 0
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		out = append(out, MemoryNoteDTO{Index: i, Text: c})
		i++
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
		AdHoc:      adhoc,
		Backups:    len(s.Backups()),
		Bytes:      len(raw),
	}
	if scope == "global" {
		dto.Profile = strings.TrimSpace(s.UserProfile())
	}
	return dto
}

// Memory returns the full project + global memory snapshot.
func (b *Bridge) Memory() (*MemoryDTO, error) {
	proj, _ := memory.Open("")
	glob, _ := memory.OpenGlobal()
	return &MemoryDTO{
		Project: scopeDTO(proj, "project"),
		Global:  scopeDTO(glob, "global"),
	}, nil
}

// AppendMemory adds a manual note to the given scope ("project" | "global").
func (b *Bridge) AppendMemory(scope, note string) error {
	s, err := openScope(scope)
	if err != nil {
		return err
	}
	return s.AddAdHocNote(note, time.Now())
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
