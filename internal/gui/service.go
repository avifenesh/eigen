package gui

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/memory"
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
	return c.SteerInput(id, text, nil)
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

type StreamEvent struct {
	Event  daemon.WireEvent `json:"event"`
	Replay bool             `json:"replay"`
	At     time.Time        `json:"at"`
}
