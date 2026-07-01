package gui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Crons bridge layer. Surfaces scheduled work the user has set up around eigen:
// systemd --user timers and the user's crontab. Read via the same mechanisms
// the TUI crons page uses; a timer can be started/stopped/enabled/disabled.

// CronDTO is one scheduled job (a systemd timer or a crontab line).
//
// Enabled vs Active are independent in systemd: Enabled is the persistent
// install state (survives reboot, controlled by enable/disable); Active is
// whether the unit is loaded/running right now (controlled by start/stop). A
// disabled-but-started timer fires until reboot; an enabled-but-stopped timer
// is dormant until its next start. The UI drives Enable/Disable from Enabled
// and Start/Stop from Active so each control reflects what it actually does.
type CronDTO struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`    // "timer" | "crontab"
	Next    string `json:"next"`    // human next-run (timer) or cron spec (crontab)
	Last    string `json:"last"`    // last run (timer); "" for crontab
	Active  bool   `json:"active"`  // unit is loaded/running now (start/stop)
	Enabled bool   `json:"enabled"` // unit is enabled persistently (enable/disable)
	Command string `json:"command"`
	Unit    string `json:"unit,omitempty"` // systemd unit name (timer only) for control ops
}

// CronsDTO is the scheduled-work snapshot.
type CronsDTO struct {
	Crons        []CronDTO `json:"crons"`
	Timers       int       `json:"timers"`
	Crontab      int       `json:"crontab"`
	SystemdAvail bool      `json:"systemdAvail"`
}

func humanizeMicros(us int64) string {
	if us <= 0 {
		return "—"
	}
	t := time.Unix(us/1_000_000, 0)
	if t.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		return "today " + t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}

// timerRow is the shape systemctl's --output=json emits per timer.
type timerRow struct {
	Next      int64  `json:"next"`
	Last      int64  `json:"last"`
	Unit      string `json:"unit"`
	Activates string `json:"activates"`
}

func loadSystemdTimers() ([]CronDTO, bool) {
	out, err := exec.Command("systemctl", "--user", "list-timers", "--all", "--output=json").Output()
	if err != nil {
		return nil, false
	}
	var timers []timerRow
	if json.Unmarshal(out, &timers) != nil {
		return nil, true // systemd present, just no parseable timers
	}
	// is-active/is-enabled each spawn a systemctl process; run them concurrently
	// (per timer, in parallel with each other) instead of 2N sequential spawns —
	// on a machine with a dozen timers that's the difference between ~150ms and
	// ~15ms for a view that re-fetches on every nav click (no caching upstream).
	// A semaphore caps how many of those spawns run at once — unbounded fan-out
	// on a machine with dozens of timers would otherwise hit fd/process limits.
	const maxConcurrentSpawns = 8
	sem := make(chan struct{}, maxConcurrentSpawns)
	rows := make([]CronDTO, len(timers))
	var wg sync.WaitGroup
	for i, t := range timers {
		wg.Add(1)
		go func(i int, t timerRow) {
			defer wg.Done()
			var active, enabled bool
			var iwg sync.WaitGroup
			iwg.Add(2)
			go func() {
				defer iwg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				active = timerIsActive(t.Unit)
			}()
			go func() {
				defer iwg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				enabled = timerIsEnabled(t.Unit)
			}()
			iwg.Wait()
			rows[i] = CronDTO{
				Name:    strings.TrimSuffix(t.Unit, ".timer"),
				Kind:    "timer",
				Next:    humanizeMicros(t.Next),
				Last:    humanizeMicros(t.Last),
				Active:  active,
				Enabled: enabled,
				Command: t.Activates,
				Unit:    t.Unit,
			}
		}(i, t)
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, true
}

// timerIsActive reports whether a unit is loaded/running now (start/stop axis).
// systemctl is-active exits non-zero when not active but still prints the state
// to stdout, so we read the trimmed output rather than the exit code.
func timerIsActive(unit string) bool {
	out, _ := exec.Command("systemctl", "--user", "is-active", unit).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// timerIsEnabled reports whether a unit is enabled persistently (enable/disable
// axis), independent of whether it is running. is-enabled prints e.g.
// "enabled"/"disabled"/"static" to stdout regardless of exit code.
func timerIsEnabled(unit string) bool {
	out, _ := exec.Command("systemctl", "--user", "is-enabled", unit).Output()
	return strings.TrimSpace(string(out)) == "enabled"
}

func loadCrontab() []CronDTO {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil
	}
	var rows []CronDTO
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var spec, command string
		if strings.HasPrefix(fields[0], "@") {
			spec = fields[0]
			command = strings.Join(fields[1:], " ")
		} else if len(fields) >= 6 {
			spec = strings.Join(fields[:5], " ")
			command = strings.Join(fields[5:], " ")
		} else {
			continue
		}
		rows = append(rows, CronDTO{
			Name:    command,
			Kind:    "crontab",
			Next:    spec,
			Active:  true,
			Enabled: true,
			Command: command,
		})
	}
	return rows
}

// Crons returns systemd --user timers + the user's crontab.
func (b *Bridge) Crons() (*CronsDTO, error) {
	timers, systemdAvail := loadSystemdTimers()
	crontab := loadCrontab()
	all := make([]CronDTO, 0, len(timers)+len(crontab))
	all = append(all, timers...)
	all = append(all, crontab...)
	return &CronsDTO{
		Crons:        all,
		Timers:       len(timers),
		Crontab:      len(crontab),
		SystemdAvail: systemdAvail,
	}, nil
}

// AddCrontab appends a job to the user's crontab. spec is a 5-field cron
// expression or an @keyword (@daily, @hourly, …); command is the shell command.
// Validates the spec shape, dedupes an identical line, and writes the whole
// crontab back via `crontab -`. A missing crontab (exit on `crontab -l`) is
// treated as empty, so the first job creates it.
func (b *Bridge) AddCrontab(spec, command string) error {
	spec = strings.TrimSpace(spec)
	command = strings.TrimSpace(command)
	if spec == "" || command == "" {
		return fmt.Errorf("schedule and command are required")
	}
	if err := validateCronSpec(spec); err != nil {
		return err
	}
	lines := currentCrontabLines()
	line := spec + " " + command
	for _, l := range lines {
		if strings.TrimSpace(l) == line {
			return fmt.Errorf("that schedule + command is already in your crontab")
		}
	}
	lines = append(lines, line)
	return writeCrontab(lines)
}

// RemoveCrontab deletes the crontab line matching spec + command exactly.
// Returns an error when no matching line is found (nothing removed).
func (b *Bridge) RemoveCrontab(spec, command string) error {
	spec = strings.TrimSpace(spec)
	command = strings.TrimSpace(command)
	target := spec + " " + command
	lines := currentCrontabLines()
	kept := make([]string, 0, len(lines))
	removed := false
	for _, l := range lines {
		if strings.TrimSpace(l) == target {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return fmt.Errorf("no crontab entry matched %q", target)
	}
	return writeCrontab(kept)
}

// currentCrontabLines returns the user's crontab lines (empty when none).
func currentCrontabLines() []string {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil // no crontab yet (or unreadable) — treat as empty
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimRight(l, " \t") != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// writeCrontab installs lines as the user's crontab via `crontab -` (reads from
// stdin). An empty set clears the crontab with `crontab -r`.
func writeCrontab(lines []string) error {
	if len(lines) == 0 {
		// `crontab -r` errors when there's no crontab; ignore that case.
		_ = exec.Command("crontab", "-r").Run()
		return nil
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write crontab: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// validateCronSpec checks spec is an @keyword or a 5-field cron expression.
// Field-content validation is left to cron itself (it rejects on install); this
// catches the common shape mistakes before we touch the live crontab.
func validateCronSpec(spec string) error {
	if strings.HasPrefix(spec, "@") {
		switch spec {
		case "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly", "@reboot":
			return nil
		}
		return fmt.Errorf("unknown schedule keyword %q (use @daily, @hourly, @weekly, … or a 5-field cron expression)", spec)
	}
	if len(strings.Fields(spec)) != 5 {
		return fmt.Errorf("cron schedule must be 5 fields (min hour day month weekday) or an @keyword, got %q", spec)
	}
	return nil
}

// SetTimer controls a systemd --user timer. verb is start|stop|enable|disable.
func (b *Bridge) SetTimer(unit, verb string) error {
	switch verb {
	case "start", "stop", "enable", "disable":
	default:
		return fmt.Errorf("invalid timer verb %q (want start|stop|enable|disable)", verb)
	}
	if !strings.HasSuffix(unit, ".timer") {
		return fmt.Errorf("not a timer unit: %q", unit)
	}
	out, err := exec.Command("systemctl", "--user", verb, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", verb, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}
