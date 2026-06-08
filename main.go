// Command eigen is a coding agent you own end to end.
//
// Usage:
//
//	eigen [--model ID] [--perm gated|auto] "task"
//
// It drives the configured model through a tool-use loop. Today it ships the
// read tool; write/edit/bash/grep/glob follow.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

func main() {
	home, _ := os.UserHomeDir()
	config.LoadEnvFiles(".env", filepath.Join(home, ".eigen", ".env"))

	model := flag.String("model", "", "model id (default: openai.gpt-5.5 on bedrock mantle)")
	perm := flag.String("perm", envOr("EIGEN_PERMISSION", "gated"), "permission posture: gated|auto")
	flag.Parse()

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if task == "" {
		fmt.Fprintln(os.Stderr, `usage: eigen [--model ID] [--perm gated|auto] "task"`)
		os.Exit(2)
	}

	provider, err := llm.NewMantle(*model)
	if err != nil {
		fail(err)
	}

	a := &agent.Agent{
		Provider: provider,
		Tools:    tool.NewRegistry(tool.Read(tool.DefaultPolicy())),
		Perm:     agent.Permission(*perm),
		Approve:  cliApprove,
		OnStep: func(step int, resp *llm.Response) {
			if len(resp.ToolCalls) == 0 {
				return
			}
			names := make([]string, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				names[i] = tc.Name
			}
			fmt.Fprintf(os.Stderr, "  step %d → %s\n", step+1, strings.Join(names, ", "))
		},
	}

	fmt.Fprintf(os.Stderr, "eigen · %s · perm=%s\n", provider.Name(), *perm)
	out, err := a.Run(context.Background(), task)
	if err != nil {
		fail(err)
	}
	fmt.Println(out)
}

// cliApprove prompts on stderr/stdin for gated mutating tool calls.
func cliApprove(name string, args json.RawMessage) bool {
	fmt.Fprintf(os.Stderr, "approve %s %s ? [y/N] ", name, string(args))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "eigen: "+err.Error())
	os.Exit(1)
}
