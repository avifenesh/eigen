package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	return os.Rename(tmpName, path)
}

// runRipgrep runs rg with a timeout and returns combined output and exit code.
// rg exits 1 when there are no matches, which callers treat as "no results"
// rather than an error; a non-ExitError (e.g. rg missing) is returned as err.
func runRipgrep(ctx context.Context, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "rg", args...).CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode(), nil
	}
	if err != nil {
		return "", -1, err
	}
	return string(out), 0, nil
}
