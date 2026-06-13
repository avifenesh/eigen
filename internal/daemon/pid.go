package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// PIDPath is the daemon PID file (~/.eigen/daemon.pid). It lets `eigen daemon
// --stop`/`--status` find a running daemon and detect a stale one (process
// gone) so a crashed daemon doesn't wedge the socket forever.
func PIDPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "daemon"+suffix()+".pid")
}

// WritePID records the current process as the daemon owner.
func WritePID(path string) error {
	if path == "" {
		path = PIDPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// RemovePID deletes the PID file (on clean shutdown).
func RemovePID(path string) {
	if path == "" {
		path = PIDPath()
	}
	_ = os.Remove(path)
}

// RunningPID returns the live daemon PID, or 0 if none is running. A PID file
// pointing at a dead process is treated as not-running (and is the caller's cue
// to clean up the stale socket).
func RunningPID(path string) int {
	if path == "" {
		path = PIDPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	if !processAlive(pid) {
		return 0
	}
	return pid
}

// processAlive reports whether a process with pid exists (signal 0 probe).
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Stop signals a running daemon to exit (SIGTERM). Returns the PID stopped, or
// 0 if none was running.
func Stop(pidPath string) (int, error) {
	pid := RunningPID(pidPath)
	if pid == 0 {
		return 0, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return 0, fmt.Errorf("signal pid %d: %w", pid, err)
	}
	return pid, nil
}
