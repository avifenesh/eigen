package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
)

// Bridge connects a Telegram chat to the eigen daemon: each authorized chat is
// a VIEW onto a daemon session (the same multiplexed session a TUI window
// attaches to — so you start on the machine and continue on your phone, same
// session). It long-polls Telegram, relays your messages as session input,
// streams agent events back as an edited message, and renders gated-tool
// approvals as inline ✅/❌ buttons.
//
// Security: only chat ids in `allow` are served; everything else is ignored
// (fail-closed). The bridge is a thin view — it inherits the daemon's perm
// gating, so approvals still apply.
type Bridge struct {
	bot   *Bot
	dial  func() (*daemon.Client, error)
	allow map[int64]bool

	mu     sync.Mutex
	chats  map[int64]*chatState // per-chat attached session + streaming msg
	offset int64
}

type chatState struct {
	client    *daemon.Client
	sessionID string
	// streaming message being edited as the agent's answer grows
	streamMsg int64
	streamBuf strings.Builder
	lastEdit  time.Time
	// pending approval id (set when an approval button is shown)
	pendingApproval string
}

// NewBridge builds a bridge. dial opens a fresh daemon.Client (a per-chat
// connection, since each carries its own event stream); allow is the chat-id
// allowlist.
func NewBridge(bot *Bot, dial func() (*daemon.Client, error), allow []int64) *Bridge {
	m := map[int64]bool{}
	for _, id := range allow {
		m[id] = true
	}
	return &Bridge{bot: bot, dial: dial, allow: m, chats: map[int64]*chatState{}}
}

// Run long-polls until ctx is cancelled. Panics in update handling are
// recovered so one bad update can't kill the bridge process.
func (br *Bridge) Run(ctx context.Context) error {
	me, err := br.bot.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram: cannot reach bot (check token): %w", err)
	}
	fmt.Printf("eigen telegram: bot @%s online; serving %d chat(s)\n", me, len(br.allow))
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ups, err := br.bot.GetUpdates(ctx, br.offset, 50)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 409 Conflict = another poller holds this bot (a second bridge /
			// daemon instance). Back off longer and retry — don't thrash; the
			// other one may stop, and only one bridge should win.
			delay := 3 * time.Second
			if isConflict(err) {
				delay = 30 * time.Second
				fmt.Println("eigen telegram: another poller holds this bot (409) — backing off")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		for _, up := range ups {
			br.offset = up.UpdateID + 1
			br.handle(ctx, up)
		}
	}
}

func (br *Bridge) handle(ctx context.Context, up Update) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("eigen telegram: recovered from panic handling update: %v\n", r)
		}
	}()
	switch {
	case up.CallbackQuery != nil:
		br.onCallback(ctx, up.CallbackQuery)
	case up.Message != nil && up.Message.Text != "":
		br.onMessage(ctx, up.Message)
	}
}

// authorized reports whether a chat is allowed (fail-closed).
func (br *Bridge) authorized(chatID int64) bool { return br.allow[chatID] }

func (br *Bridge) onMessage(ctx context.Context, m *Message) {
	chatID := m.Chat.ID
	if !br.authorized(chatID) {
		// Silent for unknown chats, except a one-time hint with the chat id so
		// the user can allowlist themselves.
		_, _ = br.bot.Send(ctx, chatID, fmt.Sprintf("not authorized. add this chat id to eigen's telegram allowlist: %d", chatID), nil)
		return
	}
	text := strings.TrimSpace(m.Text)
	if strings.HasPrefix(text, "/") {
		br.onCommand(ctx, chatID, text)
		return
	}
	cs := br.chat(chatID)
	if cs == nil || cs.sessionID == "" {
		_, _ = br.bot.Send(ctx, chatID, "no session attached. /sessions to list, /attach <id> to pick one, or /new <dir> to start one.", nil)
		return
	}
	// Send the message to the attached session: steer if a turn is running,
	// else a fresh turn. A new streaming message will carry the reply.
	br.resetStream(cs)
	steered, err := cs.client.SteerInput(cs.sessionID, text, nil)
	if err != nil {
		_, _ = br.bot.Send(ctx, chatID, "send failed: "+err.Error(), nil)
		return
	}
	if steered {
		_, _ = br.bot.Send(ctx, chatID, "↪ steered into the running turn", nil)
	}
}

func (br *Bridge) chat(chatID int64) *chatState {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.chats[chatID]
}

// resetStream prepares a fresh streaming message for the next reply.
func (br *Bridge) resetStream(cs *chatState) {
	br.mu.Lock()
	cs.streamMsg = 0
	cs.streamBuf.Reset()
	br.mu.Unlock()
}
