package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// TestLiveConfigSwitchRace exercises the live-switch path the TUI uses
// (/model, ctrl+a, failover) while a turn is running. Run with -race: before
// Agent.mu + SetLive/SetPerm, this was a real data race between the UI and
// agent goroutines.

// slowProvider returns empty turns so the loop keeps reading config while the
// other goroutine swaps it.
type slowProvider struct{ n int }

func (p *slowProvider) Name() string { return "slow" }
func (p *slowProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.n++
	if p.n > 3 {
		return &llm.Response{Text: "done"}, nil
	}
	return &llm.Response{Text: ""}, nil // empty turn → loop continues
}

func TestLiveConfigSwitchRace(t *testing.T) {
	a := &Agent{Provider: &slowProvider{}, Tools: mustReg(t), Perm: PermAuto}
	s := a.NewSession()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = s.Send(context.Background(), "hi") }()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			a.SetLive(&slowProvider{}, nil, 0) // what /model does mid-turn
			a.SetPerm(PermGated)               // what ctrl+a does mid-turn
		}
	}()
	wg.Wait()
}
