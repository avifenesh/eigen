package tui

// Tier 15: three voice features, three affordances — dictate-once (answer is
// text), read-last-answer (one-shot speech), conversation mode (hands-free).

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeSTT returns scripted transcripts.
type fakeSTT struct {
	mu    sync.Mutex
	texts []string
	calls int
}

func (f *fakeSTT) Available() bool { return true }
func (f *fakeSTT) Listen(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.texts) == 0 {
		return "", nil
	}
	t := f.texts[0]
	f.texts = f.texts[1:]
	return t, nil
}

// fakeTTS records what was spoken.
type fakeTTS struct {
	mu     sync.Mutex
	spoken []string
}

func (f *fakeTTS) Available() bool { return true }
func (f *fakeTTS) Speak(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spoken = append(f.spoken, text)
	return nil
}
func (f *fakeTTS) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.spoken)
}

func TestComposerBarShowsVoiceButtons(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := ansi.Strip(m.View())
	for _, want := range []string{"⏺ speak", "▶ read", "◉ voice"} {
		if !strings.Contains(view, want) {
			t.Fatalf("composer bar missing voice button %q:\n%s", want, view)
		}
	}
	// Click each control via the shared layout: composer rect + segment math.
	l := m.computeLayout()
	if l.composer.empty() {
		t.Fatal("composer rect should exist")
	}
	// Find each segment's start column and resolve its action.
	for _, want := range []struct {
		text string
		act  actionID
	}{
		{"⏺ speak", actDictate},
		{"▶ read", actSpeakAnswer},
		{"◉ voice", actVoiceToggle},
	} {
		bar := ansi.Strip(m.composerBarView())
		col := strings.Index(bar, want.text)
		if col < 0 {
			t.Fatalf("segment %q not rendered", want.text)
		}
		// Index is a byte offset; convert to display col via prefix width.
		x := ansi.StringWidth(bar[:col])
		if got := m.composerActionAt(x); got != want.act {
			t.Fatalf("click on %q → action %v, want %v", want.text, got, want.act)
		}
	}
	// The hit-test routes composer clicks to the action.
	h := m.hitTest(2, l.composer.y)
	if h.region != regComposer {
		t.Fatalf("composer row should hit regComposer, got %v", h.region)
	}
}

func TestDictateOnceSubmitsWithoutSpeaking(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tts := &fakeTTS{}
	m.stt = &fakeSTT{texts: []string{"hello there"}}
	m.tts = tts

	cmd := m.dictateOnce()
	if cmd == nil {
		t.Fatal("dictateOnce should start a recording")
	}
	msg := cmd().(voiceSpokenMsg)
	if msg.conv {
		t.Fatal("dictation must not be a conversation leg")
	}
	_, cmd2 := m.handleSpoken(msg)
	if cmd2 == nil {
		t.Fatal("a transcript should submit a turn")
	}
	// Simulate the turn completing with a text answer.
	m.state = stInput
	m.text("assistant", "hi!")
	m.Update(turnDoneMsg{})
	if tts.count() != 0 {
		t.Fatalf("dictation answer must stay text; spoke %v", tts.spoken)
	}
}

func TestSpeakLastAnswerOneShot(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	sp := &fakeSpeaker{avail: true}
	m.speaker = sp
	m.text("assistant", "the answer is 42")
	m.speakLastAnswer()
	if len(sp.spoken) != 1 || !strings.Contains(sp.spoken[0], "42") {
		t.Fatalf("speakLastAnswer should speak the last answer once, got %v", sp.spoken)
	}
	if m.readAloud {
		t.Fatal("one-shot speak must not enable the persistent read-aloud toggle")
	}
	if m.voiceOn {
		t.Fatal("one-shot speak must not enable conversation mode")
	}
}

func TestVoiceModeSpeaksAndRelistens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	stt := &fakeSTT{texts: []string{"what time is it"}}
	tts := &fakeTTS{}
	m.stt, m.tts = stt, tts

	cmd := m.toggleVoice()
	if !m.voiceOn {
		t.Fatal("voice mode should turn on")
	}
	if cmd == nil {
		t.Fatal("voice mode should start listening immediately")
	}
	msg := cmd().(voiceSpokenMsg)
	if !msg.conv {
		t.Fatal("voice-mode recording must be a conversation leg")
	}
	_, submitCmd := m.handleSpoken(msg)
	if submitCmd == nil {
		t.Fatal("transcript should submit")
	}
	// Simulate the turn completing (the read-aloud tests' pattern — driving a
	// real provider turn is owned by submit tests).
	m.state = stInput
	m.text("assistant", "it is noon")
	_, doneCmd := m.Update(turnDoneMsg{})
	// SEQUENCED: first the reply is SPOKEN (mic shows speaking, not listening
	// — starting the mic immediately killed the speech: the "never reads
	// back" bug)…
	if m.voiceMic != voiceSpeaking {
		t.Fatalf("voice mode should speak first, mic=%v", m.voiceMic)
	}
	speech := drainForMsg[voiceSpeechDoneMsg](t, doneCmd)
	if tts.count() == 0 {
		t.Fatal("voice mode should speak the answer")
	}
	// …then speech-done returns to the mic.
	m.Update(speech)
	if m.voiceMic != voiceListening {
		t.Fatalf("after the reply is spoken the mic should listen again, mic=%v", m.voiceMic)
	}
}

// drainForMsg runs a (possibly batched) command tree until a message of type T
// surfaces, failing the test if none does. Batched commands run CONCURRENTLY,
// matching bubbletea's runtime semantics — a batch may pair a blocking command
// with the one that unblocks it (speak + interrupt monitor).
func drainForMsg[T tea.Msg](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	msgs := make(chan tea.Msg, 32)
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, bc := range batch {
				go run(bc)
			}
			return
		}
		msgs <- msg
	}
	go run(cmd)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if got, ok := msg.(T); ok {
				return got
			}
		case <-deadline:
			var zero T
			t.Fatalf("no %T surfaced from the command tree", zero)
			return zero
		}
	}
}

func TestVoiceModeExitDiscardsStaleRecording(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{texts: []string{"late transcript"}}
	m.tts = &fakeTTS{}

	cmd := m.toggleVoice()
	if cmd == nil {
		t.Fatal("expected listen cmd")
	}
	staleGen := m.voiceGen
	m.exitVoiceMode("off") // user exits before the recording lands
	msg := voiceSpokenMsg{text: "late transcript", conv: true, gen: staleGen}
	_, submitCmd := m.handleSpoken(msg)
	if submitCmd != nil {
		t.Fatal("a stale recording must be discarded after exit (epoch guard)")
	}
	if m.voiceOn {
		t.Fatal("mode should stay off")
	}
}

func TestVoiceModeEmptyHearingRelistens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{}
	m.tts = &fakeTTS{}
	cmd := m.toggleVoice()
	if cmd == nil {
		t.Fatal("expected listen cmd")
	}
	msg := cmd().(voiceSpokenMsg) // fakeSTT returns "" (nothing heard)
	_, again := m.handleSpoken(msg)
	if again == nil {
		t.Fatal("voice mode should keep listening after silence")
	}
	if m.voiceMic != voiceListening {
		t.Fatal("mic should be listening again")
	}
}

// (turn driving is owned by the submit tests; voice tests simulate turn ends
// with m.text("assistant", …) + Update(turnDoneMsg{}) like the read-aloud
// tests.)

func TestDictateClickAgainStopsListening(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{texts: []string{"stopped speech"}}
	m.tts = &fakeTTS{}
	cmd := m.dictateOnce()
	if cmd == nil || m.voiceMic != voiceListening {
		t.Fatal("first click should start listening")
	}
	gen := m.voiceGen
	// Second click: stop (NOT a new recording, NOT a no-op).
	if c := m.dictateOnce(); c != nil {
		t.Fatal("second click must stop, not start another recording")
	}
	if m.voiceMic != voiceTranscribing {
		t.Fatalf("after stop the mic should be transcribing, got %v", m.voiceMic)
	}
	if m.voiceStop != nil {
		t.Fatal("stop must cancel the recording context")
	}
	// The in-flight transcript is NOT stale (same gen): it still submits.
	_, submit := m.handleSpoken(voiceSpokenMsg{text: "stopped speech", gen: gen})
	if submit == nil {
		t.Fatal("stop means 'done talking' — the transcript must still submit")
	}
}

func TestEscDiscardsDictation(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{texts: []string{"never mind"}}
	cmd := m.dictateOnce()
	if cmd == nil {
		t.Fatal("expected recording")
	}
	gen := m.voiceGen
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.voiceMic != voiceIdle {
		t.Fatalf("esc should reset the mic, got %v", m.voiceMic)
	}
	// The in-flight transcript is stale (gen bumped): discarded.
	_, submit := m.handleSpoken(voiceSpokenMsg{text: "never mind", gen: gen})
	if submit != nil {
		t.Fatal("esc means discard — the transcript must NOT submit")
	}
}

func TestComposerShowsStopWhileListening(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{}
	m.dictateOnce()
	bar := ansi.Strip(m.composerBarView())
	if !strings.Contains(bar, "stop") {
		t.Fatalf("bar should show a stop affordance while listening: %q", bar)
	}
}

func TestVoiceClickWhileSpeakingSkipsToListening(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{}
	m.tts = &fakeTTS{}
	m.toggleVoice()
	m.voiceMic = voiceSpeaking // reply being read aloud
	gen := m.voiceGen
	// Click ◉ while speaking: skip the speech, stay IN voice mode.
	m.toggleVoice()
	if !m.voiceOn {
		t.Fatal("click during speech must not exit voice mode")
	}
	// Speech-done (same gen) then returns to the mic.
	_, lc := m.Update(voiceSpeechDoneMsg{gen: gen})
	if lc == nil || m.voiceMic != voiceListening {
		t.Fatalf("after skipped speech the mic should listen, mic=%v", m.voiceMic)
	}
	// Esc while listening exits for real.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.voiceOn {
		t.Fatal("esc should exit voice mode")
	}
}

// monSTT is a fakeSTT that also implements voice.InterruptMonitor: it reports
// "the user spoke over the reply" immediately.
type monSTT struct {
	fakeSTT
	fired chan struct{}
}

func (f *monSTT) MonitorInterrupt(ctx context.Context) bool {
	defer close(f.fired)
	return true
}

// blockingTTS speaks until its context is canceled (a long reply mid-read).
type blockingTTS struct{}

func (blockingTTS) Available() bool { return true }
func (blockingTTS) Speak(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestVoiceInterruptOnSpeechCutsReplyAndRelistens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	stt := &monSTT{fired: make(chan struct{})}
	m.stt = stt
	m.tts = blockingTTS{}
	m.voiceOn = true
	m.state = stInput
	m.text("assistant", "a very long answer being read aloud")
	_, cmd := m.Update(turnDoneMsg{})
	if m.voiceMic != voiceSpeaking {
		t.Fatalf("reply should be speaking, mic=%v", m.voiceMic)
	}
	// The batch holds [speak, monitor]; the monitor fires → cancel → the
	// blocked Speak returns → voiceSpeechDoneMsg surfaces from the SAME tree.
	done := drainForMsg[voiceSpeechDoneMsg](t, cmd)
	select {
	case <-stt.fired:
	default:
		t.Fatal("interrupt monitor should have run")
	}
	m.Update(done)
	if m.voiceMic != voiceListening {
		t.Fatalf("after the interrupt the mic should listen, mic=%v", m.voiceMic)
	}
	if !m.voiceOn {
		t.Fatal("interrupt must not exit voice mode")
	}
}

func TestMuteParksConversationLoop(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{}
	m.tts = &fakeTTS{}
	m.toggleVoice()
	if m.voiceMic != voiceListening {
		t.Fatal("voice mode should be listening")
	}
	// Mute mid-listen: recording discarded, mic parked, mode stays ON.
	m.toggleMute()
	if !m.voiceMuted || m.voiceMic != voiceIdle || !m.voiceOn {
		t.Fatalf("mute should park the mic in-mode: muted=%v mic=%v on=%v", m.voiceMuted, m.voiceMic, m.voiceOn)
	}
	// Replies still speak while muted: turn done → speaking leg runs…
	m.state = stInput
	m.text("assistant", "spoken while muted")
	_, cmd := m.Update(turnDoneMsg{})
	if m.voiceMic != voiceSpeaking {
		t.Fatalf("muted conversation should still SPEAK replies, mic=%v", m.voiceMic)
	}
	done := drainForMsg[voiceSpeechDoneMsg](t, cmd)
	// …but speech-done does NOT reopen the mic while muted.
	m.Update(done)
	if m.voiceMic != voiceIdle {
		t.Fatalf("muted loop must park after speaking, mic=%v", m.voiceMic)
	}
	// Unmute resumes listening immediately.
	cmd = m.toggleMute()
	if m.voiceMuted || cmd == nil || m.voiceMic != voiceListening {
		t.Fatalf("unmute should resume listening: muted=%v mic=%v", m.voiceMuted, m.voiceMic)
	}
}

func TestMuteOutsideVoiceModeIsHint(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd := m.toggleMute(); cmd != nil || m.voiceMuted {
		t.Fatal("mute outside conversation mode should only hint")
	}
}

func TestExitVoiceModeClearsMute(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.stt = &fakeSTT{}
	m.tts = &fakeTTS{}
	m.toggleVoice()
	m.toggleMute()
	m.exitVoiceMode("off")
	if m.voiceMuted {
		t.Fatal("exiting voice mode must clear mute (next session starts live)")
	}
}

func TestComposerShowsMuteInVoiceMode(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	bar := ansi.Strip(m.composerBarView())
	if strings.Contains(bar, "mute") {
		t.Fatal("mute segment should not render outside voice mode")
	}
	m.stt = &fakeSTT{}
	m.tts = &fakeTTS{}
	m.toggleVoice()
	bar = ansi.Strip(m.composerBarView())
	if !strings.Contains(bar, "⊘ mute") {
		t.Fatalf("voice mode should show the mute button: %q", bar)
	}
	// Click maps to the action via the shared column math.
	col := strings.Index(bar, "⊘ mute")
	x := ansi.StringWidth(bar[:col])
	if got := m.composerActionAt(x); got != actVoiceMute {
		t.Fatalf("click on mute → %v", got)
	}
	m.toggleMute()
	bar = ansi.Strip(m.composerBarView())
	if !strings.Contains(bar, "⊘ muted") {
		t.Fatalf("muted state should render: %q", bar)
	}
}
