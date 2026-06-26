package google

import "sync"

var (
	defaultMu   sync.Mutex
	defaultAuth *Auth
)

// Default returns the process-wide Google auth broker (created on first use), so
// the agent tools, the GUI bridge, and the CLI share one token store + creds.
func Default() *Auth {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultAuth == nil {
		defaultAuth = NewAuth(newTokenStore(DefaultTokenPath()))
	}
	return defaultAuth
}
