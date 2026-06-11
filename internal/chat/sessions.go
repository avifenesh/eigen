package chat

// SessionEntry describes one daemon-hosted session for the in-chat switcher
// (alt+s): enough to render a row and decide where to hop.
type SessionEntry struct {
	ID      string
	Title   string
	Dir     string
	Model   string
	Status  string // "idle" | "working" | "approval" | "error"
	Turns   int
	Updated int64 // unix nano
}

// SessionLister is implemented by backends that can enumerate sibling
// sessions (daemon-hosted chats) for in-window switching. Local chats have no
// siblings and don't implement it.
type SessionLister interface {
	Sessions() []SessionEntry
	SessionID() string // this backend's session id
}

// Detacher is implemented by backends whose session outlives the view.
// Detach tells the backend the view is leaving: a running turn must keep
// running (the view's context cancellation must NOT interrupt it), and any
// blocked Send returns immediately.
type Detacher interface{ Detach() }
