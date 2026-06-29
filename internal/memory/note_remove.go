package memory

import (
	"fmt"
	"os"
	"strings"
)

// AdHocEntry is one manual save on disk (path + rendered text).
type AdHocEntry struct {
	Path string
	Text string
}

// AdHocEntries returns ad-hoc notes with their file paths (chronological order).
func (s *Store) AdHocEntries(limit int) []AdHocEntry {
	if s == nil {
		return nil
	}
	contents, paths := s.adHocNotesWithPaths(limit)
	out := make([]AdHocEntry, 0, len(contents))
	for i := range contents {
		out = append(out, AdHocEntry{Path: paths[i], Text: strings.TrimSpace(contents[i])})
	}
	return out
}

// RemoveAdHocNote deletes one ad-hoc note file by index (0-based, same order as
// AdHocNotes / AdHocEntries). The file is removed from disk (not retired).
func (s *Store) RemoveAdHocNote(index int) error {
	if s == nil {
		return fmt.Errorf("memory unavailable")
	}
	entries := s.AdHocEntries(0)
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("ad-hoc note index %d out of range (have %d)", index, len(entries))
	}
	p := entries[index].Path
	if p == "" {
		return fmt.Errorf("ad-hoc note has no path")
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	s.enqueueMaintenance()
	return nil
}

// RemoveCuratedNote drops one rendered note card from MEMORY.md by index (0-based,
// same order as splitNotes on Read()). Rewrites MEMORY.md with a snapshot backup.
func (s *Store) RemoveCuratedNote(index int) error {
	if s == nil {
		return fmt.Errorf("memory unavailable")
	}
	raw := s.Read()
	sections := splitNotesForEdit(raw)
	if index < 0 || index >= len(sections) {
		return fmt.Errorf("note index %d out of range (have %d)", index, len(sections))
	}
	sections = append(sections[:index], sections[index+1:]...)
	newContent := joinNoteSections(sections)
	return s.Rewrite(newContent)
}

// splitNotesForEdit returns note bodies the same way the GUI splitNotes does.
func splitNotesForEdit(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	sections := splitOnTopLevelHeadings(content)
	if sections == nil {
		sections = splitOnBlankLines(content)
	}
	return sections
}

func joinNoteSections(sections []string) string {
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n") + "\n"
}
