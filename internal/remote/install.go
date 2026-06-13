package remote

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// InstallOpts configures a remote bootstrap.
type InstallOpts struct {
	// LocalBinary is the path to an eigen binary that runs on the REMOTE arch
	// (the caller picks: the running binary when arch matches, or a freshly
	// cross-compiled one). Required.
	LocalBinary string
	// PushCreds pushes ~/.eigen/daemon.env (env snapshot) + ~/.aws/credentials
	// so the remote daemon can authenticate. The caller supplies the env
	// snapshot lines (KEY=VALUE) since the key list lives in package main.
	PushCreds   bool
	EnvSnapshot string // the daemon.env content to push (empty = none)
	// Progress receives human-readable step lines (for a CLI or a TUI panel).
	Progress func(string)
}

// Install bootstraps eigen onto target over ssh/scp: scp the binary to
// ~/.local/bin/eigen (temp + atomic mv), verify it runs, and optionally push
// credentials (daemon.env + ~/.aws/credentials, 0600). Pure orchestration —
// no stdout; progress goes to opts.Progress. Returns the remote `eigen version`
// string on success.
func Install(target string, opts InstallOpts) (version string, err error) {
	step := func(s string) {
		if opts.Progress != nil {
			opts.Progress(s)
		}
	}
	if opts.LocalBinary == "" {
		return "", fmt.Errorf("no binary to install")
	}
	remoteLog("install %s: binary=%s pushCreds=%v", target, opts.LocalBinary, opts.PushCreds)

	step("ensuring ~/.local/bin on " + target)
	if err := runSSHQuiet(target, "mkdir -p ~/.local/bin"); err != nil {
		return "", fmt.Errorf("remote mkdir: %w", err)
	}

	step("copying eigen binary")
	remoteTmp := "~/.local/bin/.eigen.upload"
	remoteFinal := "~/.local/bin/eigen"
	if err := scpQuiet(opts.LocalBinary, target, remoteTmp); err != nil {
		return "", fmt.Errorf("scp: %w", err)
	}
	if err := runSSHQuiet(target, fmt.Sprintf("chmod 755 %s && mv -f %s %s", remoteTmp, remoteTmp, remoteFinal)); err != nil {
		return "", fmt.Errorf("install binary: %w", err)
	}

	step("verifying")
	out, verr := exec.Command("ssh", SSHArgs(target, "~/.local/bin/eigen version")...).Output()
	if verr != nil {
		// Installed, but PATH may not include ~/.local/bin for a non-login
		// shell. The binary is still there; report a soft warning.
		step("installed (could not verify `eigen version` — ensure ~/.local/bin is on the remote PATH)")
	} else {
		version = strings.TrimSpace(string(out))
		step("installed: " + version)
	}

	if opts.PushCreds {
		if err := pushCreds(target, opts.EnvSnapshot, step); err != nil {
			step("credentials NOT pushed (" + err.Error() + ") — remote sessions may fail to authenticate")
		}
	}
	return version, nil
}

// pushCreds writes the env snapshot to ~/.eigen/daemon.env and the local
// ~/.aws/credentials to the remote ~/.aws/credentials, both 0600, piped over
// ssh (no temp files, no secrets on argv).
func pushCreds(target, envSnapshot string, step func(string)) error {
	hasBearer := strings.Contains(envSnapshot, "AWS_BEARER_TOKEN_BEDROCK=")
	if strings.TrimSpace(envSnapshot) != "" {
		cmd := exec.Command("ssh", SSHArgs(target, "umask 077 && mkdir -p ~/.eigen && cat > ~/.eigen/daemon.env && chmod 600 ~/.eigen/daemon.env")...)
		cmd.Stdin = strings.NewReader(envSnapshot)
		if err := cmd.Run(); err != nil {
			return err
		}
		step("pushed credential snapshot → ~/.eigen/daemon.env (0600)")
	}
	// The Bedrock bearer token (in the snapshot) drives the default Converse
	// model on its own — no AWS file needed. Only fall back to copying
	// ~/.aws/credentials when there's NO bearer token (SigV4 is the only option
	// then). Avoids putting real AWS keys on the remote in the common case.
	if !hasBearer {
		if home, err := os.UserHomeDir(); err == nil {
			if data, rerr := os.ReadFile(filepath.Join(home, ".aws", "credentials")); rerr == nil && len(data) > 0 {
				cmd := exec.Command("ssh", SSHArgs(target, "umask 077 && mkdir -p ~/.aws && cat > ~/.aws/credentials && chmod 600 ~/.aws/credentials")...)
				cmd.Stdin = bytes.NewReader(data)
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("push ~/.aws/credentials: %w", err)
				}
				step("pushed ~/.aws/credentials → ~/.aws/credentials (0600; no bearer token available)")
			}
		}
	}
	return nil
}

// remoteLog appends a timestamped line to ~/.eigen/remote.log — a durable trace
// of remote ops (install/list) so TUI failures (where stderr is hidden) are
// diagnosable after the fact. Best-effort.
func remoteLog(format string, args ...any) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(home, ".eigen", "remote.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, args...)...)
}

// runSSHQuiet runs a remote command, discarding output (errors carry context).
func runSSHQuiet(target, cmd string) error {
	c := exec.Command("ssh", SSHArgs(target, cmd)...)
	var errb bytes.Buffer
	c.Stderr = &errb
	if err := c.Run(); err != nil {
		if s := strings.TrimSpace(errb.String()); s != "" {
			remoteLog("ssh %s %q FAILED: %v: %s", target, cmd, err, s)
			return fmt.Errorf("%s: %s", err, firstLine(s))
		}
		remoteLog("ssh %s %q FAILED: %v", target, cmd, err)
		return err
	}
	return nil
}

// scpQuiet copies a local file to target:dest.
func scpQuiet(local, target, dest string) error {
	c := exec.Command("scp", "-q", local, target+":"+dest)
	var errb bytes.Buffer
	c.Stderr = &errb
	if err := c.Run(); err != nil {
		if s := strings.TrimSpace(errb.String()); s != "" {
			remoteLog("scp %s -> %s FAILED: %v: %s", local, dest, err, s)
			return fmt.Errorf("%s: %s", err, firstLine(s))
		}
		remoteLog("scp %s -> %s FAILED: %v", local, dest, err)
		return err
	}
	return nil
}

// DetectArch returns the remote's Go build target via `ssh target uname -sm`.
func DetectArch(target string) (Target, error) {
	out, err := exec.Command("ssh", SSHArgs(target, "uname -sm")...).Output()
	if err != nil {
		remoteLog("ssh %s uname FAILED: %v (out=%q)", target, err, string(out))
		return Target{}, fmt.Errorf("ssh %s uname: %w (check connectivity / ~/.ssh/config)", target, err)
	}
	return TargetFromUname(string(out))
}

// LocalTarget is this machine's Go build target.
func LocalTarget() Target {
	return Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

// CredentialKeys are the environment variables a remote daemon needs that an
// ssh non-login shell won't have (provider credentials + tuning). The single
// source of truth, shared by the CLI and the app's one-click install.
var CredentialKeys = []string{
	"AWS_BEARER_TOKEN_BEDROCK", "AWS_REGION", "AWS_PROFILE",
	"XAI_API_KEY", "EIGEN_GROK_API_KEY", "GLM_API_KEY", "ANTHROPIC_API_KEY",
	"EIGEN_SMALL_MODEL", "EIGEN_TITLE_MODEL", "EIGEN_LLAMA_BASE_URL",
	"EIGEN_MANTLE_REGION", "EIGEN_REASONING_EFFORT", "EIGEN_NOTIFY_CMD",
}

// CredentialSnapshot builds the KEY=VALUE daemon.env content from the current
// environment (only set vars).
func CredentialSnapshot() string {
	var b strings.Builder
	for _, k := range CredentialKeys {
		if v := os.Getenv(k); v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	return b.String()
}

// RunningBinaryInstallable reports whether the LOCAL running binary can be
// directly installed on target (same arch → no cross-compile needed). The app
// uses this to decide whether one-click install works or the user must run the
// CLI (which can cross-compile from source).
func RunningBinaryInstallable(target string) (bool, error) {
	rt, err := DetectArch(target)
	if err != nil {
		return false, err
	}
	return rt == LocalTarget(), nil
}
