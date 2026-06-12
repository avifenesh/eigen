package tui

// Streaming speech (Tier 15): start speaking as soon as the first sentence of
// the reply lands instead of waiting for the whole answer. Deltas feed a
// sentence buffer; complete sentences go into a speechQueue whose single
// speaker loop coalesces everything that accumulated while the previous chunk
// played — one TTS process per BATCH, so Kokoro's ~2s model load is paid a
// couple of times per answer, not per sentence.

import (
	"context"
	"strings"
	"sync"

	"github.com/avifenesh/eigen/internal/voice"
)

// speechQueue speaks pushed text serially through one TTS. Push appends,
// Close marks end-of-input (the queue drains then finishes), Stop cancels
// mid-sentence. done closes when the queue is fully drained or stopped.
type speechQueue struct {
	mu      sync.Mutex
	pending strings.Builder
	closed  bool
	kick    chan struct{}
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
}

func newSpeechQueue(parent context.Context, tts voice.TTS) *speechQueue {
	ctx, cancel := context.WithCancel(parent)
	q := &speechQueue{
		kick:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	go q.run(tts)
	return q
}

// Push appends text to be spoken. Safe from any goroutine.
func (q *speechQueue) Push(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	q.mu.Lock()
	q.pending.WriteString(text)
	q.mu.Unlock()
	q.wake()
}

// Close marks end-of-input: the queue speaks what remains, then finishes.
func (q *speechQueue) Close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.wake()
}

// Stop cancels speech immediately (mid-sentence); done closes promptly.
func (q *speechQueue) Stop() { q.cancel() }

func (q *speechQueue) wake() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

// take drains everything accumulated so far — the coalescing step.
func (q *speechQueue) take() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	t := q.pending.String()
	q.pending.Reset()
	return t, q.closed
}

func (q *speechQueue) run(tts voice.TTS) {
	defer close(q.done)
	defer q.cancel()
	for {
		text, closed := q.take()
		if text != "" {
			_ = tts.Speak(q.ctx, text)
			if q.ctx.Err() != nil {
				return // stopped mid-sentence
			}
			continue // more may have accumulated while speaking
		}
		if closed {
			return // drained
		}
		select {
		case <-q.kick:
		case <-q.ctx.Done():
			return
		}
	}
}

// speechStreaming: should streamed deltas feed speech right now? Voice mode
// and the read-aloud toggle both stream; the one-shot ▶ read can't (the
// answer already exists by the time it's clicked).
func (m *model) speechStreaming() bool {
	return (m.voiceOn || m.readAloud) && m.tts != nil && m.tts.Available()
}

// speechFeed accumulates streamed assistant text and pushes COMPLETE
// sentences to the queue — speech starts at the first sentence boundary,
// not at the end of the answer. The incomplete tail stays buffered until
// the next boundary (or flushSpeech at turn end).
func (m *model) speechFeed(delta string) {
	if m.state != stRunning || !m.speechStreaming() {
		return
	}
	if m.speech == nil {
		m.speech = newSpeechQueue(m.ctx, m.tts)
		m.voiceCancel = m.speech.cancel
		m.speechBuf = ""
	}
	m.speechBuf += delta
	if cut := lastSentenceEnd(m.speechBuf); cut > 0 {
		m.speech.Push(m.speechBuf[:cut])
		m.speechBuf = m.speechBuf[cut:]
	}
}

// flushSpeech hands the queue its tail and closes it (turn finished); the
// queue keeps speaking until drained. Returns the queue, nil when none.
func (m *model) flushSpeech() *speechQueue {
	q := m.speech
	if q == nil {
		return nil
	}
	m.speech = nil
	if strings.TrimSpace(m.speechBuf) != "" {
		q.Push(m.speechBuf)
	}
	m.speechBuf = ""
	q.Close()
	return q
}

// dropSpeech kills any in-flight streamed speech (interrupt/exit paths).
func (m *model) dropSpeech() {
	if m.speech != nil {
		m.speech.Stop()
		m.speech = nil
	}
	m.speechBuf = ""
}

// lastSentenceEnd returns the index just past the last COMPLETE sentence in
// s, or 0 when none. A sentence ends at .!?: followed by whitespace, or at a
// newline. Trailing punctuation without whitespace is NOT a boundary — more
// of the same sentence may still be streaming ("3." of "3.5").
func lastSentenceEnd(s string) int {
	for i := len(s) - 1; i > 0; i-- {
		c := s[i]
		if c == '\n' {
			return i + 1
		}
		if c == ' ' || c == '\t' {
			switch s[i-1] {
			case '.', '!', '?', ':':
				return i + 1
			}
		}
	}
	return 0
}
