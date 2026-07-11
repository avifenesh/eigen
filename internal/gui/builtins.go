package gui

import (
	"context"
	"time"

	"github.com/avifenesh/eigen/internal/obsidian"
	"github.com/avifenesh/eigen/internal/revuto"
)

// Native built-in connectors beyond Google: Obsidian (local vault notes) and
// revuto (the user's PR-reviewer daemon). Both are local direct integrations
// (FS / CLI), surfaced as Connectors-view cards with status. Unlike Google they
// need no OAuth — Obsidian just needs a vault dir, revuto just needs its CLI.

// ObsidianStatusDTO is the Obsidian connector card state.
type ObsidianStatusDTO struct {
	Available bool   `json:"available"` // a vault exists at the resolved path
	Vault     string `json:"vault"`     // resolved vault path
}

// ObsidianStatus reports vault availability + path.
func (b *Bridge) ObsidianStatus() (*ObsidianStatusDTO, error) {
	s := obsidian.CurrentStatus()
	return &ObsidianStatusDTO{Available: s.Available, Vault: s.Vault}, nil
}

// NoteDTO is one vault note for the Notes view.
type NoteDTO struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// ObsidianNotes lists/searches vault notes (blank query → recent). For the
// Notes view's list pane.
func (b *Bridge) ObsidianNotes(query string) ([]NoteDTO, error) {
	notes, err := obsidian.Search(query, 200)
	if err != nil {
		return nil, err
	}
	out := make([]NoteDTO, 0, len(notes))
	for _, n := range notes {
		out = append(out, NoteDTO{Path: n.Path, Title: n.Title})
	}
	return out, nil
}

// ObsidianRead returns a note's full markdown (the read pane).
func (b *Bridge) ObsidianRead(path string) (string, error) {
	return obsidian.Read(path)
}

// ObsidianWrite creates/updates (append=false) or appends to a note. Returns the
// vault-relative path. Powers the Notes view's edit/new + quick-capture.
func (b *Bridge) ObsidianWrite(path, content string, append bool) (string, error) {
	if append {
		return obsidian.Append(path, content)
	}
	return obsidian.Write(path, content)
}

// ChooseObsidianVault opens a native folder picker and pins the chosen dir as
// the Obsidian vault (must contain a .obsidian folder). Returns the new vault
// path, or "" when the user cancelled. Lets the user point eigen at ANY vault.
// The picker is host-UI work behind promptForPath (wails.go). Qt owns its own
// folder dialog and calls SetObsidianVault with the selected path instead.
func (b *Bridge) ChooseObsidianVault() (string, error) {
	dir, err := b.promptForPath("Choose your Obsidian vault", "", true)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil // cancelled
	}
	if err := obsidian.SetVault(dir); err != nil {
		return "", err
	}
	return obsidian.VaultPath(), nil
}

// SetObsidianVault persists a vault path selected by a non-Wails frontend.
// Qt uses QFileDialog locally, then sends the chosen directory over guiserver.
func (b *Bridge) SetObsidianVault(dir string) (string, error) {
	if err := obsidian.SetVault(dir); err != nil {
		return "", err
	}
	return obsidian.VaultPath(), nil
}

// RevutoStatusDTO is the revuto connector card state.
type RevutoStatusDTO struct {
	Available bool `json:"available"` // the revuto CLI is installed
	Count     int  `json:"count"`     // registered reviewers
	Paused    int  `json:"paused"`    // how many are paused
}

// RevutoStatus reports CLI availability + reviewer counts (best-effort).
func (b *Bridge) RevutoStatus() (*RevutoStatusDTO, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := revuto.CurrentStatus(ctx)
	return &RevutoStatusDTO{Available: s.Available, Count: s.Count, Paused: s.Paused}, nil
}

// RevutoReviewerDTO is one reviewer row for the connector card.
type RevutoReviewerDTO struct {
	Repo   string `json:"repo"`
	Paused bool   `json:"paused"`
}

// RevutoReviewers lists the registered reviewers (for the card's drill-in).
func (b *Bridge) RevutoReviewers() ([]RevutoReviewerDTO, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rs, err := revuto.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RevutoReviewerDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, RevutoReviewerDTO{Repo: r.Repo, Paused: r.Paused})
	}
	return out, nil
}

// RevutoTrigger runs a revuto job (review|learn|decay) for a repo. Slow.
func (b *Bridge) RevutoTrigger(repo, job string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
	defer cancel()
	return revuto.Trigger(ctx, repo, job)
}

// RevutoSetPaused pauses/resumes revuto scheduling for a repo.
func (b *Bridge) RevutoSetPaused(repo string, paused bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if paused {
		return revuto.Pause(ctx, repo)
	}
	return revuto.Resume(ctx, repo)
}
