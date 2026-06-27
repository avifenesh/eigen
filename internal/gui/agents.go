package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
)

// Agents bridge layer. Subtask/background-task records persist to disk under
// agent.TasksDir() (~/.eigen/tasks[-instance]); the GUI reads them directly to
// render the multi-agent fan-out, and can request cancellation of a running
// task by dropping a cancel marker the host observes.

// BgTaskDTO mirrors agent.BgTask for the fan-out view, with times as unix
// millis (JSON/TS-friendly) and derived display fields.
type BgTaskDTO struct {
	ID         string `json:"id"`
	Task       string `json:"task"`
	Where      string `json:"where,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
	Model      string `json:"model,omitempty"`
	Role       string `json:"role,omitempty"`
	Attempts   int    `json:"attempts,omitempty"`
	Escalated  bool   `json:"escalated,omitempty"`
	Status     string `json:"status"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedMs  int64  `json:"startedMs"`
	FinishedMs int64  `json:"finishedMs,omitempty"`
	Pid        int    `json:"pid,omitempty"`
	Host       string `json:"host,omitempty"`
	Steps      int    `json:"steps,omitempty"`
	LastTool   string `json:"lastTool,omitempty"`
	LastNote   string `json:"lastNote,omitempty"`
	InTokens   int    `json:"inTokens,omitempty"`
	OutTokens  int    `json:"outTokens,omitempty"`
	UpdatedMs  int64  `json:"updatedMs,omitempty"`
	Canceling  bool   `json:"canceling,omitempty"`
}

// AgentsDTO is the fan-out snapshot: tasks grouped by status for the board.
type AgentsDTO struct {
	Tasks   []BgTaskDTO `json:"tasks"`
	Running int         `json:"running"`
	Done    int         `json:"done"`
	Errored int         `json:"errored"`
	Dir     string      `json:"dir"`
}

// TasksDTO is the public REST name for the same background-task snapshot the
// legacy Wails Agents bridge returns.
type TasksDTO = AgentsDTO

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func toBgTaskDTO(t agent.BgTask) BgTaskDTO {
	return BgTaskDTO{
		ID: t.ID, Task: t.Task, Where: t.Where, Kind: t.Kind, Difficulty: t.Difficulty,
		Model: t.Model, Role: t.Role, Attempts: t.Attempts, Escalated: t.Escalated,
		Status: t.Status, Result: t.Result, Error: t.Error,
		StartedMs: ms(t.Started), FinishedMs: ms(t.Finished),
		Pid: t.Pid, Host: t.Host, Steps: t.Steps, LastTool: t.LastTool,
		LastNote: t.LastNote, InTokens: t.InTokens, OutTokens: t.OutTokens,
		UpdatedMs: ms(t.Updated), Canceling: t.Canceling,
	}
}

func tasksSnapshot(dir string) *TasksDTO {
	tasks := agent.LoadBgTasks(dir) // already sorted newest-first, lost/canceling derived
	out := make([]BgTaskDTO, 0, len(tasks))
	var running, done, errored int
	for _, t := range tasks {
		out = append(out, toBgTaskDTO(t))
		switch t.Status {
		case "running":
			running++
		case "done":
			done++
		case "error", "lost":
			errored++
		}
	}
	return &TasksDTO{Tasks: out, Running: running, Done: done, Errored: errored, Dir: dir}
}

// Agents returns the background/subtask fan-out, newest first, with counts.
func (b *Bridge) Agents() (*AgentsDTO, error) {
	return tasksSnapshot(agent.TasksDir()), nil
}

// CancelAgent requests cancellation of a running background task.
func (b *Bridge) CancelAgent(id string) error {
	return agent.RequestCancel(agent.TasksDir(), id)
}

// AgentTranscript returns the raw transcript snapshot for a task, if one exists
// on disk (subtask message exchanges, one JSON line each). The id is validated
// against the same constraint readers use before it is joined into a path, so a
// crafted id (e.g. "../../etc/passwd") can never read outside the tasks dir.
func (b *Bridge) AgentTranscript(id string) (string, error) {
	if !agent.ValidTaskID(id) {
		return "", fmt.Errorf("invalid task id %q", id)
	}
	path := filepath.Join(agent.TasksDir(), id+".transcript.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// AgentHistory returns a task's full append-only state trail (attempt 1
// failed->escalating, attempt 2, overflow notes, terminal) so the board can
// show why a task retried/escalated. Records are in append order, oldest
// first. A missing task yields an empty slice; only a malformed id errors.
func (b *Bridge) AgentHistory(id string) ([]BgTaskDTO, error) {
	hist, err := agent.ReadTaskHistory(agent.TasksDir(), id)
	if err != nil {
		return nil, err
	}
	out := make([]BgTaskDTO, 0, len(hist))
	for _, t := range hist {
		out = append(out, toBgTaskDTO(t))
	}
	return out, nil
}
