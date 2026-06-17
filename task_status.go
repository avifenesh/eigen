package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

func promoteTaskTranscript(bg *agent.BgRegistry, id string) (string, error) {
	if bg == nil {
		return "", fmt.Errorf("background tasks unavailable")
	}
	if id == "" {
		return "", fmt.Errorf("promote needs a background task id")
	}
	t := bg.Get(id)
	if t == nil {
		return "", fmt.Errorf("no such background task: %s", id)
	}
	src := bg.TranscriptPath(id)
	if src == "" {
		return "", fmt.Errorf("background task %s has no transcript path", id)
	}
	msgs, err := transcript.Load(src)
	if err != nil {
		return "", fmt.Errorf("read background transcript: %w", err)
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("background transcript %s is empty", src)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".eigen", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := uniquePromotedSessionPath(dir, id)
	if err := transcript.Save(dst, msgs); err != nil {
		return "", err
	}
	wd, _ := os.Getwd()
	_ = transcript.SaveMeta(dst, transcript.SessionMeta{
		Dir:   wd,
		Title: "background " + id + ": " + oneLineLimit(t.Task, 80),
		Model: t.Model,
	})
	return fmt.Sprintf("promoted background task %s to a resumable session\npath: %s\nresume: eigen --resume %s\nmessages: %d", id, dst, strconv.Quote(dst), len(msgs)), nil
}

func uniquePromotedSessionPath(dir, id string) string {
	base := time.Now().Format("20060102-150405") + "-" + id
	path := filepath.Join(dir, base+".eigen.jsonl")
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.eigen.jsonl", base, i))
	}
}

// formatTaskStatus is the textual surface behind the task_status tool.
func formatTaskStatus(bg *agent.BgRegistry, id string, all, verbose bool, tail int) string {
	if bg == nil {
		return "background tasks unavailable"
	}
	if tail < 0 {
		tail = 0
	}
	if tail > maxTranscriptTail {
		tail = maxTranscriptTail
	}
	if id != "" && !all {
		t := bg.Get(id)
		if t == nil {
			return "no such background task: " + id
		}
		if verbose || tail > 0 {
			return formatTaskStatusVerbose(bg, *t, tail)
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

const maxTranscriptTail = 50

func formatTaskStatusVerbose(bg *agent.BgRegistry, t agent.BgTask, tail int) string {
	var b strings.Builder
	b.WriteString(t.Format())
	if state := bg.StatePath(t.ID); state != "" {
		b.WriteString("\n\nstate: " + pathExistLabel(state))
	}
	transcript := bg.TranscriptPath(t.ID)
	if transcript != "" {
		b.WriteString("\ntranscript: " + pathExistLabel(transcript))
	}
	if hist := bg.History(t.ID); len(hist) > 0 {
		b.WriteString("\n\nattempts:")
		for _, a := range summarizeAttempts(hist) {
			b.WriteString("\n  " + a)
		}
	}
	if tail > 0 && transcript != "" {
		b.WriteString("\n\n" + formatTranscriptTail(transcript, tail))
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

func formatTranscriptTail(path string, n int) string {
	if n <= 0 {
		return "transcript tail: disabled"
	}
	if n > maxTranscriptTail {
		n = maxTranscriptTail
	}
	msgs, err := readTranscriptTail(path, n)
	if err != nil {
		return "transcript tail: " + err.Error()
	}
	if len(msgs) == 0 {
		return fmt.Sprintf("transcript tail (last %d): empty", n)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("transcript tail (last %d message(s)):", len(msgs)))
	for _, m := range msgs {
		b.WriteString("\n  " + formatTranscriptMessage(m))
	}
	return b.String()
}

func readTranscriptTail(path string, n int) ([]llm.Message, error) {
	if n <= 0 {
		return nil, nil
	}
	if n > maxTranscriptTail {
		n = maxTranscriptTail
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	ring := make([]llm.Message, 0, n)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var msg llm.Message
			if json.Unmarshal(bytes.TrimSpace(line), &msg) == nil && msg.Role != "" {
				if len(ring) == n {
					copy(ring, ring[1:])
					ring[n-1] = msg
				} else {
					ring = append(ring, msg)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ring, err
		}
	}
	return ring, nil
}

func formatTranscriptMessage(m llm.Message) string {
	label := string(m.Role)
	if m.Role == llm.RoleTool {
		if m.ToolName != "" {
			label += "/" + m.ToolName
		} else if m.ToolCallID != "" {
			label += "/" + m.ToolCallID
		}
	}
	text := m.Text
	if text == "" && len(m.ToolCalls) > 0 {
		var names []string
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				names = append(names, tc.Name)
			}
		}
		if len(names) > 0 {
			text = "tool calls: " + strings.Join(names, ", ")
		}
	}
	if text == "" && len(m.Images) > 0 {
		text = fmt.Sprintf("%d image(s)", len(m.Images))
	}
	if text == "" {
		text = "(empty)"
	}
	return label + ": " + oneLineLimit(text, 240)
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

func oneLine(s string) string { return oneLineLimit(s, 120) }

func oneLineLimit(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if limit <= 0 {
		return ""
	}
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}
