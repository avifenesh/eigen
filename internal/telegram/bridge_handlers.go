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
	case "/status":
		br.status(ctx, chatID)
	case "/model":
		br.modelCmd(ctx, chatID, arg)
	case "/perm":
		br.permCmd(ctx, chatID, arg)
	case "/goal":
		br.goalCmd(ctx, chatID, arg)
	case "/compact":
		br.compactCmd(ctx, chatID)
	case "/clear":
		br.clearCmd(ctx, chatID)
	case "/resend":
		br.resendCmd(ctx, chatID)
	case "/whoami":
		br.bot.Send(ctx, chatID, fmt.Sprintf("chat id: <code>%d</code>", chatID), nil)
	default:
		br.bot.Send(ctx, chatID, "unknown command. /help", nil)
	}
}

const helpText = `<b>eigen on Telegram</b> — same sessions as your machine.

/sessions — list sessions (tap to attach)
/attach &lt;id&gt; — follow a session; replays recent history then streams live
/new &lt;dir&gt; — start a new session
/status — model, perm, context %, running
/model — switch model (inline picker)
/perm — gated (ask) / auto (run)
/goal &lt;text&gt; — set a north-star goal (/goal clear)
/compact — shrink the context
/clear — fresh context, same session
/resend — retry the last turn
/stop — interrupt the running turn

Just type to send a message (steers a running turn). Gated tools ask with ✅/❌ buttons.`

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
	b.WriteString("sessions (tap to attach):\n")
	var rows [][]Button
	for _, s := range infos {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		mark := ""
		if s.ID == cs.sessionID {
			mark = " ← attached"
		}
		fmt.Fprintf(&b, "\n<code>%s</code> — %s\n  <i>%s</i>%s", s.ID, escapeHTML(title), escapeHTML(s.Dir), mark)
		label := s.ID
		if len(title) <= 24 {
			label = s.ID + ": " + title
		}
		rows = append(rows, []Button{{Text: trunc(label, 40), Data: "attach:" + s.ID}})
		if len(rows) >= 10 {
			break
		}
	}
	br.bot.Send(ctx, chatID, b.String(), &InlineKeyboard{Rows: rows})
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
// edited-in-place message; approvals show ✅/❌ buttons; tools/notes are terse;
// reasoning is shown italic. Long final answers are split, not truncated.
func (br *Bridge) onEvent(ctx context.Context, chatID int64, cs *chatState, e daemon.WireEvent) {
	switch e.Kind {
	case "text":
		br.bot.SendChatAction(ctx, chatID, "typing")
		br.streamText(ctx, chatID, cs, e.Text)
	case "reasoning":
		br.bot.SendChatAction(ctx, chatID, "typing")
	case "tool_start":
		args := compactArgs(e.ToolArgs)
		line := "⚙ <b>" + escapeHTML(e.ToolName) + "</b>"
		if args != "" {
			line += " <code>" + escapeHTML(args) + "</code>"
		}
		br.bot.Send(ctx, chatID, line, nil)
	case "tool_result":
		if e.IsError && strings.TrimSpace(e.Result) != "" {
			br.bot.Send(ctx, chatID, "⚠ <code>"+escapeHTML(trunc(e.Result, 500))+"</code>", nil)
		}
	case "approval":
		br.mu.Lock()
		cs.pendingApproval = e.Result
		br.mu.Unlock()
		body := "🔒 approve tool?"
		if e.Text != "" {
			body = "🔒 approve: <code>" + escapeHTML(e.Text) + "</code>"
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
			br.bot.Send(ctx, chatID, "· <i>"+escapeHTML(e.Text)+"</i>", nil)
		}
	}
}

// streamText accumulates assistant text and edits the streaming message,
// throttled so we don't hammer Telegram's rate limit. Text is HTML-escaped
// (the agent's raw output may contain < & which would break HTML parse mode).
func (br *Bridge) streamText(ctx context.Context, chatID int64, cs *chatState, delta string) {
	br.mu.Lock()
	cs.streamBuf.WriteString(delta)
	text := cs.streamBuf.String()
	msg := cs.streamMsg
	throttled := time.Since(cs.lastEdit) < 1200*time.Millisecond
	br.mu.Unlock()
	rendered := escapeHTML(text)
	if len(rendered) > maxTelegramMsg {
		// While streaming past one message's worth, just show the tail live;
		// the final flush splits the whole thing into multiple messages.
		rendered = "…" + rendered[len(rendered)-maxTelegramMsg+1:]
	}
	if msg == 0 {
		id, err := br.bot.Send(ctx, chatID, rendered, nil)
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
	if err := br.bot.Edit(ctx, chatID, msg, rendered, nil); err == nil {
		br.mu.Lock()
		cs.lastEdit = time.Now()
		br.mu.Unlock()
	}
}

// flushStream writes the final answer text (the authoritative full text),
// splitting long answers across messages instead of truncating.
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
	rendered := escapeHTML(final)
	// Short enough to fit the existing streamed message → edit it in place.
	if msg != 0 && len(rendered) <= maxTelegramMsg {
		br.bot.Edit(ctx, chatID, msg, rendered, nil)
		return
	}
	// Long: replace the placeholder with the full split output.
	if msg != 0 {
		br.bot.Edit(ctx, chatID, msg, "📝 (full answer below)", nil)
	}
	br.bot.SendLong(ctx, chatID, rendered, nil)
}

// onCallback handles an inline-button tap: approve/deny, model/perm pickers,
// and quick actions.
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
	data := q.Data
	switch {
	case strings.HasPrefix(data, "approve:"), strings.HasPrefix(data, "deny:"):
		allow := strings.HasPrefix(data, "approve:")
		id := strings.TrimPrefix(strings.TrimPrefix(data, "approve:"), "deny:")
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
	case strings.HasPrefix(data, "model:"):
		br.bot.AnswerCallback(ctx, q.ID, "switching")
		br.applyModel(ctx, chatID, cs, strings.TrimPrefix(data, "model:"))
	case strings.HasPrefix(data, "perm:"):
		p := strings.TrimPrefix(data, "perm:")
		_ = cs.client.SetPerm(cs.sessionID, p)
		br.bot.AnswerCallback(ctx, q.ID, "perm "+p)
		br.bot.Send(ctx, chatID, "perm → <b>"+p+"</b>", nil)
	case data == "act:status":
		br.bot.AnswerCallback(ctx, q.ID, "")
		br.status(ctx, chatID)
	case data == "act:stop":
		br.bot.AnswerCallback(ctx, q.ID, "interrupting")
		_ = cs.client.Interrupt(cs.sessionID)
	case data == "act:compact":
		br.bot.AnswerCallback(ctx, q.ID, "compacting")
		br.compactCmd(ctx, chatID)
	case strings.HasPrefix(data, "attach:"):
		br.bot.AnswerCallback(ctx, q.ID, "attaching")
		br.attach(ctx, chatID, strings.TrimPrefix(data, "attach:"))
	default:
		br.bot.AnswerCallback(ctx, q.ID, "")
	}
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
	return fmt.Sprintf("%s · %s · %s", escapeHTML(t), st.Model, status)
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
		fmt.Fprintf(&b, "\n\n<b>%s:</b> %s", m.Role, escapeHTML(txt))
	}
	return b.String()
}

// compactArgs renders tool-call args as a short one-line hint (best-effort).
func compactArgs(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "{}" || s == "null" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	return trunc(s, 120)
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
