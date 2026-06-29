package gui

import "strings"

// NotifyChatReply fires a desktop notification when a session finishes a turn
// and the user is not viewing that chat (best-effort via notify-send).
func (b *Bridge) NotifyChatReply(sessionID, title string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	label := strings.TrimSpace(title)
	if label == "" {
		label = "Chat session"
	}
	notifyDesktop("eigen — "+label, "New reply ready", "normal")
	return nil
}
