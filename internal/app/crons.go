package app

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// CronRow is one scheduled job as the crons page shows it: a systemd user
// timer or a crontab entry.
type CronRow struct {
	Name    string // timer unit name or a crontab command snippet
	Kind    string // "timer" | "crontab"
	Next    string // human "next run" (systemd's NEXT column; cron spec for crontab)
	Last    string // last run (systemd PASSED; "" for crontab)
	Active  bool   // timer enabled/active
	Command string // what it runs (unit activated / crontab command)
}

// loadCrons gathers scheduled jobs (read-only): systemd user timers +
// the user's crontab. Failures degrade to an empty section.
func loadCrons() []CronRow {
	var rows []CronRow
	rows = append(rows, loadSystemdTimers()...)
	rows = append(rows, loadCrontab()...)
	return rows
}

// loadSystemdTimers lists systemd --user timers via systemctl's JSON output
// (robust against the table format's whitespace-aligned date columns).
func loadSystemdTimers() []CronRow {
	out, err := exec.Command("systemctl", "--user", "list-timers", "--all", "--output=json").Output()
	if err != nil {
		return nil
	}
	var timers []struct {
		Next      int64  `json:"next"` // µs since epoch (0/absent = inactive)
		Last      int64  `json:"last"`
		Unit      string `json:"unit"`
		Activates string `json:"activates"`
	}
	if json.Unmarshal(out, &timers) != nil {
		return nil
	}
	rows := make([]CronRow, 0, len(timers))
	for _, t := range timers {
		rows = append(rows, CronRow{
			Name:    strings.TrimSuffix(t.Unit, ".timer"),
			Kind:    "timer",
			Next:    humanizeMicros(t.Next),
			Last:    humanizeMicros(t.Last),
			Active:  t.Next > 0,
			Command: t.Activates,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// humanizeMicros renders a µs-epoch timestamp compactly ("today 07:00" or
// "2026-06-14 11:49"); 0 = never/inactive.
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

// loadCrontab lists the user's crontab entries.
func loadCrontab() []CronRow {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil // no crontab (or no cron) — fine
	}
	var rows []CronRow
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// spec = first 5 fields (or @keyword), command = rest
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
		name := command
		if len(name) > 40 {
			name = name[:40] + "⋯"
		}
		rows = append(rows, CronRow{
			Name:    name,
			Kind:    "crontab",
			Next:    spec,
			Active:  true,
			Command: command,
		})
	}
	return rows
}

// cronsState: the read-only crons page (systemd user timers + crontab).
type cronsState struct {
	list   list
	rows   []CronRow
	loaded bool
	status string // last action feedback
}

func (c *cronsState) init(*Data) {}

// load is lazy (first view) — shelling out to systemctl at app start would
// slow every launch for a page most visits never open.
func (c *cronsState) load() {
	if c.loaded {
		return
	}
	c.rows = loadCrons()
	c.list.count = len(c.rows)
	c.loaded = true
}

func (c *cronsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c.load()
	key := msg.String()
	if c.list.key(key, m.height-6) {
		c.status = ""
		return m, nil
	}
	switch key {
	case "R": // manual refresh (capital: bare letters are page-jumps)
		c.loaded = false
		c.load()
		c.status = ""
	case " ", "enter":
		// Toggle a systemd user timer on/off (stop also disables future
		// runs until started again; crontab rows are file-managed).
		if c.list.cursor < len(c.rows) {
			r := c.rows[c.list.cursor]
			if r.Kind != "timer" {
				c.status = "crontab entries are edited with `crontab -e`"
				break
			}
			verb := "start"
			if r.Active {
				verb = "stop"
			}
			if out, err := timerCtl(verb, r.Name+".timer"); err != nil {
				c.status = strings.TrimSpace(verb + " failed: " + out)
			} else {
				c.status = verb + "ed " + r.Name
				c.loaded = false
				c.load()
			}
		}
	case "t":
		// Trigger the unit the timer activates, right now.
		if c.list.cursor < len(c.rows) {
			r := c.rows[c.list.cursor]
			if r.Kind != "timer" || r.Command == "" {
				c.status = "trigger works on systemd timers only"
				break
			}
			if out, err := timerCtl("start", r.Command); err != nil {
				c.status = strings.TrimSpace("trigger failed: " + out)
			} else {
				c.status = "triggered " + r.Command
			}
		}
	}
	return m, nil
}

// timerCtl runs systemctl --user <verb> <unit>, returning combined output.
func timerCtl(verb, unit string) (string, error) {
	out, err := exec.Command("systemctl", "--user", verb, unit).CombinedOutput()
	return string(out), err
}

func (c *cronsState) view(m *Model, w, h int) string {
	c.load()
	out := pageTitle("crons", "systemd user timers + crontab", w)
	if len(c.rows) == 0 {
		out += "\n" + sDim.Render("  no scheduled jobs found") + "\n"
		return out
	}
	out += cronsSummaryLine(c.rows, w) + "\n\n"
	visible := h - 8
	if visible < 3 {
		visible = 3
	}
	start := c.list.top
	for i := start; i < len(c.rows) && i < start+visible; i++ {
		r := c.rows[i]
		cursor := "  "
		if i == c.list.cursor {
			cursor = sAccent.Render("▎ ")
		}
		mark := sDim.Render("○")
		if r.Active {
			mark = sOk.Render("●")
		}
		kind := sDim.Render(pad(r.Kind, 8))
		name := sText.Render(pad(truncate(r.Name, 34), 36))
		next := sDim.Render(pad(truncate(r.Next, 22), 24))
		out += cursor + mark + " " + kind + name + next + "\n"
	}
	if i := c.list.cursor; i < len(c.rows) {
		out += "\n" + cronSelectedDetail(c.rows[i], w) + "\n"
	}
	if c.status != "" {
		out += sWarn.Render("  "+truncate(c.status, w-4)) + "\n"
	}
	out += sFaint.Render("  space stop/start timer · t trigger now · R refresh")
	return out
}

func cronsSummaryLine(rows []CronRow, w int) string {
	var timers, crontabs, active int
	for _, r := range rows {
		switch r.Kind {
		case "timer":
			timers++
		case "crontab":
			crontabs++
		}
		if r.Active {
			active++
		}
	}
	parts := []string{fmt.Sprintf("%d jobs", len(rows)), fmt.Sprintf("%d active", active), fmt.Sprintf("%d timers", timers), fmt.Sprintf("%d crontab", crontabs)}
	return sFaint.Render("  " + truncate(strings.Join(parts, "  ·  "), max(20, w-2)))
}

func cronSelectedDetail(r CronRow, w int) string {
	cmd := r.Command
	if cmd == "" {
		cmd = r.Name
	}
	line := fmt.Sprintf("selected: %s · next %s · runs %s", r.Kind, r.Next, cmd)
	return sFaint.Render("  " + truncate(line, max(20, w-2)))
}
