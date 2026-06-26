package gui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/remote"
)

// Remote bridge layer. Surfaces the machines eigen can reach over ssh — saved
// hosts (~/.eigen/remote hosts.json) + detected ~/.ssh/config aliases — and
// (on demand) the sessions running on a remote daemon. Listing machines is
// instant + local; RemoteSessions dials over ssh, so the GUI calls it only on
// drill-in. Install is intentionally NOT exposed (it needs interactive ssh /
// credential push — done via `eigen remote install`).

// MachineDTO mirrors remote.Machine for the machines board.
type MachineDTO struct {
	Name     string `json:"name"`
	SSH      string `json:"ssh"`
	Addr     string `json:"addr,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Model    string `json:"model,omitempty"`
	Perm     string `json:"perm,omitempty"`
	Saved    bool   `json:"saved"`
	Detected bool   `json:"detected"`
}

// MachinesDTO is the remote-targets snapshot.
type MachinesDTO struct {
	Machines []MachineDTO `json:"machines"`
}

// Machines returns saved + ssh-config-detected remote targets (instant, local).
func (b *Bridge) Machines() (*MachinesDTO, error) {
	ms := remote.Machines()
	out := make([]MachineDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, MachineDTO{
			Name: m.Name, SSH: m.SSH, Addr: m.Addr, Dir: m.Dir,
			Model: m.Model, Perm: m.Perm, Saved: m.Saved, Detected: m.Detected,
		})
	}
	return &MachinesDTO{Machines: out}, nil
}

// remoteDialTimeout bounds the read-only ssh peek (dial + List). remote.Dial
// spawns `ssh …` whose Start returns immediately, so an unreachable host only
// surfaces when c.List blocks — and that blocks for the full daemon request
// timeout (~30s) with no cancel hook from the GUI. Cap the whole peek at 10s
// (the connect-timeout budget for the Machines drill-in) and tear the ssh
// process down on expiry so an unreachable machine fails fast instead of
// freezing the Machines view. Mirrors a 10s ssh ConnectTimeout, applied here
// because the read-only peek is the only caller that must stay snappy.
const remoteDialTimeout = 10 * time.Second

// RemoteSessions lists the sessions on a remote eigen daemon (dials over ssh —
// slow; called on drill-in only). Errors when the host is unreachable or has no
// eigen daemon running. The peek is bounded by remoteDialTimeout and is
// abandoned (ssh process killed, no leak) if a passed-in context is canceled,
// so an unreachable host can never wedge the UI goroutine that awaits it.
func (b *Bridge) RemoteSessions(target string) ([]SessionInfoDTO, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteDialTimeout)
	defer cancel()
	return b.remoteSessions(ctx, target)
}

// remoteSessions is the cancellable core of RemoteSessions. It reimplements the
// read-only peek (rather than calling remote.ListSessions, which dials + lists
// + closes with no cancellation hook) so the dial's io.Closer is in reach: when
// ctx is canceled — deadline or caller — Close kills the underlying ssh process
// instead of letting List block on the daemon request timeout.
func (b *Bridge) remoteSessions(ctx context.Context, target string) ([]SessionInfoDTO, error) {
	var errBuf bytes.Buffer
	c, closer, err := remote.Dial(target, &errBuf)
	if err != nil {
		return nil, err
	}

	type result struct {
		infos []daemon.SessionInfo
		err   error
	}
	done := make(chan result, 1)
	go func() {
		infos, lerr := c.List()
		done <- result{infos: infos, err: lerr}
	}()

	select {
	case <-ctx.Done():
		// Deadline hit or caller canceled: tear the ssh process down so the
		// pending List unblocks and no ssh subprocess / goroutine leaks. The
		// goroutine drains into the buffered channel after Close, then exits.
		_ = closer.Close()
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%s: timed out after %s (host unreachable?)", target, remoteDialTimeout)
		}
		return nil, fmt.Errorf("%s: %w", target, ctx.Err())
	case r := <-done:
		if r.err != nil {
			// Close first: it Waits on the ssh process, flushing remote stderr
			// into errBuf so a failure surfaces its real reason (eigen missing,
			// daemon can't spawn, bad creds) instead of a bare stream error.
			_ = closer.Close()
			if detail := strings.TrimSpace(errBuf.String()); detail != "" {
				return nil, fmt.Errorf("%s: %s", target, firstRemoteLine(detail))
			}
			return nil, fmt.Errorf("no eigen on %s — install it: eigen remote install %s", target, target)
		}
		_ = closer.Close()
		out := make([]SessionInfoDTO, 0, len(r.infos))
		for _, in := range r.infos {
			out = append(out, toSessionInfoDTO(in))
		}
		return out, nil
	}
}

// firstRemoteLine returns the first non-empty line of (possibly multi-line)
// remote stderr — the actionable reason, without the noise.
func firstRemoteLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return s
}
