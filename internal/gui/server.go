package gui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

//go:embed static/*
var staticFS embed.FS

// ServeOptions configures the development/browser preview for the GUI. The
// desktop shell will use the same Service directly; this server is the thin
// browser-facing adapter and a fast way to validate the UI while the native
// wrapper is still being added.
type ServeOptions struct {
	Addr string // default 127.0.0.1:0
	Open bool
}

type Server struct {
	HTTP *http.Server
	URL  string
}

// Serve starts a local-only HTTP/SSE adapter for the GUI service.
func Serve(ctx context.Context, svc *Service, opts ServeOptions) (*Server, error) {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if !isLocalAddr(ln.Addr().String()) {
		_ = ln.Close()
		return nil, fmt.Errorf("gui server refuses non-local bind %q", ln.Addr().String())
	}
	mux := http.NewServeMux()
	h := &handler{svc: svc}
	h.routes(mux)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	out := &Server{HTTP: srv, URL: "http://" + ln.Addr().String()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Println("eigen gui server:", err)
		}
	}()
	if opts.Open {
		go openBrowser(out.URL)
	}
	return out, nil
}

func isLocalAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type handler struct{ svc *Service }

// StaticAssets returns the embedded frontend assets rooted at internal/gui/static.
func StaticAssets() fs.FS {
	sub, _ := fs.Sub(staticFS, "static")
	return sub
}

// Handler returns the browser-facing API/static handler used by both `eigen gui`
// preview mode and the Wails asset server fallback.
func Handler(svc *Service) http.Handler {
	mux := http.NewServeMux()
	h := &handler{svc: svc}
	h.routes(mux)
	return mux
}

func (h *handler) routes(mux *http.ServeMux) {
	mux.Handle("/", http.FileServer(http.FS(StaticAssets())))
	mux.HandleFunc("/api/health", h.health)
	mux.HandleFunc("/api/observe", h.observe)
	mux.HandleFunc("/api/profile", h.profile)
	mux.HandleFunc("/api/memory", h.memory)
	mux.HandleFunc("/api/skills", h.skills)
	mux.HandleFunc("/api/sessions", h.sessions)
	mux.HandleFunc("/api/sessions/", h.session)
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	v, err := h.svc.Health()
	writeJSON(w, v, err)
}

func (h *handler) observe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 5000
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	v, err := h.svc.Observe(limit)
	writeJSON(w, v, err)
}

func (h *handler) profile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		v, err := h.svc.UserProfile()
		writeJSON(w, map[string]string{"profile": v}, err)
	case http.MethodPost:
		var in struct {
			Profile string `json:"profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true}, h.svc.WriteUserProfile(in.Profile))
	default:
		methodNotAllowed(w)
	}
}

func (h *handler) memory(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dir == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("dir query parameter required"))
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("q")) != "" {
		hits, err := h.svc.SearchProjectMemory(dir, r.URL.Query().Get("q"))
		writeJSON(w, map[string]any{"hits": hits}, err)
		return
	}
	v, err := h.svc.ProjectMemory(dir)
	writeJSON(w, v, err)
}

func (h *handler) skills(w http.ResponseWriter, r *http.Request) {
	if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" {
		body, err := h.svc.SkillBody(name)
		writeJSON(w, map[string]string{"name": name, "body": body}, err)
		return
	}
	v, err := h.svc.Skills()
	writeJSON(w, v, err)
}

func (h *handler) sessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		v, err := h.svc.Sessions()
		writeJSON(w, v, err)
	case http.MethodPost:
		var in struct {
			Dir   string `json:"dir"`
			Model string `json:"model"`
			Perm  string `json:"perm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		id, err := h.svc.NewSession(in.Dir, in.Model, in.Perm)
		writeJSON(w, map[string]string{"id": id}, err)
	case http.MethodDelete:
		var in struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(in.ID) == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("session id required"))
			return
		}
		writeJSON(w, map[string]bool{"ok": true}, h.svc.Remove(in.ID))
	default:
		methodNotAllowed(w)
	}
}

func (h *handler) session(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("session id required"))
		return
	}
	id := parts[0]
	action := "state"
	if len(parts) > 1 && parts[1] != "" {
		action = parts[1]
	}
	switch action {
	case "state":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		v, err := h.svc.State(id)
		writeJSON(w, v, err)
	case "events":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		h.events(w, r, id)
	case "input":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Text       string   `json:"text"`
			AllowTools []string `json:"allow_tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		steered, err := h.svc.InputWithTools(id, in.Text, in.AllowTools)
		writeJSON(w, map[string]bool{"steered": steered}, err)
	case "approve":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Approval string `json:"approval"`
			Allow    bool   `json:"allow"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true}, h.svc.Approve(id, in.Approval, in.Allow))
	case "interrupt":
		requirePost(w, r, func() error { return h.svc.Interrupt(id) })
	case "resend":
		requirePost(w, r, func() error { return h.svc.Resend(id) })
	case "clear":
		requirePost(w, r, func() error { return h.svc.Clear(id) })
	case "kill-shell":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Shell string `json:"shell"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		killed, err := h.svc.KillShell(id, in.Shell)
		writeJSON(w, map[string]bool{"killed": killed}, err)
	case "detach-bash":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		detached, err := h.svc.DetachBash(id)
		writeJSON(w, map[string]bool{"detached": detached}, err)
	case "compact":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Target int `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		before, after, err := h.svc.Compact(id, in.Target)
		writeJSON(w, map[string]int{"before": before, "after": after}, err)
	case "goal":
		h.setting(w, r, func(v string) error { return h.svc.SetGoal(id, v) })
	case "title":
		h.setting(w, r, func(v string) error { return h.svc.SetTitle(id, v) })
	case "add-dir":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		root, err := h.svc.AddDir(id, in.Path)
		writeJSON(w, map[string]string{"root": root}, err)
	case "model":
		h.setting(w, r, func(v string) error { return h.svc.SetModel(id, v) })
	case "effort":
		h.setting(w, r, func(v string) error { return h.svc.SetEffort(id, v) })
	case "perm":
		h.setting(w, r, func(v string) error { return h.svc.SetPerm(id, v) })
	case "search":
		h.setting(w, r, func(v string) error { return h.svc.SetSearch(id, v) })
	case "fast":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var in struct {
			Value bool `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true}, h.svc.SetFast(id, in.Value))
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown session action %q", action))
	}
}

func (h *handler) setting(w http.ResponseWriter, r *http.Request, fn func(string) error) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var in struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true}, fn(in.Value))
}

func (h *handler) events(w http.ResponseWriter, r *http.Request, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	stream, events, err := h.svc.Events(r.Context(), id)
	if err != nil {
		writeSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}
	defer stream.Close()
	writeSSE(w, "ready", map[string]string{"id": id})
	flusher.Flush()
	for ev := range events {
		writeSSE(w, "event", ev)
		flusher.Flush()
	}
}

func requirePost(w http.ResponseWriter, r *http.Request, fn func() error) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, map[string]bool{"ok": true}, fn())
}

func writeJSON(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}

func writeSSE(w http.ResponseWriter, event string, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if p, err := exec.LookPath("xdg-open"); err == nil {
			cmd = exec.Command(p, url)
		} else if p, err := exec.LookPath("gio"); err == nil {
			cmd = exec.Command(p, "open", url)
		}
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}
