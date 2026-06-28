package gui

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
)

// TestSessionRefRoundTrip proves encodeRemoteRef and parseSessionRef are exact
// inverses for the ssh targets a remote session ref must survive: a bare alias,
// a user@host, a host:port (the colon is the case the parse MUST get right), a
// user@host:dir, and one with characters base64url must keep intact. A round
// trip through the route param / event name is identity (the router does no
// extra encode/decode), so the Go encode↔parse pair is the whole contract.
func TestSessionRefRoundTrip(t *testing.T) {
	cases := []struct {
		target string
		realID string
	}{
		{"dev", "s1"},
		{"ubuntu@10.0.0.4", "s12"},
		{"ubuntu@ec2-1-2-3-4:2222", "s3"},                  // host:port — colon in target
		{"ubuntu@host:/srv/project", "s7"},                 // user@host:dir
		{"user.name@some-host.internal.example.com", "s0"}, // dotted, hyphenated
	}
	for _, c := range cases {
		ref := encodeRemoteRef(c.target, c.realID)
		if !strings.HasPrefix(ref, remoteRefPrefix) {
			t.Fatalf("encoded ref %q missing %q prefix", ref, remoteRefPrefix)
		}
		// base64url (raw) must not introduce a URL-hostile byte.
		if strings.ContainsAny(ref[len(remoteRefPrefix):strings.LastIndex(ref, ":")], "+/=") {
			t.Fatalf("encoded target part of %q contains a URL-hostile char", ref)
		}
		target, realID, isRemote := parseSessionRef(ref)
		if !isRemote {
			t.Fatalf("parseSessionRef(%q) isRemote=false, want true", ref)
		}
		if target != c.target {
			t.Errorf("target round-trip: got %q want %q", target, c.target)
		}
		if realID != c.realID {
			t.Errorf("realID round-trip: got %q want %q", realID, c.realID)
		}
	}
}

// TestParseSessionRefLocal: a bare local id is not remote and passes through
// unchanged as the real id (every existing local-session code path keeps
// working untouched).
func TestParseSessionRefLocal(t *testing.T) {
	for _, id := range []string{"s1", "s42", "abc", ""} {
		target, realID, isRemote := parseSessionRef(id)
		if isRemote {
			t.Errorf("parseSessionRef(%q) isRemote=true, want false", id)
		}
		if target != "" {
			t.Errorf("parseSessionRef(%q) target=%q, want empty", id, target)
		}
		if realID != id {
			t.Errorf("parseSessionRef(%q) realID=%q, want unchanged", id, realID)
		}
	}
}

// TestParseSessionRefMalformed: a value that merely starts with the prefix but
// is garbled (bad base64, missing pieces) must degrade to a LOCAL lookup rather
// than panic — it'll simply 404 against the local daemon. A hostile route param
// can't crash the bridge.
func TestParseSessionRefMalformed(t *testing.T) {
	bad := []string{
		"remote:",            // nothing after prefix
		"remote:onlyonepart", // no second colon
		"remote::s1",         // empty target encoding
		"remote:not_base64!@#:s1",
		"remote:" + "Zm9v" + ":", // valid b64 target but empty realID
	}
	for _, ref := range bad {
		target, realID, isRemote := parseSessionRef(ref)
		if isRemote {
			t.Errorf("parseSessionRef(%q) isRemote=true, want false (malformed → local)", ref)
		}
		if target != "" {
			t.Errorf("parseSessionRef(%q) target=%q, want empty", ref, target)
		}
		if realID != ref {
			t.Errorf("parseSessionRef(%q) realID=%q, want the ref verbatim", ref, realID)
		}
	}
}

// TestClientForLocalUsesControl: a local id resolves through the local control
// client and returns the id unchanged. With an ensure() that hands back a nil
// client + nil error (the test-bridge convention used across this package),
// clientFor takes the LOCAL branch — same (nil) client control() yields and the
// real id passed through verbatim — proving a bare id never triggers a remote
// ssh dial.
func TestClientForLocalUsesControl(t *testing.T) {
	b := NewBridge(func() (*daemon.Client, error) { return nil, nil }, nil, nil)
	c, realID, err := b.clientFor("s7")
	if err != nil {
		t.Fatalf("clientFor(local) err = %v, want nil", err)
	}
	if c != nil {
		t.Fatalf("clientFor(local) client = %v, want the (nil) control client", c)
	}
	if realID != "s7" {
		t.Fatalf("clientFor(local) realID = %q, want %q unchanged", realID, "s7")
	}
	// No remote control client should have been created for a local id.
	if len(b.remoteCtrls) != 0 {
		t.Fatalf("clientFor(local) created %d remote control client(s), want 0", len(b.remoteCtrls))
	}
}
