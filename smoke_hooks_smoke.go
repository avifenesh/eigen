//go:build smoke

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/app"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
	"github.com/avifenesh/eigen/internal/tui"
)

// runSmokeCommand is compiled only in tests. It keeps PTY smoke entrypoints out
// of the production binary while still exercising the real app/tui Program
// paths from subprocess-based tests.
func runTestSmokeCommand(arg string) bool {
	if arg == "app-smoke" && os.Getenv("EIGEN_APP_SMOKE") == "1" {
		res, err := app.RunAt(app.LoadEmpty(), app.PageHome)
		if err != nil {
			fail(err)
		}
		fmt.Printf("app-smoke action=%d\n", res.Action)
		return true
	}
	if arg == "tui-smoke" && os.Getenv("EIGEN_TUI_SMOKE") == "1" {
		reg, _ := tool.NewRegistry()
		be := chat.NewLocal(&agent.Agent{Provider: smokeProvider{}, Tools: reg, Perm: agent.PermAuto}, nil, "smoke")
		res, err := tui.Run(be, tui.Options{Provider: "smoke", Model: "smoke", NoSessionFile: true})
		if err != nil {
			fail(err)
		}
		fmt.Printf("tui-smoke openApp=%v switchTo=%s rebuild=%v\n", res.OpenApp, res.SwitchTo, res.Rebuild)
		return true
	}
	return false
}

type smokeProvider struct{}

func (smokeProvider) Name() string    { return "smoke" }
func (smokeProvider) ModelID() string { return "smoke" }
func (smokeProvider) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "smoke answer"}, nil
}
