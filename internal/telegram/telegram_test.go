package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTG is a stand-in Telegram API server.
func fakeTG(t *testing.T, handler func(method string, r *http.Request) any) *Bot {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		method := strings.TrimPrefix(r.URL.Path, "/botTEST/")
		result := handler(method, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	t.Cleanup(srv.Close)
	b := New("TEST")
	b.api = srv.URL + "/botTEST"
	return b
}

func TestBotSendAndGetMe(t *testing.T) {
	var sentText string
	bot := fakeTG(t, func(method string, r *http.Request) any {
		switch method {
		case "getMe":
			return map[string]any{"id": 1, "username": "eigenbot"}
		case "sendMessage":
			sentText = r.FormValue("text")
			return map[string]any{"message_id": 42}
		}
		return nil
	})
	if name, err := bot.GetMe(context.Background()); err != nil || name != "eigenbot" {
		t.Fatalf("getMe: %q %v", name, err)
	}
	id, err := bot.Send(context.Background(), 100, "hello", nil)
	if err != nil || id != 42 {
		t.Fatalf("send: id=%d err=%v", id, err)
	}
	if sentText != "hello" {
		t.Fatalf("text not sent: %q", sentText)
	}
}

func TestBotInlineKeyboard(t *testing.T) {
	var markup string
	bot := fakeTG(t, func(method string, r *http.Request) any {
		if method == "sendMessage" {
			markup = r.FormValue("reply_markup")
			return map[string]any{"message_id": 1}
		}
		return nil
	})
	bot.Send(context.Background(), 1, "approve?", Buttons([2]string{"✅", "approve:x"}, [2]string{"❌", "deny:x"}))
	if !strings.Contains(markup, "inline_keyboard") || !strings.Contains(markup, "approve:x") {
		t.Fatalf("inline keyboard not encoded: %q", markup)
	}
}

func TestBridgeAuthAllowlist(t *testing.T) {
	var sentTo []int64
	bot := fakeTG(t, func(method string, r *http.Request) any {
		if method == "sendMessage" {
			// record chat_id
			var id int64
			json.Unmarshal([]byte(r.FormValue("chat_id")), &id)
			sentTo = append(sentTo, id)
			return map[string]any{"message_id": 1}
		}
		return nil
	})
	br := NewBridge(bot, nil, []int64{777})
	// Unauthorized chat → gets the "not authorized" reply, never reaches daemon.
	br.onMessage(context.Background(), &Message{Chat: &Chat{ID: 999}, Text: "hi"})
	if !br.authorized(777) || br.authorized(999) {
		t.Fatal("allowlist wrong")
	}
}

func TestClampMsg(t *testing.T) {
	if clampMsg("") != "…" {
		t.Fatal("empty must become a placeholder (Telegram rejects empty)")
	}
	long := strings.Repeat("x", 5000)
	if len(clampMsg(long)) > maxTelegramMsg+5 {
		t.Fatal("long messages must be clamped")
	}
}
