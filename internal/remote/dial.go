package remote

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/avifenesh/eigen/internal/daemon"
)

// SSHArgs are the base flags for every ssh invocation: no pty (-T) so the byte
// stream isn't mangled by tty line discipline or `~.` escapes, and a keepalive
// so idle remote sessions don't silently drop. The user's ~/.ssh/config still
// applies (and wins for anything it sets).
func SSHArgs(extra ...string) []string {
	base := []string{"-T", "-o", "ServerAliveInterval=15"}
	return append(base, extra...)
}

// sshConn is an io.ReadWriteCloser over an `ssh host eigen daemon stdio`
// subprocess: Write → ssh stdin → remote socket; Read ← ssh stdout ← remote
// socket. Closing it tears the ssh process down. This is the remote transport
// for the daemon Client (DialConn accepts any io.ReadWriteCloser).
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

// Dial opens an ssh-stdio transport to a remote eigen daemon and returns a
// daemon.Client driving it. The remote runs `eigen daemon stdio`, which ensures
// a daemon is up there and bridges its socket to stdio. Remote stderr is
// forwarded to errOut (pass os.Stderr for an interactive session, io.Discard
// for a quiet listing) so connection problems are visible without corrupting
// the protocol byte stream (stdout). Close the returned io.Closer to tear the
// ssh process down.
func Dial(target string, errOut io.Writer) (*daemon.Client, io.Closer, error) {
	if errOut == nil {
		errOut = io.Discard
	}
	// ~/.local/bin may not be on a non-login ssh PATH; call the bare command
	// with an absolute-path fallback so either install location works.
	remoteCmd := `sh -lc 'eigen daemon stdio 2>/dev/null || ~/.local/bin/eigen daemon stdio'`
	cmd := exec.Command("ssh", SSHArgs(target, remoteCmd)...)
	cmd.Stderr = errOut
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("ssh %s: %w", target, err)
	}
	conn := &sshConn{cmd: cmd, stdin: stdin, stdout: stdout}
	return daemon.DialConn(conn), conn, nil
}

// ListSessions opens a transient ssh connection, lists the remote daemon's
// sessions, and closes it — a read-only peek for the Machines drill-in. Quiet
// (ssh stderr discarded); returns a clear error when eigen isn't installed
// remotely.
func ListSessions(target string) ([]daemon.SessionInfo, error) {
	c, closer, err := Dial(target, io.Discard)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	infos, err := c.List()
	if err != nil {
		return nil, fmt.Errorf("no eigen daemon on %s (install it: eigen remote install %s): %w", target, target, err)
	}
	return infos, nil
}
