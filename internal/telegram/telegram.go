// Package telegram is a minimal, dependency-free Telegram Bot API client for
// eigen's phone bridge. It long-polls getUpdates (outbound HTTPS — no inbound
// listener, no port, satisfying eigen's "no raw network endpoint" rule) and
// sends/edits messages with inline keyboards (for tap-to-approve). It is NOT a
// full bot framework — just the handful of primitives the eigen bridge needs.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Bot is a Telegram bot client bound to one token.
type Bot struct {
	token string
	http  *http.Client
	api   string // override for tests
}

// New returns a Bot for the given token.
func New(token string) *Bot {
	return &Bot{
		token: token,
		http:  &http.Client{Timeout: 70 * time.Second}, // > long-poll timeout
	}
}

func (b *Bot) base() string {
	if b.api != "" {
		return b.api
	}
	return "https://api.telegram.org/bot" + b.token
}

// --- update types (only the fields we use) ----------------------------------

// Update is one polled update.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// Message is an incoming text message.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      *Chat  `json:"chat"`
	Text      string `json:"text"`
}

// User is a Telegram user.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Chat is a Telegram chat.
type Chat struct {
	ID int64 `json:"id"`
}

// CallbackQuery is an inline-button tap.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// --- API calls ---------------------------------------------------------------

func (b *Bot) call(ctx context.Context, method string, params url.Values, out any) error {
	u := b.base() + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("telegram %s: bad response: %s", method, truncate(string(body), 200))
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// GetMe verifies the token and returns the bot's username.
func (b *Bot) GetMe(ctx context.Context) (string, error) {
	var u User
	if err := b.call(ctx, "getMe", url.Values{}, &u); err != nil {
		return "", err
	}
	return u.Username, nil
}

// GetUpdates long-polls for updates after offset (exclusive). timeout is the
// server-side long-poll seconds.
func (b *Bot) GetUpdates(ctx context.Context, offset int64, timeoutSecs int) ([]Update, error) {
	p := url.Values{}
	p.Set("offset", strconv.FormatInt(offset, 10))
	p.Set("timeout", strconv.Itoa(timeoutSecs))
	// Only the update types we handle.
	p.Set("allowed_updates", `["message","callback_query"]`)
	var ups []Update
	if err := b.call(ctx, "getUpdates", p, &ups); err != nil {
		return nil, err
	}
	return ups, nil
}

// Send sends a text message to chat, returning the new message id. markup may be
// nil (no buttons).
func (b *Bot) Send(ctx context.Context, chatID int64, text string, markup *InlineKeyboard) (int64, error) {
	p := url.Values{}
	p.Set("chat_id", strconv.FormatInt(chatID, 10))
	p.Set("text", clampMsg(text))
	if markup != nil {
		j, _ := json.Marshal(markup)
		p.Set("reply_markup", string(j))
	}
	var m Message
	if err := b.call(ctx, "sendMessage", p, &m); err != nil {
		return 0, err
	}
	return m.MessageID, nil
}

// Edit replaces the text (and markup) of a previously-sent message — used to
// stream the agent's growing reply in place instead of spamming new messages.
func (b *Bot) Edit(ctx context.Context, chatID, messageID int64, text string, markup *InlineKeyboard) error {
	p := url.Values{}
	p.Set("chat_id", strconv.FormatInt(chatID, 10))
	p.Set("message_id", strconv.FormatInt(messageID, 10))
	p.Set("text", clampMsg(text))
	if markup != nil {
		j, _ := json.Marshal(markup)
		p.Set("reply_markup", string(j))
	}
	err := b.call(ctx, "editMessageText", p, nil)
	// Telegram errors if the text is identical to what's already there; that's
	// benign for a streaming editor.
	if err != nil && isNotModified(err) {
		return nil
	}
	return err
}

// AnswerCallback acknowledges a button tap (stops the client's spinner). text
// may be empty.
func (b *Bot) AnswerCallback(ctx context.Context, callbackID, text string) error {
	p := url.Values{}
	p.Set("callback_query_id", callbackID)
	if text != "" {
		p.Set("text", text)
	}
	return b.call(ctx, "answerCallbackQuery", p, nil)
}

// --- inline keyboards --------------------------------------------------------

// InlineKeyboard is a grid of callback buttons.
type InlineKeyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}

// Button is one inline button: a label + the callback data sent back on tap.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

// Buttons builds a single-row keyboard from label/data pairs.
func Buttons(pairs ...[2]string) *InlineKeyboard {
	var row []Button
	for _, p := range pairs {
		row = append(row, Button{Text: p[0], Data: p[1]})
	}
	return &InlineKeyboard{Rows: [][]Button{row}}
}

// --- helpers -----------------------------------------------------------------

// maxTelegramMsg is Telegram's 4096-char message limit; we clamp under it.
const maxTelegramMsg = 4000

func clampMsg(s string) string {
	if s == "" {
		return "…" // Telegram rejects empty text
	}
	if len(s) > maxTelegramMsg {
		// keep the TAIL (the latest output is what matters when streaming)
		return "…" + s[len(s)-maxTelegramMsg:]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func isNotModified(err error) bool {
	return err != nil && bytes.Contains([]byte(err.Error()), []byte("not modified"))
}
