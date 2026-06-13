// Package remote holds the transport-agnostic helpers for Tier 19 remote use:
// parsing host specs, mapping `uname` output to Go build targets, and deciding
// how to get an eigen binary onto a remote host. The ssh/scp orchestration that
// uses these lives in package main (remote.go); keeping the pure logic here
// makes it unit-testable without a live sshd.
package remote

import (
	"fmt"
	"strings"
)

// HostSpec is a parsed `[user@]host[:dir]` target. Host is whatever ssh
// understands (a hostname, an IP, or a ~/.ssh/config alias); Dir is an optional
// remote working directory for a session (empty = the remote's default).
type HostSpec struct {
	User string // optional; empty = ssh's default (current user / config)
	Host string // hostname / IP / ssh alias (never empty for a valid spec)
	Dir  string // optional remote session root
}

// SSHTarget is the `[user@]host` string to hand to ssh.
func (h HostSpec) SSHTarget() string {
	if h.User != "" {
		return h.User + "@" + h.Host
	}
	return h.Host
}

// ParseHostSpec parses `[user@]host[:dir]`. The optional `:dir` is recognized
// only when the part after the colon looks like a path (starts with /, ~, or
// .) so it never swallows an ssh alias that happens to contain a colon. A bare
// alias from ~/.ssh/config (no user, no dir) is valid.
func ParseHostSpec(s string) (HostSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return HostSpec{}, fmt.Errorf("empty host spec (want user@host[:dir])")
	}
	var h HostSpec
	if at := strings.Index(s, "@"); at >= 0 {
		h.User = s[:at]
		s = s[at+1:]
		if h.User == "" {
			return HostSpec{}, fmt.Errorf("empty user before @ in host spec")
		}
	}
	// Split an optional :dir, but only when it looks like a path — otherwise a
	// colon belongs to the host (rare, but don't mis-split).
	if c := strings.Index(s, ":"); c >= 0 {
		rest := s[c+1:]
		if rest != "" && (rest[0] == '/' || rest[0] == '~' || rest[0] == '.') {
			h.Dir = rest
			s = s[:c]
		}
	}
	h.Host = strings.TrimSpace(s)
	if h.Host == "" {
		return HostSpec{}, fmt.Errorf("missing host in spec (want user@host[:dir])")
	}
	return h, nil
}

// Target is a Go build target (GOOS/GOARCH).
type Target struct {
	GOOS   string
	GOARCH string
}

func (t Target) String() string { return t.GOOS + "/" + t.GOARCH }

// TargetFromUname maps `uname -s -m` output (e.g. "Linux x86_64") to a Go
// build target. Refuses unknown OS/arch rather than guessing — a wrong binary
// is worse than a clear error.
func TargetFromUname(uname string) (Target, error) {
	fields := strings.Fields(strings.TrimSpace(uname))
	if len(fields) < 2 {
		return Target{}, fmt.Errorf("unexpected `uname -sm` output: %q", uname)
	}
	goos, err := unameOS(fields[0])
	if err != nil {
		return Target{}, err
	}
	goarch, err := unameArch(fields[1])
	if err != nil {
		return Target{}, err
	}
	return Target{GOOS: goos, GOARCH: goarch}, nil
}

func unameOS(s string) (string, error) {
	switch strings.ToLower(s) {
	case "linux":
		return "linux", nil
	case "darwin":
		return "darwin", nil
	case "freebsd":
		return "freebsd", nil
	default:
		return "", fmt.Errorf("unsupported remote OS %q (eigen supports linux, darwin, freebsd)", s)
	}
}

func unameArch(s string) (string, error) {
	switch strings.ToLower(s) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported remote arch %q (eigen supports x86_64/amd64, aarch64/arm64)", s)
	}
}

// Action is how to obtain the eigen binary for a remote target.
type Action int

const (
	// CopyRunning: remote target matches the local build, so scp the
	// already-running binary (no compile, no source needed). The common case.
	CopyRunning Action = iota
	// CrossCompile: remote target differs from local, so `go build` with
	// GOOS/GOARCH from source, then scp. Requires the source tree + toolchain.
	CrossCompile
)

func (a Action) String() string {
	if a == CrossCompile {
		return "cross-compile"
	}
	return "copy-running-binary"
}

// PlanBootstrap decides how to get a binary for `remote` onto the host given
// the `local` build target and whether the source tree is available for
// cross-compiling. Same target → copy the running binary (always possible).
// Different target → cross-compile, which needs source.
func PlanBootstrap(local, remote Target, srcAvailable bool) (Action, error) {
	if local == remote {
		return CopyRunning, nil
	}
	if !srcAvailable {
		return 0, fmt.Errorf(
			"remote is %s but local eigen is %s; cross-compiling needs the source tree "+
				"(set EIGEN_SRC or run from a checkout)", remote, local)
	}
	return CrossCompile, nil
}
