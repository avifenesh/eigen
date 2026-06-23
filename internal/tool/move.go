package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Move returns the file move/rename tool. Both paths are confined by the policy.
// Mutating: requires approval in gated mode.
func Move(policy *Policy) Definition {
	return Definition{
		Name:        "move",
		Description: "Move or rename a file or directory. Both source and destination must be within the allowed roots.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "from": { "type": "string", "description": "Existing path to move." },
    "to": { "type": "string", "description": "Destination path." }
  },
  "required": ["from", "to"],
  "additionalProperties": false
}`),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if in.From == "" || in.To == "" {
				return "", fmt.Errorf("both from and to are required")
			}
			from, err := policy.Resolve(in.From)
			if err != nil {
				return "", err
			}
			to, err := policy.Resolve(in.To)
			if err != nil {
				return "", err
			}
			if _, err := os.Stat(from); err != nil {
				return "", fmt.Errorf("source does not exist: %s", in.From)
			}
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return "", err
			}
			if err := os.Rename(from, to); err != nil {
				// A rename across filesystems fails with EXDEV ("invalid
				// cross-device link"). An /add-dir'd root may live on a
				// different mount, so a move between two allowed roots is
				// legitimate; fall back to copy-then-remove in that case.
				if !errors.Is(err, syscall.EXDEV) {
					return "", err
				}
				if err := moveCrossDevice(from, to); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("moved %s -> %s", in.From, in.To), nil
		},
	}
}

// moveCrossDevice emulates a rename across filesystem boundaries by copying
// src to dst (preserving file modes) and then removing src. It handles both
// regular files and directory trees. On any copy failure the partially copied
// destination is cleaned up so a failed move never leaves a half-written tree
// while also still holding the original source.
func moveCrossDevice(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := copyPath(src, dst, fi); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// copyPath recursively copies src to dst. fi must be the os.Lstat of src.
func copyPath(src, dst string, fi os.FileInfo) error {
	switch {
	case fi.IsDir():
		return copyDir(src, dst, fi)
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	default:
		return copyFile(src, dst, fi)
	}
}

// copyFile copies a regular file from src to dst, preserving its permission
// bits. fi must be the os.Lstat of src.
func copyFile(src, dst string, fi os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// OpenFile honors umask, so set the mode explicitly to match the source.
	return os.Chmod(dst, fi.Mode().Perm())
}

// copyDir recursively copies the directory tree at src to dst, preserving
// each entry's permission bits. fi must be the os.Lstat of src.
func copyDir(src, dst string, fi os.FileInfo) error {
	if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		es, err := e.Info()
		if err != nil {
			return err
		}
		if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), es); err != nil {
			return err
		}
	}
	// Reassert the directory mode after children are created, since MkdirAll
	// applies umask and child writes don't change it but explicit is safest.
	return os.Chmod(dst, fi.Mode().Perm())
}
