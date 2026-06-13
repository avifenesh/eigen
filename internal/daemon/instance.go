package daemon

import (
	"os"
	"regexp"
	"sync"
)

// Instance isolation lets a SEPARATE eigen daemon (its own socket, pid, log,
// sessions, tasks) run alongside the production one — so developing eigen and
// /rebuild-ing it never touches the user's real running sessions. The default
// instance ("") maps to the EXACT historical paths (~/.eigen/daemon.sock etc.)
// so existing users are unaffected; a named instance (e.g. "dev") suffixes
// every runtime path (daemon-dev.sock, daemon-dev/sessions, tasks-dev, …).
//
// Selection precedence (resolved once by SetInstance, called from main after
// flag parsing): the --instance flag wins, then $EIGEN_INSTANCE, then default.
// The resolved value is what spawned daemons receive (explicitly, via
// --instance), NOT whatever env happens to be inherited.

var (
	instanceMu  sync.RWMutex
	instance    string
	instanceSet bool
)

// validInstance constrains a name used in filesystem paths + a unix socket
// name: short, no separators, no traversal. Empty = default (production).
var validInstance = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,31}$`)

// ValidInstanceName reports whether name is a usable instance id (empty is the
// valid default).
func ValidInstanceName(name string) bool {
	return name == "" || validInstance.MatchString(name)
}

// SetInstance fixes the active instance for this process. Call once from main
// after resolving the flag/env. An invalid name is a hard error (return false)
// — falling back to production would be dangerous (you'd think you're on dev).
func SetInstance(name string) bool {
	if !ValidInstanceName(name) {
		return false
	}
	instanceMu.Lock()
	instance = name
	instanceSet = true
	instanceMu.Unlock()
	return true
}

// ResolveInstance picks the instance from an explicit flag (wins) then
// $EIGEN_INSTANCE. Returns ("", false) when the chosen name is invalid.
func ResolveInstance(flagVal string) (string, bool) {
	name := flagVal
	if name == "" {
		name = os.Getenv("EIGEN_INSTANCE")
	}
	if !ValidInstanceName(name) {
		return "", false
	}
	return name, true
}

// Instance returns the active instance name ("" = default/production). If
// SetInstance was never called (e.g. a subcommand that didn't parse the flag),
// it falls back to $EIGEN_INSTANCE so spawned daemons still land on the right
// instance.
func Instance() string {
	instanceMu.RLock()
	set, name := instanceSet, instance
	instanceMu.RUnlock()
	if set {
		return name
	}
	if env := os.Getenv("EIGEN_INSTANCE"); ValidInstanceName(env) {
		return env
	}
	return ""
}

// IsDefaultInstance reports whether the active instance is production (the
// default ""). Used to guard the destructive default-daemon rebuild.
func IsDefaultInstance() bool { return Instance() == "" }

// suffix returns "" for the default instance, or "-<name>" otherwise — appended
// to a base filename (daemon.sock → daemon-dev.sock).
func suffix() string {
	if n := Instance(); n != "" {
		return "-" + n
	}
	return ""
}
