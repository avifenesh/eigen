package app

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/remote"
)

func TestMachinesPageRenders(t *testing.T) {
	d := &Data{Machines: []remote.Machine{
		{Name: "dev", SSH: "ubuntu@ec2-1-2-3-4", Detected: true},
		{Name: "work", SSH: "me@box", Dir: "/srv/app", Model: "openai.gpt-5.5", Saved: true, Detected: true},
	}}
	m := &Model{data: d, active: PageMachines, width: 100, height: 30}
	m.machines.init(d)
	out := m.machines.view(m, 96, 26)
	for _, want := range []string{"machines", "2 known", "dev", "work", "ubuntu@ec2-1-2-3-4", "saved", "ssh-config", "openai.gpt-5.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("machines view missing %q", want)
		}
	}
}

func TestMachinesEnterDrillsIn(t *testing.T) {
	d := &Data{Machines: []remote.Machine{{Name: "dev", SSH: "ubuntu@ec2-1-2-3-4", Detected: true}}}
	m := &Model{data: d, active: PageMachines, width: 100, height: 30}
	m.machines.init(d)
	m.machines.list.cursor = 0
	// enter drills in (does NOT quit) and kicks off the async fetch.
	_, cmd := m.machines.update(m, key("enter"))
	if !m.machines.inside || m.machines.mach != 0 {
		t.Fatalf("enter should drill into the machine, inside=%v mach=%d", m.machines.inside, m.machines.mach)
	}
	if !m.machines.loading || cmd == nil {
		t.Fatalf("drill-in should be loading + return a fetch cmd")
	}
	// Loading view.
	if !strings.Contains(m.machines.view(m, 96, 26), "listing sessions") {
		t.Error("loading view should say it's listing sessions")
	}
	// Deliver the remote session list.
	m2, _ := m.Update(machineSessionsMsg{mach: 0, sessions: []daemon.SessionInfo{
		{ID: "s1", Title: "fix the parser", Status: daemon.StatusIdle, Turns: 4, Updated: 1},
		{ID: "s2", Title: "", Status: daemon.StatusWorking, Turns: 1, Updated: 2},
	}})
	_ = m2
	if m.machines.loading || len(m.machines.sessions) != 2 {
		t.Fatalf("after msg: loading=%v n=%d", m.machines.loading, len(m.machines.sessions))
	}
	out := m.machines.view(m, 96, 26)
	for _, want := range []string{"‹ dev", "fix the parser", "(untitled)"} {
		if !strings.Contains(out, want) {
			t.Errorf("drill-in view missing %q", want)
		}
	}
	// enter on the first remote session → ActionRemote with its id.
	m.machines.inner.cursor = 0
	m.machines.update(m, key("enter"))
	if m.result.Action != ActionRemote || m.result.Host != "dev" || m.result.SessionID != "s1" {
		t.Errorf("enter on session → %v host=%q id=%q, want ActionRemote dev s1", m.result.Action, m.result.Host, m.result.SessionID)
	}
}

func TestMachinesNewSessionDirect(t *testing.T) {
	d := &Data{Machines: []remote.Machine{{Name: "dev", SSH: "ubuntu@x", Detected: true}}}
	m := &Model{data: d, active: PageMachines, width: 100, height: 30}
	m.machines.init(d)
	m.machines.list.cursor = 0
	// `n` opens a NEW session directly (no drill-in), id empty.
	m.machines.update(m, key("n"))
	if m.result.Action != ActionRemote || m.result.Host != "dev" || m.result.SessionID != "" {
		t.Errorf("n → %v host=%q id=%q, want ActionRemote dev (no id)", m.result.Action, m.result.Host, m.result.SessionID)
	}
}

func TestMachinesEscLeavesDrillIn(t *testing.T) {
	d := &Data{Machines: []remote.Machine{{Name: "dev", SSH: "ubuntu@x"}}}
	m := &Model{data: d, active: PageMachines, width: 100, height: 30}
	m.machines.init(d)
	m.machines.update(m, key("enter")) // drill in
	if !m.machines.inside {
		t.Fatal("should be inside")
	}
	m.machines.update(m, key("esc"))
	if m.machines.inside {
		t.Error("esc should leave the drill-in")
	}
}
