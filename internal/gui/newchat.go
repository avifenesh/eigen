package gui

import (
	"os"
	"path/filepath"
)

// New-chat working-directory picker. Starting a fresh chat means choosing a
// root dir, and typing a full path is hostile — so the GUI offers two affordances
// instead: RecentDirs() lists the project dirs the user has actually worked in
// (newest-first, reused from the proactive-feed's session-history universe), and
// PickDirectory() opens the native OS folder dialog. Both are server-side so the
// frontend never reasons about paths or window handles.

// recentDirsCap bounds RecentDirs to the most recent working dirs — enough to
// fill the picker without turning it into a scroll list.
const recentDirsCap = 12

// RecentDirDTO is one recent project dir for the new-chat picker: the absolute
// path plus its basename for a compact label.
type RecentDirDTO struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// RecentDirs returns the recent project working dirs for the new-chat picker,
// newest-first. Source is b.projectDirs() (the same distinct session-history
// dirs the proactive feed scans); we dedup, drop any that no longer exist on
// disk (os.Stat — a since-deleted project shouldn't surface as a start option),
// and cap to recentDirsCap. Never errors (an empty universe just yields nil),
// but keeps the (…, error) shape so the binding can grow a failure mode later.
func (b *Bridge) RecentDirs() ([]RecentDirDTO, error) {
	dirs := b.projectDirs()
	out := make([]RecentDirDTO, 0, len(dirs))
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		if dir == "" || isEphemeralDir(dir) {
			continue // skip throwaway temp/sandbox cwds (see isEphemeralDir)
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue // gone or not a dir — don't offer it as a start option
		}
		out = append(out, RecentDirDTO{Dir: dir, Name: filepath.Base(dir)})
		if len(out) >= recentDirsCap {
			break
		}
	}
	return out, nil
}

// PickDirectory opens the native OS folder dialog and returns the chosen
// absolute path, or "" if the user cancelled (a cancel is NOT an error — the
// caller just keeps its current selection). The dialog itself is host-UI work
// behind promptForPath (wails.go): the Wails build shows the real dialog; the
// tagless build fails closed telling the client to pass an explicit path (the
// Qt client uses QFileDialog and never calls this).
//
// The dialog is scoped to directories only and starts at a sensible default
// (the user's home, else the cwd) so the user isn't dropped at the filesystem
// root.
func (b *Bridge) PickDirectory() (string, error) {
	return b.promptForPath("Choose project directory", defaultPickDir(), true)
}

// defaultPickDir picks a friendly start dir for the folder dialog: the user's
// home, falling back to the current working dir, else "" (let the OS decide).
func defaultPickDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}
