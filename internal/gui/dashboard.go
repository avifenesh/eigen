package gui

import (
	"context"
	"time"

	"github.com/avifenesh/eigen/internal/google"
	"github.com/avifenesh/eigen/internal/syshealth"
)

// Dashboard bridge — the working-station command center's data: today's
// calendar, unread mail, and machine health in ONE call so Home makes a single
// round-trip. Each section degrades independently (Google not linked → its
// fields stay empty + a flag; health always reads). Eigen is a working station,
// not a coding tool — Home should answer "what's my day + is my machine OK",
// not just "what sessions do I have".

// CalEventDTO / MailMsgDTO mirror the google summary structs for the frontend.
type CalEventDTO struct {
	Summary  string `json:"summary"`
	Start    string `json:"start"`
	AllDay   bool   `json:"allDay"`
	Location string `json:"location"`
}

type MailMsgDTO struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
}

// SysHealthDTO is the machine snapshot for the dashboard.
type SysHealthDTO struct {
	LoadPerCPU  float64 `json:"loadPerCpu"`
	CPUs        int     `json:"cpus"`
	MemUsedPct  float64 `json:"memUsedPct"`
	MemUsedGB   float64 `json:"memUsedGb"`
	MemTotalGB  float64 `json:"memTotalGb"`
	DiskUsedPct float64 `json:"diskUsedPct"`
	DiskUsedGB  float64 `json:"diskUsedGb"`
	DiskTotalGB float64 `json:"diskTotalGb"`
	// Swap (0 when none configured).
	SwapUsedPct float64 `json:"swapUsedPct"`
	SwapUsedGB  float64 `json:"swapUsedGb"`
	SwapTotalGB float64 `json:"swapTotalGb"`
	// CPU package temperature, °C (0 when unreadable).
	CPUTempC float64 `json:"cpuTempC"`
	// GPUs — per-accelerator util/mem/temp/power (training-rig signals).
	GPUs        []GPUDTO `json:"gpus,omitempty"`
	UptimeHours float64  `json:"uptimeHours"`
}

// GPUDTO is one GPU's stats for the dashboard.
type GPUDTO struct {
	Name       string  `json:"name"`
	UtilPct    float64 `json:"utilPct"`
	MemUsedGB  float64 `json:"memUsedGb"`
	MemTotalGB float64 `json:"memTotalGb"`
	MemUsedPct float64 `json:"memUsedPct"`
	TempC      float64 `json:"tempC"`
	PowerW     float64 `json:"powerW"`
}

// DashboardDTO is the full command-center snapshot.
type DashboardDTO struct {
	// Google section.
	GoogleConnected bool          `json:"googleConnected"`
	Events          []CalEventDTO `json:"events"`
	UnreadCount     int           `json:"unreadCount"`
	Unread          []MailMsgDTO  `json:"unread"`
	// Machine health (always present).
	Health SysHealthDTO `json:"health"`
}

// Dashboard returns the working-station command-center snapshot.
func (b *Bridge) Dashboard() (*DashboardDTO, error) {
	out := &DashboardDTO{Health: healthDTO(syshealth.Read())}

	g := google.Default()
	if g.Connected() {
		out.GoogleConnected = true
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		// Best-effort per section: a failure on one leaves it empty, doesn't sink
		// the whole dashboard.
		if evs, err := g.UpcomingEvents(ctx, 2, 8); err == nil {
			for _, e := range evs {
				out.Events = append(out.Events, CalEventDTO{Summary: e.Summary, Start: e.Start, AllDay: e.AllDay, Location: e.Location})
			}
		}
		if n, err := g.UnreadCount(ctx); err == nil {
			out.UnreadCount = n
		}
		if msgs, err := g.RecentUnread(ctx, 5); err == nil {
			for _, m := range msgs {
				out.Unread = append(out.Unread, MailMsgDTO{From: m.From, Subject: m.Subject})
			}
		}
	}
	return out, nil
}

const gb = 1 << 30

func healthDTO(h syshealth.Health) SysHealthDTO {
	round2 := func(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
	d := SysHealthDTO{
		LoadPerCPU:  round2(h.LoadPerCPU),
		CPUs:        h.CPUs,
		MemUsedPct:  round2(h.MemUsedPct),
		MemUsedGB:   round2(float64(h.MemUsed) / gb),
		MemTotalGB:  round2(float64(h.MemTotal) / gb),
		DiskUsedPct: round2(h.DiskUsedPct),
		DiskUsedGB:  round2(float64(h.DiskUsed) / gb),
		DiskTotalGB: round2(float64(h.DiskTotal) / gb),
		SwapUsedPct: round2(h.SwapUsedPct),
		SwapUsedGB:  round2(float64(h.SwapUsed) / gb),
		SwapTotalGB: round2(float64(h.SwapTotal) / gb),
		CPUTempC:    round2(h.CPUTempC),
		UptimeHours: round2(float64(h.UptimeSec) / 3600),
	}
	for _, g := range h.GPUs {
		d.GPUs = append(d.GPUs, GPUDTO{
			Name:       g.Name,
			UtilPct:    round2(g.UtilPct),
			MemUsedGB:  round2(float64(g.MemUsed) / gb),
			MemTotalGB: round2(float64(g.MemTotal) / gb),
			MemUsedPct: round2(g.MemUsedPct),
			TempC:      round2(g.TempC),
			PowerW:     round2(g.PowerW),
		})
	}
	return d
}
