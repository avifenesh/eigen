package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/orientation"
)

// OrientationHome is the persistent Eigen-owned orientation state home. The
// engine itself is native Go inside Eigen; install only creates state/config,
// a compatibility wrapper, and hooks.
func OrientationHome() string { return orientation.DefaultPaths().Home }

// OrientationInstalled reports whether the wrapper/state directory have been
// installed for command-line and hook compatibility. The feature is available
// through `eigen orientation` even when this returns false.
func OrientationInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "orientation")); err != nil {
		return false
	}
	if _, err := os.Stat(orientation.DefaultPaths().Allowlist); err != nil {
		return false
	}
	return true
}

// InstallOrientation installs Eigen's native orientation harness integration:
// state directory, allowlist stub, a small `orientation` compatibility wrapper,
// and no separate engine/runtime package.
func InstallOrientation(eigenBin, dstDir string) error {
	if err := orientation.EnsureHome(); err != nil {
		return err
	}
	if dstDir == "" {
		userHome, _ := os.UserHomeDir()
		dstDir = filepath.Join(userHome, ".local", "bin")
	}
	if err := removeLegacyOrientationEngineFiles(); err != nil {
		return err
	}
	return installOrientationWrapper(eigenBin, dstDir)
}

func removeLegacyOrientationEngineFiles() error {
	// Preserve data/ and projects.txt; remove the old embedded-JS engine files so
	// a current install is genuinely Go-native and not a separate skill package.
	for _, name := range []string{"adapters.js", "classify.js", "condense.js", "consume.js", "doctor.js", "filters.js", "graph.js", "hook.js", "hooks.js", "ingest.js", "project.js", "refresh.sh", "state.js", "status.js"} {
		if err := os.Remove(filepath.Join(OrientationHome(), name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func installOrientationWrapper(eigenBin, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if eigenBin == "" {
		if p, err := os.Executable(); err == nil {
			eigenBin = p
		}
	}
	quotedEigen := shellQuote(eigenBin)
	body := fmt.Sprintf(`#!/bin/sh
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -x "$DIR/eigen" ]; then
  exec "$DIR/eigen" orientation "$@"
fi
if [ -n %s ] && [ -x %s ]; then
  exec %s orientation "$@"
fi
exec eigen orientation "$@"
`, quotedEigen, quotedEigen, quotedEigen)
	return writeExecutable(filepath.Join(dstDir, "orientation"), []byte(body))
}

func writeExecutable(dst string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// InstallOrientationHooks installs Eigen turn/session hooks that call the
// built-in orientation wrapper, replacing legacy action-graph/orientation hook
// commands when present.
func InstallOrientationHooks(ctx context.Context) error {
	home, _ := os.UserHomeDir()
	return orientation.InstallHooks(filepath.Join(home, ".local", "bin", "orientation"))
}

// RunOrientation executes the native Go orientation CLI.
func RunOrientation(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "install" {
		exe, _ := os.Executable()
		if err := InstallOrientation(exe, ""); err != nil {
			return err
		}
		fmt.Println("orientation installed →", OrientationHome())
		if err := InstallOrientationHooks(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "orientation hooks not installed:", err)
		}
		return nil
	}
	return orientation.RunCLI(ctx, args, os.Stdin, os.Stdout, os.Stderr)
}
