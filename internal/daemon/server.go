package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/avifenesh/eigen/internal/agent"
)

// Server exposes a Host over a Unix socket. One connection = one view; a view
// may attach to a session to stream its events, or issue control ops.
type Server struct {
	host    *Host
	build   Builder
	ln      net.Listener
	sockPth string
	wg      sync.WaitGroup
}

// SocketPath is the daemon socket (~/.eigen/daemon.sock for the default
// instance; daemon-<instance>.sock otherwise).
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "daemon"+suffix()+".sock")
}

// Listen binds the daemon socket (removing a stale one). Only one daemon may
// own the socket; a second bind fails, which the caller treats as "already
// running".
func Listen(sockPath string, host *Host, build Builder) (*Server, error) {
	host.SetBuilder(build)
	if sockPath == "" {
		sockPath = SocketPath()
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return nil, err
	}
	// If a stale socket exists but nothing answers, remove it.
	if _, err := os.Stat(sockPath); err == nil {
		if c, derr := net.Dial("unix", sockPath); derr == nil {
			c.Close()
			return nil, errors.New("daemon already running")
		}
		_ = os.Remove(sockPath)
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	return &Server{host: host, build: build, ln: ln, sockPth: sockPath}, nil
}

// Serve accepts connections until the listener is closed.
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

// Close stops the server and removes the socket.
func (s *Server) Close() error {
	err := s.ln.Close()
	s.wg.Wait()
	_ = os.Remove(s.sockPth)
	return err
}

// handle serves one view connection.
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	// A panic while serving one connection (malformed request, a handler bug)
	// must not crash the daemon and take down every other session. Contain it
	// to this connection.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "eigen daemon: connection handler panic: %v\n", r)
		}
	}()
	var writeMu sync.Mutex
	send := func(v any) {
		b, err := encode(v)
		if err != nil {
			return
		}
		writeMu.Lock()
		_, _ = conn.Write(b)
		writeMu.Unlock()
	}

	var detach func()
	defer func() {
		if detach != nil {
			detach()
		}
	}()

	withLiveSession := func(id string, fn func(*Session)) {
		sess := s.host.Get(id)
		if sess == nil {
			send(Response{Type: "error", Error: "no such session"})
			return
		}
		sess.loadMu.Lock()
		defer sess.loadMu.Unlock()
		if !s.host.isCurrent(id, sess) {
			send(Response{Type: "error", Error: "no such session"})
			return
		}
		if err := s.host.hydrateLocked(sess); err != nil {
			send(Response{Type: "error", Error: err.Error()})
			return
		}
		fn(sess)
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			send(Response{Type: "error", Error: "bad request"})
			continue
		}
		switch req.Op {
		case "ping":
			send(Response{Type: "ok"})
		case "stats":
			st := s.host.Stats()
			send(Response{Type: "stats", Stats: &st})
		case "list":
			send(Response{Type: "sessions", Sessions: s.host.List()})
		case "new":
			a, closeFn, err := s.build(req.Dir, req.Model)
			if err != nil {
				send(Response{Type: "error", Error: err.Error()})
				continue
			}
			if req.Perm != "" {
				a.SetPerm(agent.Permission(req.Perm))
			}
			sess := s.host.Add(req.Dir, req.Model, a)
			sess.onClose = closeFn
			if len(req.History) > 0 {
				sess.resume(req.History)
				s.host.saveSessionMeta(sess)
			}
			send(Response{Type: "ok", ID: sess.ID})
			// A newly-created daemon row should not keep providers/MCP/LSP resident
			// until a view/input actually uses it.
			s.host.UnloadIfInactive(sess.ID)
		case "remove":
			if s.host.Remove(req.ID) {
				send(Response{Type: "ok"})
			} else {
				send(Response{Type: "error", Error: "no such session"})
			}
		case "prune":
			send(Response{Type: "ok", Pruned: s.host.PruneEmpty()})
		case "state":
			withLiveSession(req.ID, func(sess *Session) {
				send(Response{Type: "state", ID: sess.ID, State: sess.state()})
			})
		case "set":
			withLiveSession(req.ID, func(sess *Session) {
				switch {
				case req.Perm != "":
					sess.setPerm(req.Perm)
				case req.Goal != nil:
					sess.setGoal(*req.Goal)
				case req.Title != nil:
					sess.SetTitle(*req.Title)
				case req.Effort != "":
					if !sess.setEffort(req.Effort) {
						send(Response{Type: "error", Error: "effort not supported (or unknown level)"})
						return
					}
				case req.Search != "":
					if !sess.setSearch(req.Search) {
						send(Response{Type: "error", Error: "search not supported (or unknown mode)"})
						return
					}
				case req.Fast != nil:
					if !sess.setFast(*req.Fast) {
						send(Response{Type: "error", Error: "fast mode not supported on this model"})
						return
					}
				case req.Model != "":
					if s.host.switchModel == nil {
						send(Response{Type: "error", Error: "model switching unavailable"})
						return
					}
					p, c, budget, err := s.host.switchModel(sess.Dir, req.Model)
					if err != nil {
						send(Response{Type: "error", Error: err.Error()})
						return
					}
					sess.setModel(req.Model, p, c, budget)
				default:
					send(Response{Type: "error", Error: "set: nothing to set"})
					return
				}
				s.host.saveSessionMeta(sess) // durable: survives daemon restart
				send(Response{Type: "ok"})
			})
		case "add-dir":
			withLiveSession(req.ID, func(sess *Session) {
				root, err := sess.addDir(req.AddDir)
				if err != nil {
					send(Response{Type: "error", Error: err.Error()})
					return
				}
				s.host.saveSessionMeta(sess) // persist the added root across restart
				send(Response{Type: "ok", Root: root})
			})
		case "kill-shell":
			withLiveSession(req.ID, func(sess *Session) {
				send(Response{Type: "ok", Killed: sess.killShell(req.Shell)})
			})
		case "detach-bash":
			withLiveSession(req.ID, func(sess *Session) {
				send(Response{Type: "ok", Detached: sess.detachBash()})
			})
		case "clear":
			withLiveSession(req.ID, func(sess *Session) {
				if len(req.History) > 0 {
					// Reset-to-history: the /resume command loads a transcript
					// into this session (replaces the conversation).
					sess.clear()
					sess.resume(req.History)
				} else {
					sess.clear()
				}
				s.host.saveSessionMeta(sess)
				send(Response{Type: "ok"})
			})
		case "resend":
			withLiveSession(req.ID, func(sess *Session) {
				sess.resetGoalWakes() // user-initiated: refresh the auto-continue budget
				if !sess.resend() {
					send(Response{Type: "error", Error: "session busy"})
					return
				}
				send(Response{Type: "ok"})
			})
		case "compact":
			withLiveSession(req.ID, func(sess *Session) {
				before, after, err := sess.compact(context.Background(), req.Target)
				if err != nil {
					send(Response{Type: "error", Error: err.Error()})
					return
				}
				send(Response{Type: "compacted", Before: before, After: after})
			})
		case "approve":
			withLiveSession(req.ID, func(sess *Session) {
				if sess.answer(req.Approval, req.Allow) {
					send(Response{Type: "ok"})
				} else {
					send(Response{Type: "error", Error: "no such pending approval"})
				}
			})
		case "interrupt":
			// Route through withLiveSession so a cold (unloaded) session hydrates
			// rather than silently swallowing the interrupt, and surface whether a
			// turn was actually running — a view must be able to tell "interrupted
			// a running turn" from "nothing to interrupt".
			withLiveSession(req.ID, func(sess *Session) {
				send(Response{Type: "ok", Interrupted: sess.interrupt()})
			})
		case "input":
			withLiveSession(req.ID, func(sess *Session) {
				sess.resetGoalWakes() // user-initiated: refresh the auto-continue budget
				if sess.send(req.Text, req.Images, req.AllowTools) {
					send(Response{Type: "ok"})
					return
				}
				// A turn is already running → steer it (inject between tool-call
				// rounds) instead of rejecting "busy".
				if sess.steer(req.Text, req.Images) {
					send(Response{Type: "ok", Steered: true})
					return
				}
				send(Response{Type: "error", Error: "session busy"})
			})
		case "attach":
			if detach != nil {
				detach() // one attachment per connection
				detach = nil
			}
			withLiveSession(req.ID, func(sess *Session) {
				replay, live, d := sess.attach()
				detach = d
				send(Response{Type: "attached", ID: sess.ID})
				for _, e := range replay {
					// Approvals are re-sent canonically from pendingList() below
					// (outstanding only, no duplicates). Skipping them here avoids
					// delivering the same approval twice on a mid-wait attach and
					// avoids replaying already-resolved approvals as still pending.
					if e.Kind == agent.EventApproval {
						continue
					}
					send(Response{Type: "event", Event: wireEvent(e), Replay: true})
				}
				// A view attaching mid-wait must still see outstanding approvals
				// (their broadcast happened before this attach).
				for _, p := range sess.pendingList() {
					send(Response{Type: "event", Event: &WireEvent{Kind: "approval", ToolName: p.Tool, Text: p.Tool + " " + p.Args, Result: p.ID}})
				}
				// Stream live events for this session on a goroutine; the read loop
				// continues so the view can still send input/interrupt.
				go func() {
					for e := range live {
						send(Response{Type: "event", Event: wireEvent(e)})
					}
				}()
			})
		default:
			send(Response{Type: "error", Error: "unknown op: " + req.Op})
		}
	}
}
