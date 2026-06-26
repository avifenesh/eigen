package llm

import (
	"runtime/debug"
	"sync"
)

// fullVersion caches the build-stamped version (base + short git rev + -dirty).
var (
	fullVersionOnce sync.Once
	fullVersionStr  string
)

// FullVersion returns the base Version annotated with the build's git revision
// and a -dirty marker when the build had uncommitted changes — e.g.
// "0.1.0+7c6737f" or "0.1.0+7c6737f-dirty". Falls back to bare Version when no
// VCS stamp is embedded (e.g. `go run`). Computed once from debug.BuildInfo, the
// same source the daemon uses for its build identity, so daemon and CLI/GUI/TUI
// all report the same string for a given binary.
func FullVersion() string {
	fullVersionOnce.Do(func() {
		fullVersionStr = Version
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		var rev string
		var modified bool
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			fullVersionStr = Version + "+" + rev
			if modified {
				fullVersionStr += "-dirty"
			}
		}
	})
	return fullVersionStr
}
