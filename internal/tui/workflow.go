package tui

import (
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/workflow"
	tea "github.com/charmbracelet/bubbletea"
)

// runWorkflowCmd implements in-TUI `/workflow <name> [k=v ...]`: load an
// authored workflow and play its steps in THIS session — submit step 1, queue
// the rest (the queue drains after each turn). The full headless runner
// (`eigen run`) adds judged checks + on_failure handling + exit codes; in the
// chat we keep it to "play the prompts in order" so it composes with the live
// session, approvals, and steering. {{var.X}} is filled from k=v args.
func (m *model) runWorkflowCmd(arg string) tea.Cmd {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		names := workflow.List()
		if len(names) == 0 {
			m.note("no workflows yet — author one at ~/.eigen/workflows/<name>.md (see /help). headless: `eigen run <name>`")
		} else {
			m.note("workflows: " + strings.Join(names, ", ") + "   ·   /workflow <name> [k=v …]")
		}
		return nil
	}
	name := fields[0]
	vars := map[string]string{}
	for _, kv := range fields[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	wf, err := workflow.Load(name)
	if err != nil {
		m.note("workflow: " + err.Error())
		return nil
	}
	// Interpolate every step now; report any unset vars once.
	prompts := make([]string, 0, len(wf.Steps))
	var missing []string
	for _, s := range wf.Steps {
		p, miss := workflow.Interpolate(s.Prompt, vars)
		prompts = append(prompts, p)
		missing = append(missing, miss...)
	}
	if len(missing) > 0 {
		m.note("workflow: unset vars: " + strings.Join(dedupe(missing), ", ") + " (pass k=v)")
	}
	if m.state == stRunning {
		m.note("finish or interrupt the current turn before starting a workflow")
		return nil
	}
	m.note(fmt.Sprintf("workflow %s — %d steps (checks/on-failure apply only to headless `eigen run`)", wf.Name, len(prompts)))
	// Queue steps 2..N; submit step 1 (the queue drains after each turn).
	if len(prompts) > 1 {
		m.queued = append(m.queued, prompts[1:]...)
	}
	return m.submit(prompts[0])
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
