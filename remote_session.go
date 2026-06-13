package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/hook"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/remote"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/tui"
)

// runRemote is `eigen --remote user@host[:dir]`: attach a local TUI view to a
// session on a REMOTE eigen daemon over ssh. The whole agent loop (bash, file
// tools) runs on the remote; this side is a pure view. Mirrors the local
// daemon attach, minus local-only behavior (no local chdir, no /rebuild).
func runRemote(spec string, cfg config.Config) {
	runRemoteSession(spec, "", cfg)
}

// runRemoteSession opens a view on a remote machine. When sessionID is set it
// attaches that exact session; otherwise it picks the newest (or creates one
// rooted at the host's dir). This is the single remote entry — `eigen --remote`
// and the app's Machines drill-in both call it.
func runRemoteSession(spec, sessionID string, cfg config.Config) {
	hosts, herr := remote.LoadHosts()
	if herr != nil {
		fail(fmt.Errorf("hosts.json: %w", herr))
	}
	h, model, perm, err := hosts.Resolve(spec)
	if err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "connecting to %s …\n", h.SSHTarget())
	c, conn, err := remote.Dial(h.SSHTarget(), os.Stderr)
	if err != nil {
		fail(err)
	}
	defer conn.Close()

	// Probe the connection; a command-not-found / no-daemon failure should be a
	// clear "install eigen there" message, not a cryptic stream error.
	infos, err := c.List()
	if err != nil {
		fail(fmt.Errorf("no eigen daemon on %s: %w\n  bootstrap it with: eigen remote install %s",
			h.SSHTarget(), err, spec))
	}

	id := sessionID
	if id == "" {
		// Pick the newest session, or create one rooted at the host's dir. A
		// saved host's model/perm seed a newly created session.
		if len(infos) > 0 && h.Dir == "" {
			id = infos[0].ID
		} else {
			nid, nerr := c.NewSession(h.Dir, model, perm, nil)
			if nerr != nil {
				fail(nerr)
			}
			id = nid
		}
	}
	remoteAttachLoop(c, h, id, cfg)
}

// remoteAttachLoop runs the view over a remote session, handling alt+s hops to
// other REMOTE sessions. No local chdir (paths are remote) and no /rebuild
// (that would rebuild the local binary).
func remoteAttachLoop(c *daemon.Client, h remote.HostSpec, id string, cfg config.Config) {
	for {
		backend, err := chat.NewRemote(c, id)
		if err != nil {
			fail(err)
		}
		skills := skill.Discover(skillDirs()...)
		cwd, _ := os.Getwd()
		mem, _ := memory.Open(cwd)
		store, _ := session.Open()
		hookRunner, _ := hook.Load(hookConfigPath())
		res, err := tui.Run(backend, tui.Options{
			Provider:      backend.ProviderName(),
			Model:         backend.ModelID(),
			Memory:        mem,
			Store:         store,
			Skills:        skills,
			MaxTokens:     cfg.MaxTokens,
			NotifyCmd:     cfg.NotifyCmd,
			HookRunner:    hookRunner,
			NoSessionFile: true,
		})
		if err != nil {
			fail(err)
		}
		switch {
		case res.SwitchTo != "":
			id = res.SwitchTo
		default:
			return
		}
	}
}

// extractSockFlag scans `attach` sub-args for `--sock <path>` / `--sock=path`
// in any position (Go's flag pkg stops at the first positional, so `eigen
// attach --sock X` wouldn't otherwise parse). Returns (sockPath, sessionID);
// sockPath empty means no --sock was given.
func extractSockFlag(args []string) (sock, id string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--sock" || a == "-sock":
			if i+1 < len(args) {
				sock = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--sock="):
			sock = strings.TrimPrefix(a, "--sock=")
		case strings.HasPrefix(a, "-sock="):
			sock = strings.TrimPrefix(a, "-sock=")
		default:
			if id == "" && !strings.HasPrefix(a, "-") {
				id = a
			}
		}
	}
	return sock, id
}

// runAttachSock attaches to a daemon at an explicit socket path — the SSH
// unix-socket-forwarding path for "control eigen from another machine":
//
//	ssh -L /tmp/desktop-eigen.sock:$HOME/.eigen/daemon.sock desktop
//	eigen attach --sock /tmp/desktop-eigen.sock
//
// This reuses ssh's auth/crypto entirely — no token, no TLS, no network
// listener, no new attack surface. Unlike a local attach it NEVER auto-spawns a
// daemon (the daemon lives on the other end of the forward) and does no local
// chdir / rebuild (the session's project dir is on the remote machine).
func runAttachSock(sock, id string, cfg config.Config) {
	c, err := daemon.Dial(sock)
	if err != nil {
		fail(fmt.Errorf("attach --sock %s: %w\n  (is the socket forwarded? e.g. ssh -L %s:~/.eigen/daemon.sock <host>)", sock, err, sock))
	}
	defer c.Close()
	infos, err := c.List()
	if err != nil {
		fail(fmt.Errorf("list sessions over %s: %w", sock, err))
	}
	if id == "" {
		if len(infos) == 0 {
			cwd, _ := os.Getwd()
			nid, nerr := c.NewSession(cwd, "", "", nil)
			if nerr != nil {
				fail(nerr)
			}
			id = nid
		} else {
			id = infos[0].ID
		}
	}
	// Reuse the remote view loop (no local chdir / rebuild). Host label is
	// cosmetic here.
	remoteAttachLoop(c, remote.HostSpec{Host: "forwarded"}, id, cfg)
}
