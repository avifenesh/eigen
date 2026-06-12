package tui

// Streamed sentence-by-sentence speech: the reply starts speaking at the
// first sentence boundary, mid-turn, instead of after the whole answer.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/agent"
)

func TestLastSentenceEnd(t *testing.T) {
	cases := []struct {
		in   string
		want string // spoken prefix ("" = no boundary yet)
	}{
		{"Hello there. And then", "Hello there. "},
		{"First. Second! Third? tail", "First. Second! Third? "},
		{"no boundary yet", ""},
		{"version 3.5 is out", ""},           // dot inside a number ≠ boundary
		{"line one\nline two", "line one\n"}, // newline is a boundary
		{"Header: body follows", "Header: "},
	}
	for _, c := range cases {
		cut := lastSentenceEnd(c.in)
		if got := c.in[:cut]; got != c.want {
			t.Errorf("lastSentenceEnd(%q): spoke %q, want %q", c.in, got, c.want)
		}
	}
}

// recordTTS records Speak calls; each call returns immediately.
type recordTTS struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordTTS) Available() bool { return true }
func (r *recordTTS) Speak(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, text)
	return nil
}
func (r *recordTTS) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.calls, "")
}

func TestSpeechQueueSpeaksAllAndCloses(t *testing.T) {
	tts := &recordTTS{}
	q := newSpeechQueue(context.Background(), tts)
	q.Push("One. ")
	q.Push("Two. ")
	q.Close()
	select {
	case <-q.done:
	case <-time.After(2 * time.Second):
		t.Fatal("queue did not drain")
	}
	if got := tts.joined(); got != "One. Two. " {
		t.Fatalf("spoke %q", got)
	}
}

func TestSpeechQueueStopCutsImmediately(t *testing.T) {
	q := newSpeechQueue(context.Background(), blockingTTS{})
	q.Push("a very long sentence being spoken. ")
	time.Sleep(50 * time.Millisecond) // let Speak start and block
	q.Stop()
	select {
	case <-q.done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not end the queue")
	}
}

func TestStreamedDeltasSpeakMidTurn(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tts := &recordTTS{}
	m.tts = tts
	m.voiceOn = true
	m.state = stRunning
	// Deltas stream in; the complete sentence should be PUSHED mid-turn.
	m.renderEvent(agent.Event{Kind: agent.EventTextDelta, Text: "The answer "})
	m.renderEvent(agent.Event{Kind: agent.EventTextDelta, Text: "is 42. And the rest"})
	if m.speech == nil {
		t.Fatal("streamed deltas should start a speech queue")
	}
	deadline := time.Now().Add(2 * time.Second)
	for tts.joined() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := tts.joined(); got != "The answer is 42. " {
		t.Fatalf("mid-turn speech spoke %q, want the complete sentence only", got)
	}
	if m.speechBuf != "And the rest" {
		t.Fatalf("tail should stay buffered, got %q", m.speechBuf)
	}
	// Turn ends: voiceTurnDone flushes the tail and waits for drain, then
	// relistens — WITHOUT speaking the whole answer again.
	m.stt = &fakeSTT{}
	m.state = stInput
	m.text("assistant", "The answer is 42. And the rest")
	_, cmd := m.Update(turnDoneMsg{})
	if m.voiceMic != voiceSpeaking {
		t.Fatalf("draining stream should read as speaking, mic=%v", m.voiceMic)
	}
	done := drainForMsg[voiceSpeechDoneMsg](t, cmd)
	if got := tts.joined(); got != "The answer is 42. And the rest" {
		t.Fatalf("after flush spoke %q — must be exactly the streamed answer, no repeat", got)
	}
	m.Update(done)
	if m.voiceMic != voiceListening {
		t.Fatalf("after the stream drains the mic should listen, mic=%v", m.voiceMic)
	}
}

func TestReadAloudStreamDoesNotRepeatAnswer(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tts := &recordTTS{}
	spk := &fakeSpeaker{avail: true}
	m.tts = tts
	m.speaker = spk
	m.readAloud = true
	m.state = stRunning
	m.renderEvent(agent.Event{Kind: agent.EventTextDelta, Text: "Streamed sentence. "})
	m.state = stInput
	m.text("assistant", "Streamed sentence.")
	m.Update(turnDoneMsg{})
	if len(spk.spoken) != 0 {
		t.Fatalf("read-aloud must not re-speak a streamed answer, spoke %q", spk.spoken)
	}
}

func TestEscDropsStreamedSpeech(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.tts = blockingTTS{}
	m.voiceOn = true
	m.state = stRunning
	m.cancel = func() {}
	m.renderEvent(agent.Event{Kind: agent.EventTextDelta, Text: "Long answer. "})
	q := m.speech
	if q == nil {
		t.Fatal("expected speech queue")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case <-q.done:
	case <-time.After(2 * time.Second):
		t.Fatal("esc should stop streamed speech")
	}
	if m.speech != nil {
		t.Fatal("speech queue should be cleared")
	}
}
