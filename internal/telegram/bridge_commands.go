package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
)

// requireSession returns the chat's attached session, or sends a hint + false.
func (br *Bridge) requireSession(ctx context.Context, chatID int64) (*chatState, bool) {
	cs := br.chat(chatID)
	if cs == nil || cs.sessionID == "" {
		br.bot.Send(ctx, chatID, "no session attached — /sessions then /attach &lt;id&gt;", nil)
		return nil, false
	}
	return cs, true
}

// status shows the attached session's live state.
func (br *Bridge) status(ctx context.Context, chatID int64) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	st, err := cs.client.State(cs.sessionID)
	if err != nil || st == nil {
		br.bot.Send(ctx, chatID, "status unavailable: "+errText(err), nil)
		return
	}
	run := "idle"
	if st.Running {
		run = "⚙ running"
	}
	pct := 0
	if st.MaxTokens > 0 {
		pct = st.Tokens * 100 / st.MaxTokens
	}
	body := fmt.Sprintf(
		"<b>%s</b>\n<code>%s</code>\nmodel: %s\nperm: %s · %s\ncontext: %d / %d tok (%d%%)",
		escapeHTML(nz(st.Title, "(untitled)")), cs.sessionID,
		st.Model, st.Perm, run, st.Tokens, st.MaxTokens, pct)
	if st.Goal != "" {
		body += "\ngoal: " + escapeHTML(trunc(st.Goal, 200))
	}
	br.bot.Send(ctx, chatID, body, Buttons(
		[2]string{"⟳ refresh", "act:status"},
		[2]string{"⏹ stop", "act:stop"},
		[2]string{"♻ compact", "act:compact"},
	))
}

// modelCmd switches the model, or (no arg) shows an inline picker of the
// chat-capable catalog models.
func (br *Bridge) modelCmd(ctx context.Context, chatID int64, arg string) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	if arg == "" {
		var rows [][]Button
		for _, m := range llm.Models() {
			rows = append(rows, []Button{{Text: m.ID, Data: "model:" + m.ID}})
			if len(rows) >= 12 {
				break
			}
		}
		if len(rows) == 0 {
			br.bot.Send(ctx, chatID, "no models in the catalog", nil)
			return
		}
		br.bot.Send(ctx, chatID, "pick a model:", &InlineKeyboard{Rows: rows})
		return
	}
	br.applyModel(ctx, chatID, cs, arg)
}

func (br *Bridge) applyModel(ctx context.Context, chatID int64, cs *chatState, model string) {
	if err := cs.client.SetModel(cs.sessionID, model); err != nil {
		br.bot.Send(ctx, chatID, "model switch failed: "+errText(err), nil)
		return
	}
	br.bot.Send(ctx, chatID, "model → <b>"+escapeHTML(model)+"</b>", nil)
}

// permCmd toggles/sets the permission posture.
func (br *Bridge) permCmd(ctx context.Context, chatID int64, arg string) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	arg = strings.TrimSpace(strings.ToLower(arg))
	if arg == "" {
		br.bot.Send(ctx, chatID, "perm: send <code>/perm gated</code> or <code>/perm auto</code>", Buttons(
			[2]string{"gated (ask)", "perm:gated"},
			[2]string{"auto (run)", "perm:auto"},
		))
		return
	}
	if arg != "gated" && arg != "auto" {
		br.bot.Send(ctx, chatID, "perm must be gated|auto", nil)
		return
	}
	if err := cs.client.SetPerm(cs.sessionID, arg); err != nil {
		br.bot.Send(ctx, chatID, "perm failed: "+errText(err), nil)
		return
	}
	br.bot.Send(ctx, chatID, "perm → <b>"+arg+"</b>", nil)
}

// goalCmd sets/clears/shows the session goal.
func (br *Bridge) goalCmd(ctx context.Context, chatID int64, arg string) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	switch strings.TrimSpace(arg) {
	case "":
		st, _ := cs.client.State(cs.sessionID)
		if st != nil && st.Goal != "" {
			br.bot.Send(ctx, chatID, "goal: "+escapeHTML(st.Goal)+"\n(/goal clear to unset)", nil)
		} else {
			br.bot.Send(ctx, chatID, "no goal set — /goal &lt;text&gt;", nil)
		}
	case "clear", "none", "off":
		cs.client.SetGoal(cs.sessionID, "")
		br.bot.Send(ctx, chatID, "goal cleared", nil)
	default:
		if err := cs.client.SetGoal(cs.sessionID, arg); err != nil {
			br.bot.Send(ctx, chatID, "goal failed: "+errText(err), nil)
			return
		}
		br.bot.Send(ctx, chatID, "🎯 goal set", nil)
	}
}

func (br *Bridge) compactCmd(ctx context.Context, chatID int64) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	br.bot.Send(ctx, chatID, "compacting…", nil)
	before, after, err := cs.client.Compact(cs.sessionID, 0)
	if err != nil {
		br.bot.Send(ctx, chatID, "compact failed: "+errText(err), nil)
		return
	}
	br.bot.Send(ctx, chatID, fmt.Sprintf("♻ compacted: %d → %d messages", before, after), nil)
}

func (br *Bridge) clearCmd(ctx context.Context, chatID int64) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	if err := cs.client.Clear(cs.sessionID); err != nil {
		br.bot.Send(ctx, chatID, "clear failed: "+errText(err), nil)
		return
	}
	br.bot.Send(ctx, chatID, "🧹 conversation cleared (same session, fresh context)", nil)
}

func (br *Bridge) resendCmd(ctx context.Context, chatID int64) {
	cs, ok := br.requireSession(ctx, chatID)
	if !ok {
		return
	}
	br.resetStream(cs)
	if err := cs.client.Resend(cs.sessionID); err != nil {
		br.bot.Send(ctx, chatID, "resend failed: "+errText(err), nil)
		return
	}
	br.bot.SendChatAction(ctx, chatID, "typing")
}

func errText(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

func nz(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
