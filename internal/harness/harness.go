package harness

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SourceFS contains the built-in harness helper sources. Keeping these sources
// inside eigen means computer-use/workspace install does not depend on sibling
// checkouts such as ~/projects/computer-use-linux or ~/projects/agent-workspace-linux.
// Cargo/Rust/system desktop packages are still only touched when the user
// explicitly runs an install/build command.
//
//go:embed embedded/computer-use-linux/** embedded/agent-workspace-linux/** embedded/chrome-bridge/**
var SourceFS embed.FS

type Component struct {
	Name        string
	SourceDir   string
	Description string
	Binaries    []string
}

var Components = map[string]Component{
	"computer-use": {
		Name:        "computer-use",
		SourceDir:   "embedded/computer-use-linux",
		Description: "real Linux desktop computer-use MCP server",
		Binaries:    []string{"computer-use-linux", "computer-use-linux-cosmic"},
	},
	"workspace": {
		Name:        "workspace",
		SourceDir:   "embedded/agent-workspace-linux",
		Description: "isolated agent workspace MCP server",
		Binaries:    []string{"agent-workspace-linux"},
	},
}

func ComponentNames() []string { return []string{"computer-use", "workspace"} }

// Install builds a bundled component from embedded source and copies its release
// binaries into dstDir. It intentionally shells out to cargo only when the user
// requested installation; normal eigen builds/tests do not require Rust.
func Install(ctx context.Context, name, dstDir string) error {
	c, ok := Components[name]
	if !ok {
		return fmt.Errorf("unknown harness component %q", name)
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		return fmt.Errorf("cargo not found — install Rust, then rerun `eigen harness install`")
	}
	if dstDir == "" {
		home, _ := os.UserHomeDir()
		dstDir = filepath.Join(home, ".local", "bin")
	}
	root, cleanup, err := Materialize(c)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, "cargo", "build", "--release", "--locked", "--manifest-path", filepath.Join(root, "Cargo.toml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo build %s: %w", name, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	for _, bin := range c.Binaries {
		src := filepath.Join(root, "target", "release", bin)
		dst := filepath.Join(dstDir, bin)
		if err := copyExecutable(src, dst); err != nil {
			return fmt.Errorf("install %s: %w", bin, err)
		}
	}
	return nil
}

// Materialize writes a component's embedded source tree to a temporary directory
// and returns that component root plus a cleanup function.
func Materialize(c Component) (root string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "eigen-harness-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	root = filepath.Join(tmp, filepath.Base(c.SourceDir))
	prefix := strings.TrimSuffix(c.SourceDir, "/") + "/"
	err = fs.WalkDir(SourceFS, c.SourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := strings.TrimPrefix(path, prefix)
		if rel == c.SourceDir || rel == "" {
			return os.MkdirAll(root, 0o755)
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := SourceFS.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return root, cleanup, nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
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
