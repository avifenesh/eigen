package connector

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// callbackResult carries what the OAuth redirect delivered.
type callbackResult struct {
	code  string
	state string
	err   string // OAuth "error" param (e.g. access_denied), "" on success
}

// loopbackServer is a one-shot localhost HTTP server that catches the OAuth
// redirect. OAuth 2.1 for native apps uses a loopback redirect
// (http://127.0.0.1:<port>/callback) — the browser sends the user back here
// with the authorization code. The port is OS-assigned so concurrent flows
// don't collide.
type loopbackServer struct {
	srv      *http.Server
	redirect string
	results  chan callbackResult
}

// newLoopbackServer binds an ephemeral localhost port and starts serving the
// callback path. Call redirectURI() for the URL to register, wait() for the
// result, and close() when done.
func newLoopbackServer(path string) (*loopbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("connector: bind loopback callback: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ls := &loopbackServer{
		redirect: fmt.Sprintf("http://127.0.0.1:%d%s", port, path),
		results:  make(chan callbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := callbackResult{
			code:  q.Get("code"),
			state: q.Get("state"),
			err:   q.Get("error"),
		}
		// Tell the user they can close the tab; deliver the result to wait().
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != "" || res.code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, callbackHTML("Authorization failed", "You can close this tab and return to Eigen."))
		} else {
			fmt.Fprint(w, callbackHTML("Connected", "Authorization complete — you can close this tab and return to Eigen."))
		}
		select {
		case ls.results <- res:
		default:
		}
	})
	ls.srv = &http.Server{Handler: mux}
	go ls.srv.Serve(ln)
	return ls, nil
}

func (ls *loopbackServer) redirectURI() string { return ls.redirect }

// wait blocks for the redirect (or ctx cancel / timeout).
func (ls *loopbackServer) wait(ctx context.Context) (callbackResult, error) {
	select {
	case res := <-ls.results:
		return res, nil
	case <-ctx.Done():
		return callbackResult{}, ctx.Err()
	}
}

func (ls *loopbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = ls.srv.Shutdown(ctx)
}

// callbackHTML renders the minimal page shown in the browser after redirect.
func callbackHTML(title, body string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>Eigen</title>
<style>body{font-family:system-ui,sans-serif;background:#0f1419;color:#e6e6e6;display:flex;
align-items:center;justify-content:center;height:100vh;margin:0}
.card{text-align:center;padding:2rem 3rem;border:1px solid #2a3340;border-radius:12px;background:#161b22}
h1{font-size:1.3rem;margin:0 0 .5rem}p{color:#9aa4b2;margin:0}</style></head>
<body><div class="card"><h1>` + title + `</h1><p>` + body + `</p></div></body></html>`
}
