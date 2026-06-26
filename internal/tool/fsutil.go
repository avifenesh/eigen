package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// atomicWrite writes data to path via a temp file in the same directory and a
// rename, so a reader never sees a partially written file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".eigen-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil {
		os.Remove(tmpName)
		return werr
	}
	if cerr != nil {
		os.Remove(tmpName)
		return cerr
	}
	// Preserve the destination's mode when overwriting an existing file
	// (CreateTemp makes 0o600, which would silently strip e.g. a 0o755
	// script's executable bit). For brand-new files default to 0o644.
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// runRipgrep runs rg with a timeout and returns combined output and exit code.
// rg exits 0 on matches and 1 when there are no matches; callers treat code 1
// as "no results" rather than an error, so both are returned with err==nil.
// Any other exit (code >= 2, e.g. an invalid regex) is a real failure: rg's
// stderr is in the combined output, so it's surfaced as err rather than being
// mistaken for search results. A non-ExitError (e.g. rg missing) is also err.
func runRipgrep(ctx context.Context, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "rg", args...).CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		if code >= 2 {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = ee.String()
			}
			return "", code, fmt.Errorf("ripgrep failed: %s", msg)
		}
		return string(out), code, nil
	}
	if err != nil {
		return "", -1, err
	}
	return string(out), 0, nil
}
