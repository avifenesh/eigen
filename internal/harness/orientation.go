package harness

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const orientationSourceDir = "embedded/orientation"

// OrientationHome is the persistent Eigen-owned orientation engine + state home.
func OrientationHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "orientation")
}

// OrientationInstalled reports whether the Eigen-bundled orientation engine has
// been materialized into ~/.eigen/orientation.
func OrientationInstalled() bool {
	home := OrientationHome()
	if home == "" {
		return false
	}
	for _, f := range []string{"consume.js", "hook.js", "state.js"} {
		if _, err := os.Stat(filepath.Join(home, f)); err != nil {
			return false
		}
	}
	return true
}

// InstallOrientation installs the embedded orientation engine and an `orientation`
// compatibility wrapper. Existing data/ and projects.txt are preserved; engine
// files are refreshed from Eigen's embedded copy.
func InstallOrientation(eigenBin, dstDir string) error {
	home := OrientationHome()
	if home == "" {
		return os.ErrNotExist
	}
	if dstDir == "" {
		userHome, _ := os.UserHomeDir()
		dstDir = filepath.Join(userHome, ".local", "bin")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(home, 0o700)
	prefix := orientationSourceDir + "/"
	if err := fs.WalkDir(SourceFS, orientationSourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, prefix)
		// Preserve user state/config. The example is copied below only when no
		// projects.txt exists yet.
		if rel == "projects.txt" || rel == "projects.txt.example" {
			return nil
		}
		return copyEmbeddedFile(path, filepath.Join(home, filepath.FromSlash(rel)))
	}); err != nil {
		return err
	}
	projects := filepath.Join(home, "projects.txt")
	if _, err := os.Stat(projects); os.IsNotExist(err) {
		if err := copyEmbeddedFile(filepath.Join(orientationSourceDir, "projects.txt.example"), projects); err != nil {
			return err
		}
	}
	if err := installOrientationWrapper(eigenBin, dstDir); err != nil {
		return err
	}
	return nil
}

func copyEmbeddedFile(src, dst string) error {
	in, err := SourceFS.Open(filepath.ToSlash(src))
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := fs.Stat(SourceFS, filepath.ToSlash(src))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	ok = true
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
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
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
	if err := os.Rename(tmpName, dst); err != nil {
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

// RunOrientation executes the bundled orientation CLI. It prefers the installed
// ~/.eigen/orientation engine, and falls back to a temporary materialized engine
// while keeping state in ~/.eigen/orientation.
func RunOrientation(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "install" {
		return runOrientationScript(ctx, "", args)
	}
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node not found — install Node.js or run `eigen orientation install` after Node is available")
	}
	engine := OrientationHome()
	cleanup := func() {}
	if !OrientationInstalled() {
		root, c, err := materializeOrientationEngine()
		if err != nil {
			return err
		}
		engine, cleanup = root, c
	}
	defer cleanup()
	return runOrientationScript(ctx, engine, args)
}

func materializeOrientationEngine() (string, func(), error) {
	tmp, err := os.MkdirTemp("", "eigen-orientation-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	root := filepath.Join(tmp, "orientation")
	prefix := orientationSourceDir + "/"
	err = fs.WalkDir(SourceFS, orientationSourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, strings.TrimPrefix(path, orientationSourceDir)), 0o755)
		}
		rel := strings.TrimPrefix(path, prefix)
		return copyEmbeddedFile(path, filepath.Join(root, filepath.FromSlash(rel)))
	})
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return root, cleanup, nil
}

// InstallOrientationHooks installs Eigen turn/session hooks that call the
// built-in orientation wrapper, replacing legacy action-graph/orientation hook
// commands when present.
func InstallOrientationHooks(ctx context.Context) error {
	if _, err := exec.LookPath("node"); err != nil {
		return err
	}
	if !OrientationInstalled() {
		return fmt.Errorf("orientation is not installed")
	}
	return runOrientationScript(ctx, OrientationHome(), []string{"hooks", "install", "--runtime", "eigen"})
}

func runOrientationScript(ctx context.Context, engine string, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printOrientationUsage()
		return nil
	}
	cmdName, rest := args[0], args[1:]
	var cmd *exec.Cmd
	switch cmdName {
	case "install":
		exe, _ := os.Executable()
		if err := InstallOrientation(exe, ""); err != nil {
			return err
		}
		fmt.Println("orientation installed →", OrientationHome())
		if err := InstallOrientationHooks(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "orientation hooks not installed:", err)
		}
		return nil
	case "refresh":
		cmd = exec.CommandContext(ctx, "bash", filepath.Join(engine, "refresh.sh"))
	case "provenance":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "consume.js"), "--provenance"}, rest...)...)
	case "related":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "consume.js"), "--related"}, rest...)...)
	case "query":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "consume.js"), "--query"}, rest...)...)
	case "threads":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "graph.js"), "--threads"}, rest...)...)
	case "coupled":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "graph.js"), "--coupled"}, rest...)...)
	case "status":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "status.js")}, rest...)...)
	case "doctor":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "doctor.js")}, rest...)...)
	case "hook":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "hook.js")}, rest...)...)
	case "hooks":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "hooks.js")}, rest...)...)
	case "ingest":
		cmd = exec.CommandContext(ctx, "node", append([]string{filepath.Join(engine, "ingest.js")}, rest...)...)
	default:
		return fmt.Errorf("unknown orientation command %q", cmdName)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = orientationEnv(os.Environ(), engine)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("orientation %s: %w", cmdName, err)
	}
	return nil
}

func orientationEnv(base []string, engine string) []string {
	home := OrientationHome()
	out := make([]string, 0, len(base)+3)
	for _, kv := range base {
		if strings.HasPrefix(kv, "ORIENTATION_HOME=") || strings.HasPrefix(kv, "ORIENTATION_ENGINE_DIR=") || strings.HasPrefix(kv, "EIGEN_ORIENTATION_HOME=") || strings.HasPrefix(kv, "EIGEN_ORIENTATION_DIR=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "ORIENTATION_HOME="+home, "EIGEN_ORIENTATION_HOME="+home, "ORIENTATION_ENGINE_DIR="+engine)
	return out
}

func printOrientationUsage() {
	fmt.Println(`orientation — built-in Eigen harness history lookup

usage:
  eigen orientation install                 install engine + orientation wrapper
  eigen orientation refresh                 rebuild graphs now
  eigen orientation provenance <cwd> <file> history + verdict for a file
  eigen orientation related    <cwd> <file> prior work + sibling files
  eigen orientation threads    <cwd>        resume threads across detours
  eigen orientation coupled    <cwd> <file> files co-edited with this one
  eigen orientation status [cwd]            summarize indexed projects
  eigen orientation doctor [cwd]            inspect hooks/state
  eigen orientation hook [--runtime eigen]  run the turn/session hook
  eigen orientation hooks <action> ...      manage orientation hooks

State lives under ~/.eigen/orientation. The engine source is bundled inside Eigen
and installed by 'eigen harness install' or 'eigen orientation install'.`)
}
