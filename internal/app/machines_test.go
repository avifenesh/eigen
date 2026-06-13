package app

import (
	"strings"
	"testing"

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
	t.Logf("\n%s", out)
	for _, want := range []string{"machines", "2 known", "dev", "work", "ubuntu@ec2-1-2-3-4", "saved", "ssh-config", "openai.gpt-5.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("machines view missing %q", want)
		}
	}
	// enter on row 0 → ActionRemote with that host name
	m.machines.list.cursor = 0
	m.machines.update(m, key("enter"))
	if m.result.Action != ActionRemote || m.result.Host != "dev" {
		t.Errorf("enter → %v host=%q, want ActionRemote dev", m.result.Action, m.result.Host)
	}
}
