package gui

import (
	"github.com/avifenesh/eigen/internal/remote"
)

// Remote bridge layer. Surfaces the machines eigen can reach over ssh — saved
// hosts (~/.eigen/remote hosts.json) + detected ~/.ssh/config aliases — and
// (on demand) the sessions running on a remote daemon. Listing machines is
// instant + local; RemoteSessions dials over ssh, so the GUI calls it only on
// drill-in. Install is intentionally NOT exposed (it needs interactive ssh /
// credential push — done via `eigen remote install`).

// MachineDTO mirrors remote.Machine for the machines board.
type MachineDTO struct {
	Name     string `json:"name"`
	SSH      string `json:"ssh"`
	Addr     string `json:"addr,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Model    string `json:"model,omitempty"`
	Perm     string `json:"perm,omitempty"`
	Saved    bool   `json:"saved"`
	Detected bool   `json:"detected"`
}

// MachinesDTO is the remote-targets snapshot.
type MachinesDTO struct {
	Machines []MachineDTO `json:"machines"`
}

// Machines returns saved + ssh-config-detected remote targets (instant, local).
func (b *Bridge) Machines() (*MachinesDTO, error) {
	ms := remote.Machines()
	out := make([]MachineDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, MachineDTO{
			Name: m.Name, SSH: m.SSH, Addr: m.Addr, Dir: m.Dir,
			Model: m.Model, Perm: m.Perm, Saved: m.Saved, Detected: m.Detected,
		})
	}
	return &MachinesDTO{Machines: out}, nil
}

// RemoteSessions lists the sessions on a remote eigen daemon (dials over ssh —
// slow; called on drill-in only). Errors when the host is unreachable or has no
// eigen daemon running.
func (b *Bridge) RemoteSessions(target string) ([]SessionInfoDTO, error) {
	infos, err := remote.ListSessions(target)
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfoDTO, 0, len(infos))
	for _, in := range infos {
		out = append(out, toSessionInfoDTO(in))
	}
	return out, nil
}
