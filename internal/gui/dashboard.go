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
	UptimeHours float64 `json:"uptimeHours"`
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
	return SysHealthDTO{
		LoadPerCPU:  round2(h.LoadPerCPU),
		CPUs:        h.CPUs,
		MemUsedPct:  round2(h.MemUsedPct),
		MemUsedGB:   round2(float64(h.MemUsed) / gb),
		MemTotalGB:  round2(float64(h.MemTotal) / gb),
		DiskUsedPct: round2(h.DiskUsedPct),
		DiskUsedGB:  round2(float64(h.DiskUsed) / gb),
		DiskTotalGB: round2(float64(h.DiskTotal) / gb),
		UptimeHours: round2(float64(h.UptimeSec) / 3600),
	}
}
