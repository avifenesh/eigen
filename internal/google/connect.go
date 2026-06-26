package google

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// callbackPath is the loopback redirect path the authorization code lands on.
const callbackPath = "/eigen/google/callback"

// authFlowTimeout bounds how long we wait for the user to finish consent.
const authFlowTimeout = 5 * time.Minute

// Connect runs the interactive loopback authorization-code flow: bind an
// ephemeral localhost port, open the Google consent page (offline access so we
// get a refresh token), catch the redirect, exchange the code, and persist the
// token. Returns when the refresh token is stored, or on error/timeout.
func (a *Auth) Connect(ctx context.Context) error {
	a.mu.Lock()
	if !a.haveC {
		a.mu.Unlock()
		return fmt.Errorf("google: no OAuth client configured — %s", SetupHint())
	}
	cfg := a.oauthConfig("") // redirect filled in after we bind the port
	a.mu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("google: bind loopback callback: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	cfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)

	results := make(chan callbackResult, 1)
	srv := &http.Server{Handler: callbackHandler(callbackPath, results)}
	go srv.Serve(ln)
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	verifier := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,          // request a refresh token
		oauth2.ApprovalForce,              // force consent so a refresh token is re-issued
		oauth2.S256ChallengeOption(verifier),
	)
	if err := a.openFn(authURL); err != nil {
		return fmt.Errorf("google: open browser: %w (visit manually: %s)", err, authURL)
	}

	flowCtx, cancel := context.WithTimeout(ctx, authFlowTimeout)
	defer cancel()
	var res callbackResult
	select {
	case res = <-results:
	case <-flowCtx.Done():
		return fmt.Errorf("google: timed out waiting for authorization: %w", flowCtx.Err())
	}
	if res.err != "" {
		return fmt.Errorf("google: authorization denied: %s", res.err)
	}
	if res.state != state {
		return fmt.Errorf("google: state mismatch (possible CSRF) — aborting")
	}

	tok, err := cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("google: token exchange failed: %w", err)
	}
	if tok.RefreshToken == "" {
		return fmt.Errorf("google: no refresh token returned (revoke prior grant at myaccount.google.com and retry)")
	}

	a.mu.Lock()
	a.src = nil // force a fresh source next use
	a.mu.Unlock()
	if err := a.store.save(tok); err != nil {
		return fmt.Errorf("google: store token: %w", err)
	}
	return nil
}

// callbackResult carries what the OAuth redirect delivered.
type callbackResult struct {
	code  string
	state string
	err   string
}

func callbackHandler(path string, results chan callbackResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := callbackResult{code: q.Get("code"), state: q.Get("state"), err: q.Get("error")}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != "" || res.code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, callbackHTML("Authorization failed", "You can close this tab and return to Eigen."))
		} else {
			fmt.Fprint(w, callbackHTML("Google connected", "Authorization complete — close this tab and return to Eigen."))
		}
		select {
		case results <- res:
		default:
		}
	})
	return mux
}

func callbackHTML(title, body string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>Eigen</title>
<style>body{font-family:system-ui,sans-serif;background:#0f1419;color:#e6e6e6;display:flex;
align-items:center;justify-content:center;height:100vh;margin:0}
.card{text-align:center;padding:2rem 3rem;border:1px solid #2a3340;border-radius:12px;background:#161b22}
h1{font-size:1.3rem;margin:0 0 .5rem}p{color:#9aa4b2;margin:0}</style></head>
<body><div class="card"><h1>` + title + `</h1><p>` + body + `</p></div></body></html>`
}

// SetupHint is the one-line "how to enable Google" instruction shown when no
// BYO client is configured.
func SetupHint() string {
	return "create a Desktop OAuth client in Google Cloud Console (enable Calendar + Gmail APIs), download the JSON, and save it to ~/.config/eigen/google_client.json"
}
