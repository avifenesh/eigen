package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/view"
)

// runDaemon starts the long-lived session host: the real eigen app. It owns
// agent sessions (each rooted at its own dir), serves views over a Unix
// socket, and keeps running until interrupted. Views (`eigen` windows) attach,
// mirror events, and send input; a session's lifetime is independent of any
// view.
func runDaemon(cfg config.Config) {
	// Refuse to start a second daemon; clean up after a dead one.
	if pid := daemon.RunningPID(daemon.PIDPath()); pid != 0 {
		fmt.Fprintf(os.Stderr, "eigen daemon already running (pid %d)\n", pid)
		return
	}

	gmem, _ := memory.OpenGlobal()
	skills := skill.Discover(skillDirs()...)

	host := daemon.NewHost()
	// The builder turns a (dir, model) request into a fully-wired agent +
	// resource closer, reusing the same per-session construction as a chat.
	build := func(dir, model string) (*agent.Agent, func(), error) {
		if dir == "" {
			dir, _ = os.Getwd()
		}
		prov := firstNonEmpty(cfg.Provider, "converse")
		mdl := firstNonEmpty(model, cfg.Model)
		deps, err := buildSession(buildParams{
			Dir:       dir,
			Provider:  prov,
			Model:     mdl,
			Perm:      firstNonEmpty(cfg.Perm, "gated"),
			MaxTokens: cfg.MaxTokens,
			Cfg:       cfg,
			Skills:    skills,
			GlobalMem: gmem,
		})
		if err != nil {
			return nil, nil, err
		}
		return deps.Agent, deps.Close, nil
	}

	srv, err := daemon.Listen(daemon.SocketPath(), host, build)
	if err != nil {
		fail(fmt.Errorf("daemon: %w", err))
	}
	if err := daemon.WritePID(daemon.PIDPath()); err != nil {
		fmt.Fprintln(os.Stderr, "eigen daemon: pid file:", err)
	}
	defer daemon.RemovePID(daemon.PIDPath())
	fmt.Fprintf(os.Stderr, "eigen daemon listening on %s (pid %d)\n", daemon.SocketPath(), os.Getpid())

	// Graceful shutdown on SIGINT/SIGTERM: interrupt every session and close.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Fprintln(os.Stderr, "eigen daemon shutting down")
		for _, in := range host.List() {
			host.Remove(in.ID) // interrupt turns + release resources
		}
		srv.Close()
		daemon.RemovePID(daemon.PIDPath())
	}()

	if err := srv.Serve(); err != nil {
		// A closed listener (clean shutdown) is not an error to report.
		if !isClosedErr(err) {
			fail(fmt.Errorf("daemon serve: %w", err))
		}
	}
}

// daemonControl handles `eigen daemon <status|stop>`; returns true if it
// handled a control subcommand (caller should return).
func daemonControl(sub string) bool {
	switch sub {
	case "status":
		if pid := daemon.RunningPID(daemon.PIDPath()); pid != 0 {
			fmt.Printf("eigen daemon running (pid %d) on %s\n", pid, daemon.SocketPath())
		} else {
			fmt.Println("eigen daemon not running")
		}
		return true
	case "stop":
		pid, err := daemon.Stop(daemon.PIDPath())
		if err != nil {
			fail(err)
		}
		if pid == 0 {
			fmt.Println("eigen daemon not running")
		} else {
			fmt.Printf("stopped eigen daemon (pid %d)\n", pid)
			daemon.RemovePID(daemon.PIDPath())
			_ = os.Remove(daemon.SocketPath())
		}
		return true
	}
	return false
}

func isClosedErr(err error) bool {
	// A closed listener (clean shutdown) surfaces as "use of closed network
	// connection".
	return err != nil && strings.Contains(err.Error(), "closed")
}

// runAttach connects to the daemon and attaches a view to a session. With no
// id, it attaches to the most recently updated session, or creates one rooted
// at the current directory when the daemon has none.
func runAttach(id string, cfg config.Config) {
	c, err := daemon.Dial(daemon.SocketPath())
	if err != nil {
		fail(err)
	}
	defer c.Close()

	var title, dir string
	if id == "" {
		infos, lerr := c.List()
		if lerr != nil {
			fail(lerr)
		}
		if len(infos) == 0 {
			// No sessions yet: create one rooted here.
			cwd, _ := os.Getwd()
			nid, nerr := c.New(cwd, "")
			if nerr != nil {
				fail(nerr)
			}
			id, dir = nid, cwd
		} else {
			id = infos[0].ID // most recent
			title, dir = infos[0].Title, infos[0].Dir
		}
	} else {
		for _, in := range mustList(c) {
			if in.ID == id {
				title, dir = in.Title, in.Dir
			}
		}
	}
	if err := view.Run(c, id, title, dir); err != nil {
		fail(err)
	}
}

func mustList(c *daemon.Client) []daemon.SessionInfo {
	infos, err := c.List()
	if err != nil {
		return nil
	}
	return infos
}
