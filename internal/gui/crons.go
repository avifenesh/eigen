package gui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
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

func loadSystemdTimers() ([]CronDTO, bool) {
	out, err := exec.Command("systemctl", "--user", "list-timers", "--all", "--output=json").Output()
	if err != nil {
		return nil, false
	}
	var timers []struct {
		Next      int64  `json:"next"`
		Last      int64  `json:"last"`
		Unit      string `json:"unit"`
		Activates string `json:"activates"`
	}
	if json.Unmarshal(out, &timers) != nil {
		return nil, true // systemd present, just no parseable timers
	}
	rows := make([]CronDTO, 0, len(timers))
	for _, t := range timers {
		rows = append(rows, CronDTO{
			Name:    strings.TrimSuffix(t.Unit, ".timer"),
			Kind:    "timer",
			Next:    humanizeMicros(t.Next),
			Last:    humanizeMicros(t.Last),
			Active:  timerIsActive(t.Unit),
			Enabled: timerIsEnabled(t.Unit),
			Command: t.Activates,
			Unit:    t.Unit,
		})
	}
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
