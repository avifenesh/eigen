package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

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

// sshConn is an io.ReadWriteCloser over an `ssh host eigen daemon stdio`
// subprocess: Write → ssh stdin → remote socket; Read ← ssh stdout ← remote
// socket. Closing it tears the ssh process down. This is the remote transport
// for the daemon Client (Wave 0's DialConn accepts any io.ReadWriteCloser).
type sshConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (s *sshConn) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *sshConn) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *sshConn) Close() error {
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return s.cmd.Wait()
}

// dialRemote opens an ssh-stdio transport to a remote eigen daemon. The remote
// runs `eigen daemon stdio`, which ensures a daemon is up there and bridges its
// socket to stdio; we drive it as a normal daemon Client. Remote stderr is
// forwarded to our stderr so connection problems are visible without
// corrupting the protocol byte stream (stdout).
func dialRemote(h remote.HostSpec) (*daemon.Client, *sshConn, error) {
	// ~/.local/bin may not be on a non-login ssh PATH; call the absolute path
	// with a PATH-based fallback so either install location works.
	remoteCmd := `sh -lc 'eigen daemon stdio 2>/dev/null || ~/.local/bin/eigen daemon stdio'`
	args := sshArgs(h.SSHTarget(), remoteCmd)
	cmd := exec.Command("ssh", args...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("ssh %s: %w", h.SSHTarget(), err)
	}
	conn := &sshConn{cmd: cmd, stdin: stdin, stdout: stdout}
	client := daemon.DialConn(conn)
	return client, conn, nil
}

// runRemote is `eigen --remote user@host[:dir]`: attach a local TUI view to a
// session on a REMOTE eigen daemon over ssh. The whole agent loop (bash, file
// tools) runs on the remote; this side is a pure view. Mirrors the local
// daemon attach, minus local-only behavior (no local chdir, no /rebuild).
func runRemote(spec string, cfg config.Config) {
	hosts, herr := remote.LoadHosts()
	if herr != nil {
		fail(fmt.Errorf("hosts.json: %w", herr))
	}
	h, model, perm, err := hosts.Resolve(spec)
	if err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "connecting to %s …\n", h.SSHTarget())
	c, conn, err := dialRemote(h)
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

	// Pick or create the remote session (rooted at h.Dir when given). A saved
	// host's model/perm seed a newly created session.
	var id string
	if len(infos) > 0 && h.Dir == "" {
		id = infos[0].ID
	} else {
		nid, nerr := c.NewSession(h.Dir, model, perm, nil)
		if nerr != nil {
			fail(nerr)
		}
		id = nid
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
