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
)

// runDaemon starts the long-lived session host: the real eigen app. It owns
// agent sessions (each rooted at its own dir), serves views over a Unix
// socket, and keeps running until interrupted. Views (`eigen` windows) attach,
// mirror events, and send input; a session's lifetime is independent of any
// view.
func runDaemon(cfg config.Config) {
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
	fmt.Fprintf(os.Stderr, "eigen daemon listening on %s\n", daemon.SocketPath())

	// Graceful shutdown on SIGINT/SIGTERM.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Fprintln(os.Stderr, "eigen daemon shutting down")
		srv.Close()
	}()

	if err := srv.Serve(); err != nil {
		// A closed listener (clean shutdown) is not an error to report.
		if !isClosedErr(err) {
			fail(fmt.Errorf("daemon serve: %w", err))
		}
	}
}

func isClosedErr(err error) bool {
	// A closed listener (clean shutdown) surfaces as "use of closed network
	// connection".
	return err != nil && strings.Contains(err.Error(), "closed")
}
