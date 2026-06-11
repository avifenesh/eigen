package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/feed"
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

// openAction maps a session row to the right open action: daemon-backed
// sessions ATTACH to the durable session (never fork a new local copy);
// store sessions resume a fresh local chat from the transcript.
func openAction(r SessionRow) Result {
	if r.Source == "daemon" {
		return Result{Action: ActionAttach, SessionID: r.ID, Dir: r.Dir}
	}
	return Result{Action: ActionResume, SessionID: r.ID, Dir: r.Dir}
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
	Small     llm.Provider   // small model for page jobs (consolidate); nil = unavailable

	// Daemon is the connection to a running eigen daemon (nil when none):
	// its live sessions appear in the rail and can be attached as views.
	Daemon *daemon.Client
	Live   []daemon.SessionInfo // last-polled live sessions

	// Feed is the proactive action feed (loaded from cache instantly;
	// refreshed in the background when stale).
	Feed      feed.Feed
	FeedFresh bool
}

// projectDirs returns the known project directories, most recent first.
func (d *Data) projectDirs() []string {
	var dirs []string
	for _, p := range d.Projects {
		dirs = append(dirs, p.Dir)
	}
	return dirs
}

// refreshLive re-polls the daemon's session list (no-op without a daemon).
func (d *Data) refreshLive() {
	if d.Daemon == nil {
		return
	}
	if infos, err := d.Daemon.List(); err == nil {
		d.Live = infos
	}
}

// reloadSessions re-reads the session rows + projects from the store (titles
// fill in as the background titler persists them), then overlays durable
// daemon sessions — THE app sessions — listed straight from disk so they
// appear whether or not the daemon is up. Daemon rows attach; store rows
// resume (a daemon session must never be forked into a new local copy).
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
	for _, p := range daemon.ListPersisted() {
		title := p.Title
		if title == "" {
			title = "(untitled)"
		}
		rows = append(rows, SessionRow{
			ID:      p.ID,
			Title:   title,
			Source:  "daemon",
			Dir:     p.Dir,
			Msgs:    p.Msgs,
			Updated: p.Updated * 1_000_000_000, // store metas are unix-nano
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Updated > rows[j].Updated })
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
	}
	d.reloadSessions()
	d.Skills = skill.Discover(skillDirs()...)
	// Connect to a running daemon (optional — the app works without one).
	if c, err := daemon.Dial(daemon.SocketPath()); err == nil {
		d.Daemon = c
		d.refreshLive()
	}
	if gm, err := memory.OpenGlobal(); err == nil {
		d.GlobalMem = gm
	}
	d.Feed, d.FeedFresh = feed.Load() // instant (cache); app refreshes async
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

// feedFor returns the feed items scoped to a project dir (its loose ends),
// in feed order.
func (d *Data) feedFor(dir string) []feed.Item {
	var out []feed.Item
	for _, it := range d.Feed.Items {
		if it.Dir == dir {
			out = append(out, it)
		}
	}
	return out
}
