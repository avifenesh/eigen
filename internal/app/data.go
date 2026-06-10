package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
)

// SessionRow is one session as the app shows it.
type SessionRow struct {
	ID      string
	Title   string
	Source  string
	Dir     string // project dir (from the meta sidecar; "" if unknown)
	Msgs    int
	Updated int64
}

// ProjectRow is one project (sessions grouped by Dir).
type ProjectRow struct {
	Dir      string
	Name     string
	Sessions []SessionRow
	Updated  int64 // most recent session
}

// Data is everything the app's pages render. Loaded once at startup (cheap:
// index + metadata only, no message bodies) and refreshable.
type Data struct {
	Sessions  []SessionRow // newest first
	Projects  []ProjectRow // most recently active first
	Config    config.Config
	Skills    *skill.Set
	GlobalMem *memory.Store
	Store     *session.Store
	Titler    session.Titler // small-model background titler (nil = none)
}

// reloadSessions re-reads the session rows + projects from the store (titles
// fill in as the background titler persists them).
func (d *Data) reloadSessions() {
	if d.Store == nil {
		return
	}
	var rows []SessionRow
	for _, meta := range d.Store.List() {
		row := SessionRow{
			ID:      meta.ID,
			Title:   meta.Title,
			Source:  string(meta.Source),
			Dir:     meta.Cwd,
			Msgs:    meta.Messages,
			Updated: meta.Updated,
		}
		if row.Title == "" {
			row.Title = "(untitled)"
		}
		rows = append(rows, row)
	}
	d.Sessions = rows
	d.Projects = groupProjects(rows)
}

// Load gathers the app's data. Failures degrade (a page shows "unavailable")
// rather than failing the app.
func Load() *Data {
	d := &Data{Config: config.Load()}

	if store, err := session.Open(); err == nil {
		_ = store.Discover()
		d.Store = store
		for _, meta := range store.List() {
			row := SessionRow{
				ID:      meta.ID,
				Title:   meta.Title,
				Source:  string(meta.Source),
				Dir:     meta.Cwd,
				Msgs:    meta.Messages,
				Updated: meta.Updated,
			}
			if row.Title == "" {
				row.Title = "(untitled)"
			}
			d.Sessions = append(d.Sessions, row)
		}
		sort.Slice(d.Sessions, func(i, j int) bool { return d.Sessions[i].Updated > d.Sessions[j].Updated })
	}

	d.Projects = groupProjects(d.Sessions)
	d.Skills = skill.Discover(skillDirs()...)
	if gm, err := memory.OpenGlobal(); err == nil {
		d.GlobalMem = gm
	}
	return d
}

// groupProjects buckets sessions by Dir (sessions with no dir are skipped —
// they appear only in the flat sessions list).
func groupProjects(rows []SessionRow) []ProjectRow {
	byDir := map[string]*ProjectRow{}
	for _, r := range rows {
		if r.Dir == "" {
			continue
		}
		p, ok := byDir[r.Dir]
		if !ok {
			p = &ProjectRow{Dir: r.Dir, Name: filepath.Base(r.Dir)}
			byDir[r.Dir] = p
		}
		p.Sessions = append(p.Sessions, r)
		if r.Updated > p.Updated {
			p.Updated = r.Updated
		}
	}
	out := make([]ProjectRow, 0, len(byDir))
	for _, p := range byDir {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out
}

// skillDirs mirrors main's skill discovery: user + project + env.
func skillDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".eigen", "skills"))
	}
	dirs = append(dirs, filepath.Join(".eigen", "skills"))
	if env := os.Getenv("EIGEN_SKILLS_DIRS"); env != "" {
		dirs = append(dirs, strings.Split(env, ":")...)
	}
	return dirs
}

// ModelRow is one catalog model for the models page.
type ModelRow struct {
	ID        string
	Provider  string
	Window    int
	Available bool
	Tags      string // capability tags (search/vision/reasoning…)
}

// Models lists the catalog with availability.
func Models() []ModelRow {
	avail := map[string]bool{}
	var out []ModelRow
	for _, m := range llm.Catalog {
		cp := m.Provider
		v, seen := avail[cp]
		if !seen {
			v = llm.ProviderAvailable(cp)
			avail[cp] = v
		}
		var tags []string
		if m.Reasoning {
			tags = append(tags, "reasoning")
		}
		if m.Search {
			tags = append(tags, "search")
		}
		if m.Vision {
			tags = append(tags, "vision")
		}
		if m.Social {
			tags = append(tags, "social")
		}
		out = append(out, ModelRow{
			ID:        m.ID,
			Provider:  m.Provider,
			Window:    m.ContextWindow,
			Available: v,
			Tags:      strings.Join(tags, " "),
		})
	}
	return out
}

// ProviderRow is one provider's status for the providers page.
type ProviderRow struct {
	Name      string
	Available bool
	Default   string // default model
	Models    int
}

// Providers summarizes provider credential status from the catalog.
func Providers() []ProviderRow {
	counts := map[string]int{}
	order := []string{}
	for _, m := range llm.Catalog {
		if counts[m.Provider] == 0 {
			order = append(order, m.Provider)
		}
		counts[m.Provider]++
	}
	var out []ProviderRow
	for _, p := range order {
		out = append(out, ProviderRow{
			Name:      p,
			Available: llm.ProviderAvailable(p),
			Default:   llm.DefaultModel(p),
			Models:    counts[p],
		})
	}
	return out
}
