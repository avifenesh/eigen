package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

// formatTaskStatus is the textual surface behind the task_status tool.
func formatTaskStatus(bg *agent.BgRegistry, id string, all bool) string {
	if bg == nil {
		return "background tasks unavailable"
	}
	if id != "" && !all {
		t := bg.Get(id)
		if t == nil {
			return "no such background task: " + id
		}
		return t.Format()
	}
	tasks := bg.List()
	if len(tasks) == 0 {
		return "no background tasks"
	}
	var b strings.Builder
	for _, t := range tasks {
		line := fmt.Sprintf("%s  %-7s", t.ID, t.Status)
		if t.Where != "" {
			line += "  " + t.Where
		}
		if t.Status == "running" {
			if t.Steps > 0 {
				line += fmt.Sprintf("  step %d", t.Steps)
			}
			if t.LastTool != "" {
				line += "  tool: " + t.LastTool
			}
			line += "  " + time.Since(t.Started).Round(time.Second).String()
			if t.Canceling {
				line += "  (cancel requested)"
			}
		}
		if t.Status == "done" && t.Result != "" {
			line += "  — " + oneLine(t.Result)
		}
		if t.Status == "error" && t.Error != "" {
			line += "  — ERROR: " + oneLine(t.Error)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
