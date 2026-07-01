package gui

import (
	"context"
	"time"

	"github.com/avifenesh/eigen/internal/google"
)

// Google bridge — eigen's native Google integration (Calendar + Gmail) status +
// connect flow for the GUI. Unlike connectors (remote MCP servers), Google is a
// direct-REST built-in authorized with the user's own Google Cloud OAuth client
// over a loopback flow; this surfaces its setup/connect state.

// GoogleStatusDTO is the Google integration state for the GUI.
type GoogleStatusDTO struct {
	Configured bool   `json:"configured"` // a BYO Google Cloud client is present
	Connected  bool   `json:"connected"`  // an account is linked (refresh token stored)
	SetupHint  string `json:"setupHint"`  // how to add a client when not configured
	SetupURL   string `json:"setupUrl"`   // Google Cloud Console credentials page
	ClientPath string `json:"clientPath"` // where the imported client JSON lands
}

// GoogleStatus reports whether Google is configured (client creds present) and
// connected (account linked).
func (b *Bridge) GoogleStatus() (*GoogleStatusDTO, error) {
	a := google.Default()
	return &GoogleStatusDTO{
		Configured: a.Configured(),
		Connected:  a.Connected(),
		SetupHint:  google.SetupHint(),
		SetupURL:   google.SetupURL,
		ClientPath: google.ClientPath(),
	}, nil
}

// ImportGoogleClient opens a native file picker for the user's downloaded
// Google Cloud OAuth-client JSON and imports it (validates + copies to
// ClientPath). Returns true when a file was imported, false when the user
// cancelled. This is the in-app "set up Google" step. The picker is host-UI
// work behind promptForPath (wails.go); the tagless build fails closed and the
// Qt client passes a path to a plain-arg import instead.
func (b *Bridge) ImportGoogleClient() (bool, error) {
	path, err := b.promptForPath("Choose your Google OAuth client JSON", "", false)
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil // cancelled
	}
	if err := google.Default().ImportClient(path); err != nil {
		return false, err
	}
	return true, nil
}

// ConnectGoogle runs the loopback OAuth flow (opens the browser, waits for
// consent, stores the refresh token). Blocks until linked or it fails/times out
// — the frontend shows a spinner while it runs.
func (b *Bridge) ConnectGoogle() error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	return google.Default().Connect(ctx)
}

// DisconnectGoogle drops the stored Google token (unlinks the account).
func (b *Bridge) DisconnectGoogle() error {
	return google.Default().Disconnect()
}
