package voice

import (
	"strings"
	"testing"
)

func TestDiagnoseReportsAllComponents(t *testing.T) {
	checks := Diagnose()
	want := map[string]bool{
		"recorder": false, "whisper": false, "whisper model": false,
		"tts": false, "playback": false,
	}
	for _, c := range checks {
		want[c.Name] = true
		if !c.OK && c.Fix == "" {
			t.Errorf("failed check %q should offer a fix", c.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("Diagnose() missing a %q check", name)
		}
	}
}

func TestReportRendersGlyphs(t *testing.T) {
	r := Report()
	if !strings.ContainsAny(r, "✓✗") {
		t.Fatal("report should mark each component with ✓/✗")
	}
}

func TestDoctorTTSOverrideReported(t *testing.T) {
	t.Setenv("EIGEN_TTS_CMD", "mytts --flag")
	checks := diagnoseTTS()
	if len(checks) != 1 || !checks[0].OK || !strings.Contains(checks[0].Detail, "mytts") {
		t.Fatalf("EIGEN_TTS_CMD override should be reported as the tts backend, got %+v", checks)
	}
}
