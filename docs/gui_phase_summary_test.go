package docs_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGUIPhaseSummaryIsMachineReadable(t *testing.T) {
	b, err := os.ReadFile("gui-phase-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Status                  string   `json:"status"`
		VerificationScript      string   `json:"verification_script"`
		FullRepoGate            string   `json:"full_repo_gate"`
		ReleasePTYSoakGate      string   `json:"release_pty_soak_gate"`
		GUIStaticCheck          string   `json:"gui_static_check"`
		GUIBrowserSmoke         string   `json:"gui_browser_smoke"`
		Implemented             []string `json:"implemented"`
		NotOwnedPreexisting     []string `json:"not_owned_preexisting_staged"`
		RemainingBeforeFullGoal []string `json:"remaining_before_full_goal"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.Status != "full_gui_parity_complete_for_shipped_eigen_surfaces" {
		t.Fatalf("status = %q", s.Status)
	}
	if s.VerificationScript != "scripts/verify-gui-phase.sh" {
		t.Fatalf("verification script = %q", s.VerificationScript)
	}
	if s.FullRepoGate != "go test ./... -count=1" {
		t.Fatalf("full repo gate = %q", s.FullRepoGate)
	}
	if s.ReleasePTYSoakGate != "go test . -run 'TestPTYReleaseAppShellLongerSoak' -count=1" {
		t.Fatalf("release PTY soak gate = %q", s.ReleasePTYSoakGate)
	}
	if s.GUIStaticCheck != "node --check internal/gui/static/app.js" {
		t.Fatalf("GUI static check = %q", s.GUIStaticCheck)
	}
	if s.GUIBrowserSmoke != "scripts/gui-smoke.sh" {
		t.Fatalf("GUI browser smoke = %q", s.GUIBrowserSmoke)
	}
	if len(s.Implemented) < 9 {
		t.Fatalf("implemented list too short: %v", s.Implemented)
	}
	if len(s.NotOwnedPreexisting) != 3 {
		t.Fatalf("pre-existing staged file list drifted: %v", s.NotOwnedPreexisting)
	}
	if len(s.RemainingBeforeFullGoal) != 0 {
		t.Fatalf("full goal should have no remaining blockers: %v", s.RemainingBeforeFullGoal)
	}
}
