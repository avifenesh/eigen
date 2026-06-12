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

func TestSidebarShowsVoiceButtons(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	band := ansi.Strip(m.transcriptBand())
	for _, want := range []string{"⏺ speak", "▶ read answer", "◉ voice"} {
		if !strings.Contains(band, want) {
			t.Fatalf("sidebar missing voice button %q:\n%s", want, band)
		}
	}
	// The buttons resolve to their actions via the shared row model.
	var got []actionID
	for _, r := range m.sidebarRows() {
		if r.kind == sbNav {
			got = append(got, r.action)
		}
	}
	for _, want := range []actionID{actDictate, actSpeakAnswer, actVoiceToggle} {
		found := false
		for _, a := range got {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("sidebar rows missing action %v", want)
		}
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
	m.Update(turnDoneMsg{})
	// Turn done in voice mode → spoke the answer (async goroutine — poll) and
	// armed a new listen.
	deadline := time.Now().Add(2 * time.Second)
	for tts.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tts.count() == 0 {
		t.Fatal("voice mode should speak the answer")
	}
	if m.voiceMic != voiceListening {
		t.Fatalf("voice mode should relisten after the turn, mic=%v", m.voiceMic)
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
