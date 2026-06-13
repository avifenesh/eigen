package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
			fail(fmt.Errorf("usage: eigen remote install <user@host[:dir] | saved-name> [--no-creds]"))
		}
		// --no-creds (anywhere in the args) skips the credential push.
		pushCreds := true
		var spec string
		for _, a := range args[1:] {
			switch a {
			case "--no-creds":
				pushCreds = false
			default:
				if spec == "" {
					spec = a
				}
			}
		}
		if spec == "" {
			fail(fmt.Errorf("usage: eigen remote install <user@host[:dir] | saved-name> [--no-creds]"))
		}
		remoteInstall(spec, pushCreds)
	case "list", "ls":
		remoteList()
	case "add":
		// eigen remote add <name> <user@host[:dir]> [model] [perm]
		if len(args) < 3 {
			fail(fmt.Errorf("usage: eigen remote add <name> <user@host[:dir]> [model] [perm]"))
		}
		remoteAdd(args[1:])
	case "remove", "rm":
		if len(args) < 2 {
			fail(fmt.Errorf("usage: eigen remote remove <name>"))
		}
		remoteRemove(args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown remote subcommand %q (want: install | list | add | remove)\n", args[0])
		os.Exit(2)
	}
}

// remoteList prints saved hosts from ~/.eigen/hosts.json.
func remoteList() {
	hosts, err := remote.LoadHosts()
	if err != nil {
		fail(err)
	}
	if len(hosts) == 0 {
		fmt.Println("no saved hosts (add one: eigen remote add <name> <user@host[:dir]>)")
		return
	}
	for name, h := range hosts {
		line := fmt.Sprintf("%-16s %s", name, h.SSH)
		if h.Dir != "" {
			line += ":" + h.Dir
		}
		if h.Model != "" {
			line += "  model=" + h.Model
		}
		if h.Perm != "" {
			line += "  perm=" + h.Perm
		}
		fmt.Println(line)
	}
}

// remoteAdd saves a host to ~/.eigen/hosts.json.
func remoteAdd(args []string) {
	name, ssh := args[0], args[1]
	// Validate the ssh spec parses.
	if _, err := remote.ParseHostSpec(ssh); err != nil {
		fail(err)
	}
	h := remote.Host{SSH: ssh}
	// Pull an inline :dir out of the spec into the saved Dir.
	if s, err := remote.ParseHostSpec(ssh); err == nil && s.Dir != "" {
		h.SSH = s.SSHTarget()
		h.Dir = s.Dir
	}
	if len(args) > 2 {
		h.Model = args[2]
	}
	if len(args) > 3 {
		h.Perm = args[3]
	}
	hosts, err := remote.LoadHosts()
	if err != nil {
		fail(err)
	}
	hosts[name] = h
	if err := remote.SaveHosts(hosts); err != nil {
		fail(err)
	}
	fmt.Printf("saved host %q → %s\n", name, ssh)
}

// remoteRemove deletes a saved host.
func remoteRemove(name string) {
	hosts, err := remote.LoadHosts()
	if err != nil {
		fail(err)
	}
	if _, ok := hosts[name]; !ok {
		fail(fmt.Errorf("no saved host %q", name))
	}
	delete(hosts, name)
	if err := remote.SaveHosts(hosts); err != nil {
		fail(err)
	}
	fmt.Printf("removed host %q\n", name)
}

// remoteInstall bootstraps eigen onto a host that does NOT have it: detect the
// remote OS/arch, obtain a matching binary (copy the running one when the
// target matches, else cross-compile from source), scp it to
// ~/.local/bin/eigen, and verify it runs.
func remoteInstall(spec string, pushCreds bool) {
	// Allow a saved host name as well as a literal spec.
	hosts, _ := remote.LoadHosts()
	h, _, _, err := hosts.Resolve(spec)
	if err != nil {
		fail(err)
	}
	target := h.SSHTarget()
	fmt.Printf("eigen remote install → %s\n", target)

	binPath, cleanup, err := remoteBinaryFor(target)
	if err != nil {
		fail(err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	_, err = remote.Install(target, remote.InstallOpts{
		LocalBinary: binPath,
		PushCreds:   pushCreds,
		EnvSnapshot: credentialSnapshot(),
		Progress:    func(s string) { fmt.Println("  " + s) },
	})
	if err != nil {
		fail(err)
	}
	if !pushCreds {
		fmt.Println("  (--no-creds: skipped credential push; the remote needs its own provider creds)")
	}
	fmt.Printf("\nNow run a remote session with:\n  eigen --remote %s\n", spec)
}

// remoteBinaryFor returns an eigen binary that runs on target's arch: the
// running binary when the arch matches (no compile), else a freshly
// cross-compiled one (needs the source tree + go). cleanup removes a temp
// cross-compiled binary (nil for the copy-running case).
func remoteBinaryFor(target string) (binPath string, cleanup func(), err error) {
	remoteTarget, err := remote.DetectArch(target)
	if err != nil {
		return "", nil, err
	}
	localTarget := remote.LocalTarget()
	srcDir, srcOK := eigenSourceDir()
	action, err := remote.PlanBootstrap(localTarget, remoteTarget, srcOK)
	if err != nil {
		return "", nil, err
	}
	switch action {
	case remote.CopyRunning:
		exe, e := os.Executable()
		if e != nil {
			return "", nil, fmt.Errorf("locate running binary: %w", e)
		}
		return exe, nil, nil
	case remote.CrossCompile:
		return crossCompile(srcDir, remoteTarget)
	}
	return "", nil, fmt.Errorf("no install plan")
}

// credentialSnapshot builds the KEY=VALUE daemon.env content from the current
// environment (delegates to the remote package — the single source of truth).
func credentialSnapshot() string { return remote.CredentialSnapshot() }

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
