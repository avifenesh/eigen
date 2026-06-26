package gui

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/syshealth"
)

// GPU history + training-aware alerting. eigen is the user's training rig, so
// the Machine panel shows a recent util/temp trend per GPU (sparkline) and the
// bridge raises a desktop notification when a GPU runs sustained-hot or pegged.
// Sampled in the GUI process (the sparkline is a GUI concern); the always-on
// headless signal lives in the daemon's dream stationDigest.

const (
	gpuSampleEvery = 5 * time.Second
	gpuHistoryLen  = 60 // ~5 min of 5s samples
	// Alert thresholds: a GPU that stays this hot / pegged for alertStreak
	// consecutive samples fires ONE notification (re-armed when it cools).
	gpuTempAlertC = 87.0
	gpuUtilAlert  = 97.0
	alertStreak   = 3 // ~15s sustained before alerting (ignore brief spikes)
)

// gpuSample is one point in a GPU's history.
type gpuSample struct {
	UtilPct float64 `json:"utilPct"`
	TempC   float64 `json:"tempC"`
}

// gpuHist holds the rolling samples + alert state per GPU (keyed by index|name).
type gpuHist struct {
	mu       sync.Mutex
	samples  map[string][]gpuSample
	hotRun   map[string]int  // consecutive over-threshold samples
	alerting map[string]bool // currently in an alerted (hot) state
}

func newGPUHist() *gpuHist {
	return &gpuHist{
		samples:  map[string][]gpuSample{},
		hotRun:   map[string]int{},
		alerting: map[string]bool{},
	}
}

// gpuKey is the stable history key for a GPU (index + name handles multi-GPU).
func gpuKey(i int, name string) string { return fmt.Sprintf("%d|%s", i, name) }

// record appends a sample for each GPU and returns any alerts to fire (title,
// body) — when a GPU first crosses sustained-hot. Cooling re-arms the alert.
func (g *gpuHist) record(gpus []syshealth.GPU) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var alerts []string
	live := map[string]bool{}
	for i, gp := range gpus {
		k := gpuKey(i, gp.Name)
		live[k] = true
		s := append(g.samples[k], gpuSample{UtilPct: gp.UtilPct, TempC: gp.TempC})
		if len(s) > gpuHistoryLen {
			s = s[len(s)-gpuHistoryLen:]
		}
		g.samples[k] = s

		hot := gp.TempC >= gpuTempAlertC || gp.UtilPct >= gpuUtilAlert
		if hot {
			g.hotRun[k]++
			if g.hotRun[k] >= alertStreak && !g.alerting[k] {
				g.alerting[k] = true
				alerts = append(alerts, fmt.Sprintf("GPU hot: %s — %.0f°C, %.0f%% util", gp.Name, gp.TempC, gp.UtilPct))
			}
		} else {
			g.hotRun[k] = 0
			g.alerting[k] = false // cooled — re-arm
		}
	}
	// Drop history for GPUs that vanished (e.g. eGPU unplugged).
	for k := range g.samples {
		if !live[k] {
			delete(g.samples, k)
			delete(g.hotRun, k)
			delete(g.alerting, k)
		}
	}
	return alerts
}

// snapshot returns a copy of each GPU's samples (index-ordered) for the bridge.
func (g *gpuHist) snapshot() map[string][]gpuSample {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string][]gpuSample, len(g.samples))
	for k, s := range g.samples {
		cp := make([]gpuSample, len(s))
		copy(cp, s)
		out[k] = cp
	}
	return out
}

// GPUSampleDTO mirrors gpuSample for the frontend.
type GPUSampleDTO struct {
	UtilPct float64 `json:"utilPct"`
	TempC   float64 `json:"tempC"`
}

// GPUHistory returns recent util/temp samples per GPU (keyed "index|name"),
// oldest→newest, for the Machine-panel sparkline.
func (b *Bridge) GPUHistory() (map[string][]GPUSampleDTO, error) {
	if b.gpuHist == nil {
		return map[string][]GPUSampleDTO{}, nil
	}
	snap := b.gpuHist.snapshot()
	out := make(map[string][]GPUSampleDTO, len(snap))
	for k, s := range snap {
		dto := make([]GPUSampleDTO, len(s))
		for i, p := range s {
			dto[i] = GPUSampleDTO{UtilPct: p.UtilPct, TempC: p.TempC}
		}
		out[k] = dto
	}
	return out, nil
}

// gpuSampleLoop samples GPU stats on a ticker, records history, and fires the
// desktop notifier on sustained-hot. Started from ServiceStartup; stops on the
// shared stop channel.
func (b *Bridge) gpuSampleLoop(stop chan struct{}) {
	// Skip entirely when there's no GPU — no nvidia-smi, no work.
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return
	}
	if b.gpuHist == nil {
		b.gpuHist = newGPUHist()
	}
	t := time.NewTicker(gpuSampleEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			h := syshealth.Read()
			if len(h.GPUs) == 0 {
				continue
			}
			for _, alert := range b.gpuHist.record(h.GPUs) {
				notifyDesktop("eigen — training rig", alert)
			}
		}
	}
}

// notifyDesktop fires a best-effort desktop notification via notify-send.
func notifyDesktop(title, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	c := exec.Command("notify-send", "-u", "critical", title, body)
	if err := c.Start(); err == nil {
		go func() { _ = c.Wait() }()
	}
}
