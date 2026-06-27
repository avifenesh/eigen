package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
)

// TasksAPIHandler layers the local desktop REST task API over the embedded
// Wails asset server. It keeps task reads/cancel visible to the webview through
// normal fetch() calls while all other paths continue to serve the GUI assets.
func TasksAPIHandler(next http.Handler) http.Handler {
	return newTasksAPIHandler(next, agent.TasksDir)
}

func newTasksAPIHandler(next http.Handler, tasksDir func() string) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL == nil || (r.URL.Path != "/api/tasks" && r.URL.Path != "/api/tasks/" && !strings.HasPrefix(r.URL.Path, "/api/tasks/")) {
			next.ServeHTTP(w, r)
			return
		}
		dir := tasksDir()
		switch {
		case r.URL.Path == "/api/tasks" || r.URL.Path == "/api/tasks/":
			if r.Method != http.MethodGet {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			writeJSON(w, http.StatusOK, tasksSnapshot(dir))
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			handleTaskItem(w, r, dir, strings.TrimPrefix(r.URL.Path, "/api/tasks/"))
		default:
			writeAPIError(w, http.StatusNotFound, "not found")
		}
	})
}

func handleTaskItem(w http.ResponseWriter, r *http.Request, dir, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) != 2 {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	id, action := parts[0], parts[1]
	if !agent.ValidTaskID(id) {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("invalid task id %q", id))
		return
	}
	switch action {
	case "cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := agent.RequestCancel(dir, id); err != nil {
			writeAPIError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case "transcript":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		text, err := taskTranscript(dir, id)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"transcript": text})
	default:
		writeAPIError(w, http.StatusNotFound, "not found")
	}
}

func taskTranscript(dir, id string) (string, error) {
	if !agent.ValidTaskID(id) {
		return "", fmt.Errorf("invalid task id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".transcript.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeAPIError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
