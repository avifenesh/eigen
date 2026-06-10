package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
}

// SocketPath is the default daemon socket (~/.eigen/daemon.sock).
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "daemon.sock")
}

// Listen binds the daemon socket (removing a stale one). Only one daemon may
// own the socket; a second bind fails, which the caller treats as "already
// running".
func Listen(sockPath string, host *Host, build Builder) (*Server, error) {
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
		go s.handle(conn)
	}
}

// Close stops the server and removes the socket.
func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.sockPth)
	return err
}

// handle serves one view connection.
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
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
		case "remove":
			if s.host.Remove(req.ID) {
				send(Response{Type: "ok"})
			} else {
				send(Response{Type: "error", Error: "no such session"})
			}
		case "state":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			send(Response{Type: "state", ID: sess.ID, State: sess.state()})
		case "set":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			switch {
			case req.Perm != "":
				sess.setPerm(req.Perm)
			case req.Goal != nil:
				sess.setGoal(*req.Goal)
			case req.Model != "":
				if s.host.switchModel == nil {
					send(Response{Type: "error", Error: "model switching unavailable"})
					continue
				}
				p, c, budget, err := s.host.switchModel(sess.Dir, req.Model)
				if err != nil {
					send(Response{Type: "error", Error: err.Error()})
					continue
				}
				sess.setModel(req.Model, p, c, budget)
			default:
				send(Response{Type: "error", Error: "set: nothing to set"})
				continue
			}
			s.host.saveSessionMeta(sess) // durable: survives daemon restart
			send(Response{Type: "ok"})
		case "clear":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			sess.clear()
			s.host.saveSessionMeta(sess)
			send(Response{Type: "ok"})
		case "resend":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			if !sess.resend() {
				send(Response{Type: "error", Error: "session busy"})
				continue
			}
			send(Response{Type: "ok"})
		case "compact":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			before, after, err := sess.compact(context.Background(), req.Target)
			if err != nil {
				send(Response{Type: "error", Error: err.Error()})
				continue
			}
			send(Response{Type: "compacted", Before: before, After: after})
		case "approve":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			if sess.answer(req.Approval, req.Allow) {
				send(Response{Type: "ok"})
			} else {
				send(Response{Type: "error", Error: "no such pending approval"})
			}
		case "interrupt":
			if sess := s.host.Get(req.ID); sess != nil {
				sess.interrupt()
				send(Response{Type: "ok"})
			} else {
				send(Response{Type: "error", Error: "no such session"})
			}
		case "input":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			if !sess.send(req.Text, req.Images) {
				send(Response{Type: "error", Error: "session busy"})
				continue
			}
			send(Response{Type: "ok"})
		case "attach":
			sess := s.host.Get(req.ID)
			if sess == nil {
				send(Response{Type: "error", Error: "no such session"})
				continue
			}
			if detach != nil {
				detach() // one attachment per connection
			}
			replay, live, d := sess.attach()
			detach = d
			send(Response{Type: "attached", ID: sess.ID})
			for _, e := range replay {
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
		default:
			send(Response{Type: "error", Error: "unknown op: " + req.Op})
		}
	}
}
