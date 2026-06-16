package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
)

// onCommand handles /slash commands.
func (br *Bridge) onCommand(ctx context.Context, chatID int64, line string) {
	fields := strings.Fields(line)
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, cmd))
	switch cmd {
	case "/start", "/help":
		br.bot.Send(ctx, chatID, helpText, nil)
	case "/sessions", "/list":
		br.listSessions(ctx, chatID)
	case "/attach":
		if arg == "" {
			br.bot.Send(ctx, chatID, "usage: /attach <session-id>  (see /sessions)", nil)
			return
		}
		br.attach(ctx, chatID, arg)
	case "/new":
		br.newSession(ctx, chatID, arg)
	case "/stop", "/interrupt":
		br.interrupt(ctx, chatID)
	case "/whoami":
		br.bot.Send(ctx, chatID, fmt.Sprintf("chat id: %d", chatID), nil)
	default:
		br.bot.Send(ctx, chatID, "unknown command. /help", nil)
	}
}

const helpText = `eigen on Telegram — same sessions as your machine.

/sessions — list daemon sessions
/attach <id> — follow a session (same one your terminal sees); replays recent history then streams live
/new <dir> — start a new session rooted at <dir>
/stop — interrupt the running turn
just type — send a message to the attached session (steers a running turn)

Gated tools ask for approval with ✅ / ❌ buttons.`

// connFor returns the chat's daemon client, dialing + storing one on first use.
func (br *Bridge) connFor(chatID int64) (*chatState, error) {
	br.mu.Lock()
	defer br.mu.Unlock()
	if cs := br.chats[chatID]; cs != nil && cs.client != nil {
		return cs, nil
	}
	c, err := br.dial()
	if err != nil {
		return nil, err
	}
	cs := &chatState{client: c}
	br.chats[chatID] = cs
	return cs, nil
}

func (br *Bridge) listSessions(ctx context.Context, chatID int64) {
	cs, err := br.connFor(chatID)
	if err != nil {
		br.bot.Send(ctx, chatID, "daemon unavailable: "+err.Error(), nil)
		return
	}
	infos, err := cs.client.List()
	if err != nil {
		br.bot.Send(ctx, chatID, "list failed: "+err.Error(), nil)
		return
	}
	if len(infos) == 0 {
		br.bot.Send(ctx, chatID, "no sessions. /new <dir> to start one.", nil)
		return
	}
	var b strings.Builder
	b.WriteString("sessions (/attach <id>):\n")
	for _, s := range infos {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		mark := ""
		if s.ID == cs.sessionID {
			mark = " ← attached"
		}
		fmt.Fprintf(&b, "\n%s — %s\n  %s%s", s.ID, title, s.Dir, mark)
	}
	br.bot.Send(ctx, chatID, b.String(), nil)
}

// attach makes the chat follow a session: replays recent history, then streams
// live (the same session a terminal view sees).
func (br *Bridge) attach(ctx context.Context, chatID int64, sessionID string) {
	cs, err := br.connFor(chatID)
	if err != nil {
		br.bot.Send(ctx, chatID, "daemon unavailable: "+err.Error(), nil)
		return
	}
	// Show where the session is: its recent history.
	if st, err := cs.client.State(sessionID); err == nil && st != nil {
		br.bot.Send(ctx, chatID, "attached "+sessionID+" — "+sessionHeadline(st), nil)
		if recap := recentHistory(st.Messages, 6); recap != "" {
			br.bot.Send(ctx, chatID, recap, nil)
		}
	} else {
		br.bot.Send(ctx, chatID, "attached "+sessionID, nil)
	}
	br.mu.Lock()
	cs.sessionID = sessionID
	br.mu.Unlock()
	// Attach the event stream (replays buffered events then follows live). The
	// daemon multiplexes — a terminal can be attached to the same id too.
	go func() {
		_ = cs.client.Attach(sessionID, func(e daemon.WireEvent, replay bool) {
			if replay {
				return // we already showed history via State; skip replayed events
			}
			br.onEvent(context.Background(), chatID, cs, e)
		})
	}()
}

func (br *Bridge) newSession(ctx context.Context, chatID int64, dir string) {
	if dir == "" {
		dir = "."
	}
	cs, err := br.connFor(chatID)
	if err != nil {
		br.bot.Send(ctx, chatID, "daemon unavailable: "+err.Error(), nil)
		return
	}
	id, err := cs.client.NewSession(dir, "", "", nil)
	if err != nil {
		br.bot.Send(ctx, chatID, "new failed: "+err.Error(), nil)
		return
	}
	br.bot.Send(ctx, chatID, "started session "+id+" at "+dir, nil)
	br.attach(ctx, chatID, id)
}

func (br *Bridge) interrupt(ctx context.Context, chatID int64) {
	cs := br.chat(chatID)
	if cs == nil || cs.sessionID == "" {
		br.bot.Send(ctx, chatID, "no session attached", nil)
		return
	}
	if err := cs.client.Interrupt(cs.sessionID); err != nil {
		br.bot.Send(ctx, chatID, "interrupt failed: "+err.Error(), nil)
		return
	}
	br.bot.Send(ctx, chatID, "interrupted", nil)
}

// onEvent renders a live agent event to Telegram: text streams into an
// edited-in-place message; approvals show ✅/❌ buttons; notes/tools are terse.
func (br *Bridge) onEvent(ctx context.Context, chatID int64, cs *chatState, e daemon.WireEvent) {
	switch e.Kind {
	case "text":
		br.streamText(ctx, chatID, cs, e.Text)
	case "tool_start":
		br.bot.Send(ctx, chatID, "⚙ "+e.ToolName, nil)
	case "approval":
		br.mu.Lock()
		cs.pendingApproval = e.Result
		br.mu.Unlock()
		body := "approve tool?"
		if e.Text != "" {
			body = "approve: " + e.Text
		}
		br.bot.Send(ctx, chatID, body, Buttons(
			[2]string{"✅ approve", "approve:" + e.Result},
			[2]string{"❌ deny", "deny:" + e.Result},
		))
	case "done":
		br.flushStream(ctx, chatID, cs, e.Text)
		br.resetStream(cs)
	case "note":
		if strings.TrimSpace(e.Text) != "" {
			br.bot.Send(ctx, chatID, "· "+e.Text, nil)
		}
	}
}

// streamText accumulates assistant text and edits the streaming message,
// throttled so we don't hammer Telegram's rate limit.
func (br *Bridge) streamText(ctx context.Context, chatID int64, cs *chatState, delta string) {
	br.mu.Lock()
	cs.streamBuf.WriteString(delta)
	text := cs.streamBuf.String()
	msg := cs.streamMsg
	throttled := time.Since(cs.lastEdit) < 1200*time.Millisecond
	br.mu.Unlock()
	if msg == 0 {
		id, err := br.bot.Send(ctx, chatID, text, nil)
		if err == nil {
			br.mu.Lock()
			cs.streamMsg = id
			cs.lastEdit = time.Now()
			br.mu.Unlock()
		}
		return
	}
	if throttled {
		return // coalesce; the final flush (on done) catches up
	}
	if err := br.bot.Edit(ctx, chatID, msg, text, nil); err == nil {
		br.mu.Lock()
		cs.lastEdit = time.Now()
		br.mu.Unlock()
	}
}

// flushStream writes the final answer text (the authoritative full text).
func (br *Bridge) flushStream(ctx context.Context, chatID int64, cs *chatState, final string) {
	br.mu.Lock()
	msg := cs.streamMsg
	if final == "" {
		final = cs.streamBuf.String()
	}
	br.mu.Unlock()
	if strings.TrimSpace(final) == "" {
		return
	}
	if msg == 0 {
		br.bot.Send(ctx, chatID, final, nil)
		return
	}
	br.bot.Edit(ctx, chatID, msg, final, nil)
}

// onCallback handles an inline-button tap (approve/deny).
func (br *Bridge) onCallback(ctx context.Context, q *CallbackQuery) {
	if q.Message == nil || !br.authorized(q.Message.Chat.ID) {
		return
	}
	chatID := q.Message.Chat.ID
	cs := br.chat(chatID)
	if cs == nil || cs.sessionID == "" {
		br.bot.AnswerCallback(ctx, q.ID, "no session")
		return
	}
	allow := strings.HasPrefix(q.Data, "approve:")
	id := strings.TrimPrefix(strings.TrimPrefix(q.Data, "approve:"), "deny:")
	if err := cs.client.Approve(cs.sessionID, id, allow); err != nil {
		br.bot.AnswerCallback(ctx, q.ID, "failed: "+err.Error())
		return
	}
	verb := "approved"
	if !allow {
		verb = "denied"
	}
	br.bot.AnswerCallback(ctx, q.ID, verb)
	br.bot.Edit(ctx, chatID, q.Message.MessageID, q.Message.Text+"\n→ "+verb, nil)
}

// --- small render helpers ----------------------------------------------------

func sessionHeadline(st *daemon.SessionState) string {
	t := st.Title
	if t == "" {
		t = "(untitled)"
	}
	status := "idle"
	if st.Running {
		status = "running"
	}
	return fmt.Sprintf("%s · %s · %s", t, st.Model, status)
}

// recentHistory renders the last n user/assistant messages so a phone attach
// shows where the session is.
func recentHistory(msgs []llm.Message, n int) string {
	if len(msgs) == 0 {
		return ""
	}
	start := len(msgs) - n
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	b.WriteString("recent:")
	for _, m := range msgs[start:] {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant {
			continue
		}
		txt := strings.TrimSpace(m.Text)
		if txt == "" {
			continue
		}
		if len(txt) > 300 {
			txt = txt[:300] + "…"
		}
		fmt.Fprintf(&b, "\n\n%s: %s", m.Role, txt)
	}
	return b.String()
}
