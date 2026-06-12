package voice

// VAD-endpointed recording: stream PCM from the recorder and stop on trailing
// quiet, instead of a fixed-length window. The endpointing semantics are
// ported from the user's codex-desktop-linux conversation-mode patch.js:
// speech "starts" after ~220ms above the threshold, the utterance ends after
// ~1.8s of trailing quiet, and a softer "possible speech" threshold extends
// the tail so low-energy words aren't mistaken for silence.

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"time"
)

// vadParams tunes the endpointer. Defaults mirror patch.js speechSettings().
type vadParams struct {
	threshold float64       // RMS level that counts as voiced (0..1)
	possible  float64       // softer continuation threshold
	speechMin time.Duration // sustained voice before speech "started"
	quiet     time.Duration // trailing quiet that ends the utterance
	maxTotal  time.Duration // hard cap on the whole recording
	maxWait   time.Duration // give up if speech never starts
}

func defaultVAD() vadParams {
	return vadParams{
		threshold: envFloat("EIGEN_VOICE_VAD_THRESHOLD", 0.01),
		possible:  0, // derived below
		speechMin: 220 * time.Millisecond,
		quiet:     envDuration("EIGEN_VOICE_SILENCE_MS", 1800*time.Millisecond),
		maxTotal:  90 * time.Second,
		maxWait:   10 * time.Second,
	}
}

// recordVAD runs recorder argv (raw S16_LE mono 16kHz PCM on stdout), watches
// RMS levels, and returns the captured PCM when the utterance ends. Returns
// nil PCM when no speech was detected before maxWait. The recorder process is
// always terminated on return.
func recordVAD(ctx context.Context, argv []string, p vadParams) ([]byte, error) {
	if p.possible <= 0 {
		p.possible = math.Max(0.002, p.threshold*0.45)
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(rctx, argv[0], argv[1:]...)
	cmd.Stderr = nil
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer cmd.Wait() // reap after cancel kills it

	const sampleRate = 16000
	const frameMs = 30
	frameBytes := sampleRate * 2 * frameMs / 1000 // S16_LE mono

	var pcm []byte
	frame := make([]byte, frameBytes)
	start := time.Now()
	var voicedSince, lastSpeech time.Time
	sawSpeech := false

	for {
		if ctx.Err() != nil {
			// Caller canceled (stop button / mode exit): keep what we heard.
			if sawSpeech {
				return pcm, nil
			}
			return nil, ctx.Err()
		}
		if _, err := io.ReadFull(out, frame); err != nil {
			// Recorder died/EOF: return what was captured.
			if sawSpeech {
				return pcm, nil
			}
			return nil, nil
		}
		pcm = append(pcm, frame...)
		now := time.Now()
		level := rmsLevel(frame)
		voiced := level > p.threshold
		switch {
		case voiced:
			if voicedSince.IsZero() {
				voicedSince = now
			}
			if now.Sub(voicedSince) >= p.speechMin {
				sawSpeech = true
				lastSpeech = now
			}
		case sawSpeech && level > p.possible:
			// Soft continuation: quiet-ish but plausibly still speech.
			lastSpeech = now
			voicedSince = time.Time{}
		default:
			voicedSince = time.Time{}
		}
		if sawSpeech && now.Sub(lastSpeech) >= p.quiet {
			return pcm, nil // trailing quiet → utterance over
		}
		if !sawSpeech && now.Sub(start) >= p.maxWait {
			return nil, nil // nobody spoke
		}
		if now.Sub(start) >= p.maxTotal {
			return pcm, nil
		}
	}
}

// rmsLevel computes the normalized RMS (0..1) of S16_LE PCM.
func rmsLevel(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		v := float64(s) / 32768.0
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

// writeWAV wraps raw S16_LE mono 16kHz PCM in a minimal WAV header.
func writeWAV(path string, pcm []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [44]byte
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(pcm)))
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], 1) // mono
	binary.LittleEndian.PutUint32(hdr[24:], 16000)
	binary.LittleEndian.PutUint32(hdr[28:], 16000*2)
	binary.LittleEndian.PutUint16(hdr[32:], 2)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(pcm)))
	if _, err := f.Write(hdr[:]); err != nil {
		return err
	}
	_, err = f.Write(pcm)
	return err
}

func envFloat(name string, def float64) float64 {
	if v := os.Getenv(name); v != "" {
		var f float64
		if _, err := fmt.Sscan(v, &f); err == nil && f > 0 {
			return f
		}
	}
	return def
}

func envDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		var ms int
		if _, err := fmt.Sscan(v, &ms); err == nil && ms >= 300 && ms <= 10000 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return def
}
