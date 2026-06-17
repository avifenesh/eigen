package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

// formatTaskStatus is the textual surface behind the task_status tool.
func formatTaskStatus(bg *agent.BgRegistry, id string, all, verbose bool) string {
	if bg == nil {
		return "background tasks unavailable"
	}
	if id != "" && !all {
		t := bg.Get(id)
		if t == nil {
			return "no such background task: " + id
		}
		if verbose {
			return formatTaskStatusVerbose(bg, *t)
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
			if t.Attempts > 1 || t.Escalated {
				attempt := t.Attempts
				if attempt < 1 {
					attempt = 1
				}
				line += fmt.Sprintf("  attempt %d", attempt)
			}
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
			if t.LastNote != "" {
				line += "  — " + oneLine(t.LastNote)
			}
		}
		if t.Status == "done" && t.Result != "" {
			line += "  — " + oneLine(t.Result)
		}
		if t.Status == "error" && t.Error != "" {
			line += "  — ERROR: " + oneLine(t.Error)
		}
		if verbose {
			if p := bg.TranscriptPath(t.ID); p != "" {
				line += "  transcript: " + pathExistLabel(p)
			}
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatTaskStatusVerbose(bg *agent.BgRegistry, t agent.BgTask) string {
	var b strings.Builder
	b.WriteString(t.Format())
	if state := bg.StatePath(t.ID); state != "" {
		b.WriteString("\n\nstate: " + pathExistLabel(state))
	}
	if transcript := bg.TranscriptPath(t.ID); transcript != "" {
		b.WriteString("\ntranscript: " + pathExistLabel(transcript))
	}
	if hist := bg.History(t.ID); len(hist) > 0 {
		b.WriteString("\n\nattempts:")
		for _, a := range summarizeAttempts(hist) {
			b.WriteString("\n  " + a)
		}
	}
	return b.String()
}

type attemptSpan struct {
	n    int
	last agent.BgTask
}

func summarizeAttempts(hist []agent.BgTask) []string {
	if len(hist) == 0 {
		return nil
	}
	var spans []attemptSpan
	for _, h := range hist {
		n := h.Attempts
		if n <= 0 {
			n = 1
		}
		if len(spans) == 0 || spans[len(spans)-1].n != n {
			spans = append(spans, attemptSpan{n: n, last: h})
			continue
		}
		spans[len(spans)-1].last = h
	}
	out := make([]string, 0, len(spans))
	for i, s := range spans {
		last := s.last
		status := last.Status
		if i < len(spans)-1 {
			status = "retried"
		}
		line := fmt.Sprintf("attempt %d: %s", s.n, status)
		if last.Difficulty != "" {
			line += "  difficulty " + last.Difficulty
		}
		if last.Kind != "" {
			line += "  kind " + last.Kind
		}
		if last.Model != "" {
			line += "  model " + last.Model
		}
		if last.Where != "" {
			line += "  " + last.Where
		}
		if last.Steps > 0 {
			line += fmt.Sprintf("  %d step(s)", last.Steps)
		}
		if last.InTokens > 0 || last.OutTokens > 0 {
			line += fmt.Sprintf("  tokens %d/%d", last.InTokens, last.OutTokens)
		}
		if last.LastNote != "" {
			line += "  — " + oneLine(last.LastNote)
		}
		if last.Error != "" && status != "retried" {
			line += "  — ERROR: " + oneLine(last.Error)
		}
		out = append(out, line)
	}
	return out
}

func pathExistLabel(path string) string {
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return path + " (not created)"
	}
	return path
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
