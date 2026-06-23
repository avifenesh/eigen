package remote

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

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

// Re-listing the same machine within a few seconds (drill in, back out, drill
// back in — or two views refreshing the same target at once) shouldn't each pay
// a fresh ssh handshake + remote daemon spawn (seconds on a cold remote). Two
// guards make the read-only peek cheap under that load:
//
//   - listGroup coalesces concurrent ListSessions(target) calls: the first
//     dial-and-list for a target runs, and any sibling call for the same target
//     that arrives while it's in flight rides along on that one result instead
//     of spawning its own ssh. Different targets never block each other (the
//     singleflight key is the target).
//   - listCache holds each target's last good result for listCacheTTL. A re-list
//     inside that window returns the cached slice without dialing at all.
//
// Only successful lists are cached — an error (host down, eigen missing) retries
// on the next call rather than sticking. The cached slice is value-typed
// (daemon.SessionInfo has no reference fields) and treated read-only, so sharing
// it across coalesced/cached callers is safe.
const listCacheTTL = 3 * time.Second

var (
	listGroup singleflight.Group

	listCacheMu sync.Mutex
	listCache   = map[string]listCacheEntry{}
)

type listCacheEntry struct {
	infos []daemon.SessionInfo
	at    time.Time
}

func cachedSessions(target string) ([]daemon.SessionInfo, bool) {
	listCacheMu.Lock()
	defer listCacheMu.Unlock()
	e, ok := listCache[target]
	if !ok || time.Since(e.at) >= listCacheTTL {
		return nil, false
	}
	return e.infos, true
}

func storeSessions(target string, infos []daemon.SessionInfo) {
	listCacheMu.Lock()
	defer listCacheMu.Unlock()
	listCache[target] = listCacheEntry{infos: infos, at: time.Now()}
}

// ListSessions returns the sessions on a remote eigen daemon — a read-only peek
// for the Machines drill-in. Concurrent calls for the same target coalesce onto
// one ssh dial, and a result is reused for listCacheTTL, so re-opening the same
// machine doesn't pay the ssh handshake again within that window. A cold or
// error result is fetched freshly via dialAndList.
func ListSessions(target string) ([]daemon.SessionInfo, error) {
	if infos, ok := cachedSessions(target); ok {
		return infos, nil
	}
	v, err, _ := listGroup.Do(target, func() (any, error) {
		// Re-check inside the flight: a concurrent leader may have just
		// populated the cache between our miss above and acquiring the slot.
		if infos, ok := cachedSessions(target); ok {
			return infos, nil
		}
		infos, derr := dialAndList(target)
		if derr != nil {
			return nil, derr
		}
		storeSessions(target, infos)
		return infos, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]daemon.SessionInfo), nil
}

// dialAndList opens a transient ssh connection, lists the remote daemon's
// sessions, and closes it. The remote's stderr is captured so a failure (eigen
// not installed, daemon can't spawn, missing creds) surfaces its real reason
// instead of a bare "no daemon".
func dialAndList(target string) ([]daemon.SessionInfo, error) {
	var errBuf bytes.Buffer
	c, closer, err := Dial(target, &errBuf)
	if err != nil {
		return nil, err
	}
	infos, err := c.List()
	if err != nil {
		// Close first: it Waits on the ssh process, flushing any remote stderr
		// into errBuf before we read it.
		_ = closer.Close()
		detail := strings.TrimSpace(errBuf.String())
		if detail != "" {
			// The remote said something (command not found, build error, etc.).
			return nil, fmt.Errorf("%s: %s", target, firstLine(detail))
		}
		return nil, fmt.Errorf("no eigen on %s — install it: eigen remote install %s", target, target)
	}
	closer.Close()
	return infos, nil
}

// firstLine returns the first non-empty line (remote stderr can be multi-line).
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return s
}
