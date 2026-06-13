package remote

import "testing"

func TestParseHostSpec(t *testing.T) {
	cases := []struct {
		in              string
		user, host, dir string
		wantErr         bool
	}{
		{in: "host", host: "host"},
		{in: "user@host", user: "user", host: "host"},
		{in: "user@host:/srv/repo", user: "user", host: "host", dir: "/srv/repo"},
		{in: "host:~/work", host: "host", dir: "~/work"},
		{in: "deploy@1.2.3.4:./rel", user: "deploy", host: "1.2.3.4", dir: "./rel"},
		{in: "myalias", host: "myalias"},     // ~/.ssh/config alias, bare
		{in: "host:8022", host: "host:8022"}, // colon-not-a-path stays on host
		{in: "", wantErr: true},
		{in: "@host", wantErr: true},
	}
	for _, c := range cases {
		h, err := ParseHostSpec(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseHostSpec(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseHostSpec(%q): %v", c.in, err)
			continue
		}
		if h.User != c.user || h.Host != c.host || h.Dir != c.dir {
			t.Errorf("ParseHostSpec(%q) = %+v, want user=%q host=%q dir=%q", c.in, h, c.user, c.host, c.dir)
		}
	}
}

func TestTargetFromUname(t *testing.T) {
	ok := map[string]Target{
		"Linux x86_64":  {"linux", "amd64"},
		"Linux aarch64": {"linux", "arm64"},
		"Darwin arm64":  {"darwin", "arm64"},
		"Darwin x86_64": {"darwin", "amd64"},
		"FreeBSD amd64": {"freebsd", "amd64"},
	}
	for in, want := range ok {
		got, err := TargetFromUname(in)
		if err != nil || got != want {
			t.Errorf("TargetFromUname(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"Windows x86_64", "Linux mips", "garbage", "Linux"} {
		if _, err := TargetFromUname(bad); err == nil {
			t.Errorf("TargetFromUname(%q): want error", bad)
		}
	}
}

func TestPlanBootstrap(t *testing.T) {
	amd := Target{"linux", "amd64"}
	arm := Target{"linux", "arm64"}
	if a, err := PlanBootstrap(amd, amd, false); err != nil || a != CopyRunning {
		t.Errorf("same target should copy running binary, got %v %v", a, err)
	}
	if a, err := PlanBootstrap(amd, arm, true); err != nil || a != CrossCompile {
		t.Errorf("diff target + src should cross-compile, got %v %v", a, err)
	}
	if _, err := PlanBootstrap(amd, arm, false); err == nil {
		t.Error("diff target without src should error")
	}
}
