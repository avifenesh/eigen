package gui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/remote"
)

// Session refs. The GUI addresses every session by an opaque id string. A LOCAL
// session is a bare daemon id ("s3"). A REMOTE session — one living on another
// machine's eigen daemon, reached over ssh — is encoded as
//
//	remote:<base64url(target)>:<realID>
//
// where target is the ssh `[user@]host[:dir]` and realID is the id on THAT
// daemon. Session ids are NOT globally unique (every daemon counts s1, s2, …
// from zero), so the target MUST be part of the ref. The encoding is
// self-describing: a ref survives a deep-link / frontend reload without any
// prior RemoteSessions() call, and parseSessionRef is the exact inverse of
// encodeRemoteRef. base64url (raw, unpadded) keeps the ref free of '+' '/' '='
// so it rides a URL hash route param and a Wails event name untouched.
const remoteRefPrefix = "remote:"

// encodeRemoteRef builds the opaque GUI session ref for a remote session.
func encodeRemoteRef(target, realID string) string {
	return remoteRefPrefix + base64.RawURLEncoding.EncodeToString([]byte(target)) + ":" + realID
}

// parseSessionRef splits a session ref into its remote ssh target and the real
// id on that daemon. isRemote is false for a bare local id (target "", realID ==
// ref). A malformed remote ref (bad base64, missing pieces) also returns
// isRemote=false, so it degrades to a local lookup that simply 404s rather than
// panicking on a hostile/garbled value.
func parseSessionRef(ref string) (target, realID string, isRemote bool) {
	if !strings.HasPrefix(ref, remoteRefPrefix) {
		return "", ref, false
	}
	rest := strings.TrimPrefix(ref, remoteRefPrefix)
	// Cut on the FIRST colon: the base64url target never contains one, and
	// whatever follows is the real id verbatim (guards against a colon ever
	// appearing in a daemon id).
	enc, id, ok := strings.Cut(rest, ":")
	if !ok || enc == "" || id == "" {
		return "", ref, false
	}
	tb, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil || len(tb) == 0 {
		return "", ref, false
	}
	return string(tb), id, true
}

// remoteCtrlDialTimeout bounds the ssh dial + reachability probe for a pooled
// remote control client. A hung ssh (network black hole, host-key prompt) would
// otherwise block the calling RPC forever; the bound turns it into a clear,
// recoverable error. Generous vs the read-only Machines peek (10s) because a
// cold remote daemon may need to spawn + restore sessions on first contact.
const remoteCtrlDialTimeout = 20 * time.Second

// clientFor resolves a GUI session ref to the daemon Client that hosts it and
// the real id on that daemon. A local ref returns the long-lived local control
// client (id unchanged). A remote ref returns a per-target pooled control
// client, dialed over ssh on first use and reused while alive.
func (b *Bridge) clientFor(ref string) (*daemon.Client, string, error) {
	target, realID, isRemote := parseSessionRef(ref)
	if !isRemote {
		c, err := b.control()
		return c, ref, err
	}
	c, err := b.remoteControl(target)
	if err != nil {
		return nil, "", err
	}
	return c, realID, nil
}

// remoteControl returns a pooled control client for a remote ssh target,
// dialing it lazily on first use and reusing it while its connection is alive.
// The dial is bounded (remoteCtrlDialTimeout) and singleflighted per target, so
// concurrent RPCs for a cold target share ONE ssh spawn instead of racing to
// open — and leak — several. A dead pooled client is closed and re-dialed.
// Pool map access + the b.closing check are under b.mu (matching control());
// the ssh dial itself runs OUTSIDE the lock inside the singleflight callback so
// a slow remote can never stall unrelated RPCs.
func (b *Bridge) remoteControl(target string) (*daemon.Client, error) {
	// Fast path: a live pooled client for this target.
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return nil, fmt.Errorf("bridge shutting down")
	}
	if c := b.remoteCtrls[target]; c != nil {
		select {
		case <-c.Done(): // dead: drop + close, re-dial below
			delete(b.remoteCtrls, target)
			b.mu.Unlock()
			_ = c.Close()
		default:
			b.mu.Unlock()
			return c, nil
		}
	} else {
		b.mu.Unlock()
	}

	v, err, _ := b.remoteDial.Do(target, func() (any, error) {
		// Re-check under the lock: a sibling flight may have populated the pool
		// between our miss above and acquiring the singleflight slot.
		b.mu.Lock()
		if c := b.remoteCtrls[target]; c != nil {
			select {
			case <-c.Done():
				delete(b.remoteCtrls, target)
				b.mu.Unlock()
				_ = c.Close()
			default:
				b.mu.Unlock()
				return c, nil
			}
		} else {
			b.mu.Unlock()
		}

		c, derr := dialRemoteControl(target, remoteCtrlDialTimeout)
		if derr != nil {
			return nil, derr
		}
		b.mu.Lock()
		if b.closing {
			b.mu.Unlock()
			_ = c.Close()
			return nil, fmt.Errorf("bridge shutting down")
		}
		b.remoteCtrls[target] = c
		b.mu.Unlock()
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*daemon.Client), nil
}

// dialRemoteControl opens an ssh control connection to a remote eigen daemon and
// verifies it's reachable with a bounded List probe before returning it for
// pooling. ssh stderr is captured so a failure (eigen missing, bad creds,
// daemon can't spawn) surfaces its real reason instead of a bare stream error.
// On timeout or probe error the connection is torn down (killing the ssh
// process) so nothing leaks. daemon.Client.Close() closes the underlying ssh
// conn, so no separate io.Closer is tracked.
func dialRemoteControl(target string, d time.Duration) (*daemon.Client, error) {
	var errBuf bytes.Buffer
	c, _, err := remote.Dial(target, &errBuf)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := c.List(); done <- e }()
	select {
	case <-ctx.Done():
		// Close tears the ssh process down; the probe goroutine's List then
		// fails into the buffered channel and exits (no leak). errBuf is NOT
		// read here — only after a Close-flushed error below — so there's no
		// concurrent access to the buffer the ssh stderr pump writes.
		_ = c.Close()
		return nil, fmt.Errorf("%s: timed out after %s (host unreachable?)", target, d)
	case e := <-done:
		if e != nil {
			// Close first: it Waits on the ssh process, flushing remote stderr
			// into errBuf before we read it.
			_ = c.Close()
			if detail := strings.TrimSpace(errBuf.String()); detail != "" {
				return nil, fmt.Errorf("%s: %s", target, firstRemoteLine(detail))
			}
			return nil, fmt.Errorf("no eigen on %s — install it: eigen remote install %s", target, target)
		}
		return c, nil
	}
}

// dialRemoteStream opens a DEDICATED ssh connection for streaming one remote
// session's events (the pump). Attach is a blocking stream that can't be
// multiplexed onto the shared control client, so each subscribed remote session
// dials its own connection — exactly as local pumps use their own connection.
// The returned buffer captures ssh stderr so a failed Attach can report why.
// daemon.Client.Close() closes the underlying ssh conn.
func dialRemoteStream(target string) (*daemon.Client, *bytes.Buffer, error) {
	errBuf := &bytes.Buffer{}
	c, _, err := remote.Dial(target, errBuf)
	if err != nil {
		return nil, nil, err
	}
	return c, errBuf, nil
}
