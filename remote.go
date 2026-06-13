package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/avifenesh/eigen/internal/remote"
)

// runRemoteCmd handles `eigen remote <install|run> ...`.
func runRemoteCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: eigen remote install <user@host>")
		os.Exit(2)
	}
	switch args[0] {
	case "install":
		if len(args) < 2 {
			fail(fmt.Errorf("usage: eigen remote install <user@host[:dir]>"))
		}
		remoteInstall(args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown remote subcommand %q (want: install)\n", args[0])
		os.Exit(2)
	}
}

// sshArgs are the base flags for every ssh invocation: no pty (-T) so the byte
// stream isn't mangled by tty line discipline or `~.` escapes, and a keepalive
// so idle remote sessions don't silently drop. The user's ~/.ssh/config still
// applies (and wins for anything it sets).
func sshArgs(extra ...string) []string {
	base := []string{"-T", "-o", "ServerAliveInterval=15"}
	return append(base, extra...)
}

// remoteInstall bootstraps eigen onto a host that does NOT have it: detect the
// remote OS/arch, obtain a matching binary (copy the running one when the
// target matches, else cross-compile from source), scp it to
// ~/.local/bin/eigen, and verify it runs.
func remoteInstall(spec string) {
	h, err := remote.ParseHostSpec(spec)
	if err != nil {
		fail(err)
	}
	target := h.SSHTarget()
	fmt.Printf("eigen remote install → %s\n", target)

	// 1. Detect remote OS/arch.
	out, err := exec.Command("ssh", sshArgs(target, "uname -sm")...).Output()
	if err != nil {
		fail(fmt.Errorf("ssh %s uname: %w (check connectivity / ~/.ssh/config)", target, err))
	}
	remoteTarget, err := remote.TargetFromUname(string(out))
	if err != nil {
		fail(err)
	}
	localTarget := remote.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	fmt.Printf("  remote: %s   local: %s\n", remoteTarget, localTarget)

	// 2. Decide how to get a binary.
	srcDir, srcOK := eigenSourceDir()
	action, err := remote.PlanBootstrap(localTarget, remoteTarget, srcOK)
	if err != nil {
		fail(err)
	}
	fmt.Printf("  plan: %s\n", action)

	// 3. Produce the binary to ship.
	var binPath string
	var cleanup func()
	switch action {
	case remote.CopyRunning:
		binPath, err = os.Executable()
		if err != nil {
			fail(fmt.Errorf("locate running binary: %w", err))
		}
	case remote.CrossCompile:
		binPath, cleanup, err = crossCompile(srcDir, remoteTarget)
		if err != nil {
			fail(err)
		}
		defer cleanup()
	}

	// 4. Ensure ~/.local/bin exists remotely, then scp the binary in place.
	//    scp to a temp name + atomic mv so a half-copied binary is never run.
	if err := runSSH(target, "mkdir -p ~/.local/bin"); err != nil {
		fail(fmt.Errorf("remote mkdir: %w", err))
	}
	remoteTmp := "~/.local/bin/.eigen.upload"
	remoteFinal := "~/.local/bin/eigen"
	fmt.Printf("  copying %s → %s:%s\n", filepath.Base(binPath), target, remoteFinal)
	if err := scpTo(binPath, target, remoteTmp); err != nil {
		fail(fmt.Errorf("scp: %w", err))
	}
	if err := runSSH(target, fmt.Sprintf("chmod 755 %s && mv -f %s %s", remoteTmp, remoteTmp, remoteFinal)); err != nil {
		fail(fmt.Errorf("install binary: %w", err))
	}

	// 5. Verify it runs on the remote.
	ver, err := exec.Command("ssh", sshArgs(target, "~/.local/bin/eigen version")...).Output()
	if err != nil {
		fmt.Printf("  installed, but `eigen version` failed remotely: %v\n", err)
		fmt.Println("  (ensure ~/.local/bin is on the remote PATH)")
	} else {
		fmt.Printf("  installed: eigen %s on %s\n", strings.TrimSpace(string(ver)), target)
	}
	fmt.Printf("\nNow run a remote session with:\n  eigen --remote %s\n", spec)
}

// runSSH runs a remote command, streaming its stderr (for visibility).
func runSSH(target, cmd string) error {
	c := exec.Command("ssh", sshArgs(target, cmd)...)
	c.Stderr = os.Stderr
	return c.Run()
}

// scpTo copies a local file to target:dest via scp.
func scpTo(local, target, dest string) error {
	c := exec.Command("scp", "-q", local, target+":"+dest)
	c.Stderr = os.Stderr
	return c.Run()
}

// eigenSourceDir resolves the source tree for cross-compiling: $EIGEN_SRC, else
// ~/projects/eigen, when it contains a go.mod. Returns (dir, ok).
func eigenSourceDir() (string, bool) {
	src := os.Getenv("EIGEN_SRC")
	if src == "" {
		home, _ := os.UserHomeDir()
		src = filepath.Join(home, "projects", "eigen")
	}
	if _, err := os.Stat(filepath.Join(src, "go.mod")); err == nil {
		return src, true
	}
	return "", false
}

// crossCompile builds eigen for `t` from srcDir into a temp file; returns the
// binary path and a cleanup func.
func crossCompile(srcDir string, t remote.Target) (string, func(), error) {
	tmp, err := os.CreateTemp("", "eigen-"+t.GOOS+"-"+t.GOARCH+"-*")
	if err != nil {
		return "", nil, err
	}
	tmp.Close()
	cleanup := func() { os.Remove(tmp.Name()) }

	gobin := devFindGo()
	fmt.Printf("  cross-compiling (%s) for %s …\n", gobin, t)
	c := exec.Command(gobin, "build", "-o", tmp.Name(), ".")
	c.Dir = srcDir
	c.Env = append(os.Environ(), "GOOS="+t.GOOS, "GOARCH="+t.GOARCH, "CGO_ENABLED=0")
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("cross-compile for %s: %w", t, err)
	}
	return tmp.Name(), cleanup, nil
}
