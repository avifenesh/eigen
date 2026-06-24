// Voice store. eigen's voice stack is server-side (the GUI runs on the host, so
// it drives the same recorder/whisper STT + Kokoro/espeak TTS the TUI uses, not
// webview getUserMedia). This store holds capability flags (probed once) plus
// the live mic/speaker phase streamed on the eigen:voice event, and exposes the
// three actions: dictate (one utterance → transcript), read-aloud, and the
// hands-free conversation mode. See internal/gui/voice.go.
import { on, ev } from "$lib/events";
import { Bridge } from "$lib/bridge";
import type { VoiceStatusDTO, VoiceEventDTO } from "$lib/types";

// Phase mirrors the Go side: idle | listening | transcribing | thinking |
// speaking | error | off. "off" means voice mode ended.
export type VoicePhase = "idle" | "listening" | "transcribing" | "thinking" | "speaking" | "error" | "off";

function createVoice() {
  let stt = $state(false);
  let tts = $state(false);
  let probed = $state(false);
  // live state from the eigen:voice stream
  let phase = $state<VoicePhase>("idle");
  let lastText = $state(""); // latest transcript or error message
  let modeOn = $state(false); // hands-free conversation loop active

  function applyEvent(e: VoiceEventDTO | null) {
    if (!e) return;
    phase = (e.phase as VoicePhase) || "idle";
    if (e.text) lastText = e.text;
    // mode flips off on the explicit "off" phase; otherwise tracks the flag the
    // loop stamps on each event.
    if (phase === "off") modeOn = false;
    else if (e.mode) modeOn = true;
  }

  // start: probe capabilities once, then ride the eigen:voice push stream.
  // Returns a teardown that removes the listener.
  function start(): () => void {
    if (!probed) {
      Bridge.VoiceStatus()
        .then((s: VoiceStatusDTO | null) => {
          stt = !!s?.stt;
          tts = !!s?.tts;
          probed = true;
        })
        .catch(() => {
          probed = true; // leave both false → affordances stay hidden
        });
    }
    return on<VoiceEventDTO>(ev.voice, applyEvent);
  }

  // dictate: record ONE utterance and return its transcript (empty if nothing
  // heard). The composer appends it to the input. Pressing again cancels.
  async function dictate(): Promise<string> {
    if (!stt) return "";
    try {
      return await Bridge.VoiceListen();
    } catch {
      return "";
    }
  }
  async function cancelDictate() {
    try {
      await Bridge.VoiceCancelListen();
    } catch {
      /* idle is fine */
    }
  }

  // read aloud a given text once (cancelable via stopSpeak).
  async function speak(text: string) {
    if (!tts || !text.trim()) return;
    try {
      await Bridge.VoiceSpeak(text);
    } catch {
      /* unavailable → no-op */
    }
  }
  async function stopSpeak() {
    try {
      await Bridge.VoiceStopSpeak();
    } catch {
      /* not speaking is fine */
    }
  }

  // hands-free conversation: listen → submit → speak the reply → listen again.
  // Toggled per session. Needs both STT and TTS.
  async function toggleMode(sessionID: string) {
    if (!stt || !tts || !sessionID) return;
    try {
      if (modeOn) {
        await Bridge.VoiceModeStop();
        modeOn = false;
      } else {
        await Bridge.VoiceModeStart(sessionID);
        modeOn = true; // confirmed by the listening event that follows
      }
    } catch {
      modeOn = false;
    }
  }
  async function stopMode() {
    try {
      await Bridge.VoiceModeStop();
    } catch {
      /* not running is fine */
    }
    modeOn = false;
  }

  return {
    get stt() {
      return stt;
    },
    get tts() {
      return tts;
    },
    // any voice affordance is worth showing only when something is available
    get available() {
      return stt || tts;
    },
    get phase() {
      return phase;
    },
    get lastText() {
      return lastText;
    },
    get modeOn() {
      return modeOn;
    },
    // true while the mic is actively capturing (one-shot or in-loop)
    get listening() {
      return phase === "listening" || phase === "transcribing";
    },
    get speaking() {
      return phase === "speaking";
    },
    start,
    dictate,
    cancelDictate,
    speak,
    stopSpeak,
    toggleMode,
    stopMode,
  };
}

export const voice = createVoice();
