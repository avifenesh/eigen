package gui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/observe"
	"github.com/avifenesh/eigen/internal/skill"
)

// EnsureDaemon returns a connected daemon client, starting the daemon first when
// the caller supplies the existing CLI's ensure function. Keeping daemon startup
// injected avoids making this package depend on package main.
func EnsureDaemon(ensure func() (*daemon.Client, error)) (*daemon.Client, error) {
	if ensure == nil {
		return daemon.Dial(daemon.SocketPath())
	}
	return ensure()
}

// Service is the app-facing API used by the graphical Eigen UI. It is a thin,
// UI-shaped facade over the existing daemon client: the daemon remains the
// source of truth for sessions, transcripts, permissions, and live events.
type Service struct {
	ensure func() (*daemon.Client, error)
}

// NewService constructs a GUI service. The ensure function should connect to a
// running daemon, starting it if necessary.
func NewService(ensure func() (*daemon.Client, error)) *Service {
	return &Service{ensure: ensure}
}

// Health reports whether the daemon is reachable.
func (s *Service) Health() (Health, error) {
	c, err := s.client()
	if err != nil {
		return Health{OK: false, Socket: daemon.SocketPath(), Error: err.Error()}, nil
	}
	defer c.Close()
	st, statErr := c.Stats()
	if statErr != nil {
		return Health{OK: true, Socket: daemon.SocketPath(), Error: statErr.Error()}, nil
	}
	return Health{OK: true, Socket: daemon.SocketPath(), Stats: st}, nil
}

// Sessions lists hosted daemon sessions newest-first.
func (s *Service) Sessions() ([]daemon.SessionInfo, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	infos, err := c.List()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].Updated > infos[j].Updated })
	return infos, nil
}

// NewSession creates a daemon session rooted at dir. Empty dir means the current
// working directory.
func (s *Service) NewSession(dir, model, perm string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	c, err := s.client()
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.NewSession(dir, model, perm, nil)
}

// State fetches the complete snapshot needed to render a chat workspace.
func (s *Service) State(id string) (*daemon.SessionState, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("session id required")
	}
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.State(id)
}

// Input sends user text to a session. It returns whether the daemon treated the
// input as a mid-turn steer instead of starting a fresh turn.
func (s *Service) Input(id, text string) (bool, error) {
	return s.InputWithTools(id, text, nil)
}

func (s *Service) InputWithTools(id, text string, allowTools []string) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, fmt.Errorf("session id required")
	}
	if strings.TrimSpace(text) == "" {
		return false, fmt.Errorf("input text required")
	}
	c, err := s.client()
	if err != nil {
		return false, err
	}
	defer c.Close()
	// Always use the general Input path (which starts a turn when idle, or
	// steers when a turn is running). SteerInput is a narrower "inject into
	// running turn" API; using it for the primary send path caused messages
	// to be accepted but the agent to stay idle (no new turn started).
	return false, c.Input(id, text, nil, allowTools)
}

func (s *Service) Approve(id, approvalID string, allow bool) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Approve(id, approvalID, allow)
}

func (s *Service) Interrupt(id string) error {
	return s.withClient(func(c *daemon.Client) error { return c.Interrupt(id) })
}
func (s *Service) Resend(id string) error {
	return s.withClient(func(c *daemon.Client) error { return c.Resend(id) })
}
func (s *Service) Clear(id string) error {
	return s.withClient(func(c *daemon.Client) error { return c.Clear(id) })
}
func (s *Service) Remove(id string) error {
	return s.withClient(func(c *daemon.Client) error { return c.Remove(id) })
}
func (s *Service) KillShell(id, shellID string) (bool, error) {
	c, err := s.client()
	if err != nil {
		return false, err
	}
	defer c.Close()
	return c.KillShell(id, shellID)
}
func (s *Service) DetachBash(id string) (bool, error) {
	c, err := s.client()
	if err != nil {
		return false, err
	}
	defer c.Close()
	return c.DetachBash(id)
}
func (s *Service) Compact(id string, target int) (int, int, error) {
	c, err := s.client()
	if err != nil {
		return 0, 0, err
	}
	defer c.Close()
	return c.Compact(id, target)
}
func (s *Service) SetGoal(id, goal string) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetGoal(id, goal) })
}
func (s *Service) SetTitle(id, title string) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetTitle(id, title) })
}
func (s *Service) AddDir(id, path string) (string, error) {
	c, err := s.client()
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.AddDir(id, path)
}
func (s *Service) SetPerm(id, perm string) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetPerm(id, perm) })
}
func (s *Service) SetModel(id, model string) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetModel(id, model) })
}
func (s *Service) SetEffort(id, effort string) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetEffort(id, effort) })
}
func (s *Service) SetSearch(id, mode string) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetSearch(id, mode) })
}
func (s *Service) SetFast(id string, on bool) error {
	return s.withClient(func(c *daemon.Client) error { return c.SetFast(id, on) })
}

// Observe returns a compact metadata-only telemetry summary for the GUI Observe
// surface. Missing logs are non-fatal so a fresh install still renders a useful
// empty state with the source path and CLI hint.
func (s *Service) Observe(limit int) (ObserveSnapshot, error) {
	if limit <= 0 {
		limit = 5000
	}
	path := observe.DefaultPath()
	out := ObserveSnapshot{Enabled: config.Load().ObserveEnabled(), Path: path, Limit: limit}
	if path == "" {
		out.Error = "observe path unavailable"
		return out, nil
	}
	summary, err := observe.ReadSummary(path, limit)
	if err != nil {
		if os.IsNotExist(err) {
			out.Missing = true
			return out, nil
		}
		out.Error = err.Error()
		return out, nil
	}
	out.Summary = summary
	return out, nil
}

// UserProfile returns the global USER.md personalization prompt used by Eigen
// memory injection. The GUI exposes this as a first-class profile editor.
func (s *Service) UserProfile() (string, error) {
	gm, err := memory.OpenGlobal()
	if err != nil {
		return "", err
	}
	return gm.UserProfile(), nil
}

// WriteUserProfile replaces the global USER.md personalization prompt. Empty
// content removes the file, matching memory.Store.WriteUserProfile semantics.
func (s *Service) WriteUserProfile(content string) error {
	gm, err := memory.OpenGlobal()
	if err != nil {
		return err
	}
	return gm.WriteUserProfile(content)
}

// ProjectMemory returns a read-only snapshot of a project's memory workspace
// (MEMORY.md, bans, ad-hoc notes, file listing) rooted at dir. The GUI Memory
// surface uses this to browse durable context without leaving the app. A
// missing/uninitialized workspace is non-fatal (returns an empty snapshot).
func (s *Service) ProjectMemory(dir string) (MemoryWorkspace, error) {
	out := MemoryWorkspace{Dir: dir}
	store, err := memory.Open(dir)
	if err != nil || store == nil {
		out.Error = "memory workspace unavailable"
		if err != nil {
			out.Error = err.Error()
		}
		return out, nil
	}
	out.Memory = store.Read()
	out.Bans = store.ListBans()
	if notes := store.AdHocNotes(50); len(notes) > 0 {
		out.Notes = notes
	}
	if files, err := store.ListFiles(); err == nil {
		out.Files = files
	}
	return out, nil
}

// SearchProjectMemory searches a project's memory workspace and returns hits.
func (s *Service) SearchProjectMemory(dir, query string) ([]memory.SearchHit, error) {
	store, err := memory.Open(dir)
	if err != nil || store == nil {
		return nil, fmt.Errorf("memory workspace unavailable")
	}
	return store.Search(query, 50)
}

// skillDirs mirrors main.skillDirs: the dirs scanned for SKILL.md skills.
func skillDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".eigen", "skills"),
		filepath.Join(".eigen", "skills"),
	}
	if extra := os.Getenv("EIGEN_SKILLS_DIRS"); extra != "" {
		dirs = append(dirs, strings.Split(extra, ":")...)
	}
	return dirs
}

// Skills discovers installed SKILL.md skills from the standard skill dirs and
// returns them for the GUI Skills surface. Discovery is best-effort: missing
// dirs simply yield fewer skills.
func (s *Service) Skills() ([]skill.Skill, error) {
	set := skill.Discover(skillDirs()...)
	return set.List(), nil
}

// SkillBody returns the full SKILL.md body for a named skill.
func (s *Service) SkillBody(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("skill name required")
	}
	set := skill.Discover(skillDirs()...)
	if _, ok := set.Get(name); !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}
	return set.Body(name)
}

func (s *Service) client() (*daemon.Client, error) { return EnsureDaemon(s.ensure) }

func (s *Service) withClient(fn func(*daemon.Client) error) error {
	c, err := s.client()
	if err != nil {
		return err
	}
	defer c.Close()
	return fn(c)
}

// EventStream owns one daemon attachment connection and converts daemon events
// into UI events. It is deliberately independent from request/response calls:
// browsers/desktops can keep one stream per open workspace while commands use
// short-lived daemon clients.
type EventStream struct {
	client *daemon.Client
	once   sync.Once
}

func (s *Service) Events(ctx context.Context, id string) (*EventStream, <-chan StreamEvent, error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil, fmt.Errorf("session id required")
	}
	c, err := s.client()
	if err != nil {
		return nil, nil, err
	}
	out := make(chan StreamEvent, 512)
	es := &EventStream{client: c}
	if err := c.Attach(id, func(e daemon.WireEvent, replay bool) {
		select {
		case out <- StreamEvent{Event: e, Replay: replay, At: time.Now()}:
		default:
			// A GUI can resync with State(); do not let a slow renderer block daemon IO.
		}
	}); err != nil {
		c.Close()
		return nil, nil, err
	}
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
		case <-c.Done():
		}
		es.Close()
	}()
	return es, out, nil
}

func (s *EventStream) Close() error {
	var err error
	s.once.Do(func() {
		if s.client != nil {
			err = s.client.Close()
		}
	})
	return err
}

// StreamJSONLines is a small adapter for dev/server modes: it writes one JSON
// object per event to w using enc(event). Desktop shells can use Events directly
// and dispatch into their native event bridge instead.
func StreamJSONLines(ctx context.Context, w io.Writer, events <-chan StreamEvent, enc func(io.Writer, StreamEvent) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if err := enc(w, ev); err != nil {
				return err
			}
		}
	}
}

// Health is intentionally JSON-shaped for frontend use.
type Health struct {
	OK     bool                `json:"ok"`
	Socket string              `json:"socket"`
	Error  string              `json:"error,omitempty"`
	Stats  *daemon.DaemonStats `json:"stats,omitempty"`
}

type ObserveSnapshot struct {
	Enabled bool            `json:"enabled"`
	Path    string          `json:"path"`
	Limit   int             `json:"limit"`
	Missing bool            `json:"missing,omitempty"`
	Error   string          `json:"error,omitempty"`
	Summary observe.Summary `json:"summary"`
}

type StreamEvent struct {
	Event  daemon.WireEvent `json:"event"`
	Replay bool             `json:"replay"`
	At     time.Time        `json:"at"`
}

// MemoryWorkspace is a read-only snapshot of a project's memory workspace for
// the GUI Memory surface.
type MemoryWorkspace struct {
	Dir    string       `json:"dir"`
	Memory string       `json:"memory"`
	Bans   []memory.Ban `json:"bans"`
	Notes  []string     `json:"notes"`
	Files  []string     `json:"files"`
	Error  string       `json:"error,omitempty"`
}
