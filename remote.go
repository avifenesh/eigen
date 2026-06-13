package main

import (
	"bytes"
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

// sshArgs delegates to remote.SSHArgs (single source of truth for the base ssh
// flags: -T no-pty + keepalive).
func sshArgs(extra ...string) []string {
	return remote.SSHArgs(extra...)
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
		fmt.Printf("  installed: %s on %s\n", strings.TrimSpace(string(ver)), target)
	}

	// Push the credential snapshot so the remote daemon can actually build
	// sessions (an ssh non-login shell has no AWS/provider creds → otherwise
	// every remote turn fails with "converse credentials"). The remote daemon
	// loads ~/.eigen/daemon.env at startup (loadDaemonEnv). Skipped with
	// --no-creds; secrets stay 0600 on both ends.
	if pushCreds {
		if err := pushRemoteCreds(target); err != nil {
			fmt.Printf("  credentials NOT pushed (%v) — remote sessions may fail to authenticate;\n  set creds on the remote or re-run without --no-creds\n", err)
		} else {
			fmt.Printf("  pushed credential snapshot → %s:~/.eigen/daemon.env (0600)\n", target)
		}
	} else {
		fmt.Println("  (--no-creds: skipped credential push; the remote needs its own provider creds)")
	}

	fmt.Printf("\nNow run a remote session with:\n  eigen --remote %s\n", spec)
}

// pushRemoteCreds writes the local credential snapshot (the same keys
// `eigen daemon install` captures) to the remote's ~/.eigen/daemon.env, 0600,
// AND — because the default Converse/Bedrock model authenticates with SigV4
// from ~/.aws/credentials (not an env token) — pushes the AWS credentials file
// too when present. All piped over ssh (never a local temp, never secrets on a
// command line); the remote files are created 0600.
func pushRemoteCreds(target string) error {
	var b strings.Builder
	n := 0
	for _, k := range credentialEnvKeys {
		if v := os.Getenv(k); v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
			n++
		}
	}
	if n > 0 {
		remoteCmd := "umask 077 && mkdir -p ~/.eigen && cat > ~/.eigen/daemon.env && chmod 600 ~/.eigen/daemon.env"
		cmd := exec.Command("ssh", sshArgs(target, remoteCmd)...)
		cmd.Stdin = strings.NewReader(b.String())
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	// AWS credentials file (SigV4 for Converse/Bedrock — the default model).
	// Without it a remote converse session fails with "converse credentials".
	if home, err := os.UserHomeDir(); err == nil {
		credFile := filepath.Join(home, ".aws", "credentials")
		if data, rerr := os.ReadFile(credFile); rerr == nil && len(data) > 0 {
			remoteCmd := "umask 077 && mkdir -p ~/.aws && cat > ~/.aws/credentials && chmod 600 ~/.aws/credentials"
			cmd := exec.Command("ssh", sshArgs(target, remoteCmd)...)
			cmd.Stdin = bytes.NewReader(data)
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("push ~/.aws/credentials: %w", err)
			}
			fmt.Printf("  pushed ~/.aws/credentials → %s:~/.aws/credentials (0600)\n", target)
		}
	}

	if n == 0 {
		return fmt.Errorf("no env credentials in the local environment to push")
	}
	return nil
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
