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
	// Load credentials only from the trusted user config, never from a
	// project-local .env (an untrusted repo must not be able to set the
	// permission posture, provider creds, or model config).
	home, _ := os.UserHomeDir()
	if err := config.LoadEnvFiles(filepath.Join(home, ".eigen", ".env")); err != nil {
		fmt.Fprintln(os.Stderr, "eigen: env:", err)
	}

	model := flag.String("model", "", "model id (default: openai.gpt-5.5 on bedrock mantle)")
	provider := flag.String("provider", envOr("EIGEN_PROVIDER", "mantle"), "provider: mantle|llama")
	perm := flag.String("perm", envOr("EIGEN_PERMISSION", "gated"), "permission posture: gated|auto")
	flag.Parse()

	switch agent.Permission(*perm) {
	case agent.PermGated, agent.PermAuto:
	default:
		fail(fmt.Errorf("invalid --perm %q (want gated|auto)", *perm))
	}

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if task == "" {
		fmt.Fprintln(os.Stderr, `usage: eigen [--model ID] [--perm gated|auto] "task"`)
		os.Exit(2)
	}

	prov, err := llm.New(*provider, *model)
	if err != nil {
		fail(err)
	}

	policy := tool.DefaultPolicy()
	registry, err := tool.NewRegistry(
		tool.Read(policy),
		tool.List(policy),
		tool.Glob(policy),
		tool.Grep(policy),
		tool.Write(policy),
		tool.Edit(policy),
		tool.Bash(),
	)
	if err != nil {
		fail(err)
	}

	streamed := false
	a := &agent.Agent{
		Provider: prov,
		Tools:    registry,
		Perm:     agent.Permission(*perm),
		Approve:  cliApprove,
		OnChunk: func(c llm.StreamChunk) {
			// Stream the model's output live to stderr as a progress view; the
			// canonical final answer still prints to stdout after the loop.
			fmt.Fprint(os.Stderr, c.Text)
			if c.Kind == llm.ChunkText {
				streamed = true
			}
		},
		OnStep: func(step int, resp *llm.Response) {
			if len(resp.ToolCalls) == 0 {
				return
			}
			names := make([]string, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				names[i] = tc.Name
			}
			fmt.Fprintf(os.Stderr, "\n  step %d → %s\n", step+1, strings.Join(names, ", "))
		},
	}

	fmt.Fprintf(os.Stderr, "eigen · %s · perm=%s\n", prov.Name(), *perm)
	out, err := a.Run(context.Background(), task)
	if err != nil {
		fail(err)
	}
	if streamed {
		fmt.Fprintln(os.Stderr)
	}
	fmt.Println(out)
}

// cliApprove prompts for a gated mutating tool call. It reads from the
// controlling terminal (/dev/tty), not stdin, so piped input cannot auto-answer
// it, and fails closed when there is no terminal. Arguments are truncated and
// flattened so a tool's payload cannot spoof the prompt.
func cliApprove(name string, args json.RawMessage) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false // no terminal: fail closed
	}
	defer tty.Close()

	shown := strings.ReplaceAll(string(args), "\n", " ")
	if len(shown) > 200 {
		shown = shown[:200] + "…"
	}
	fmt.Fprintf(tty, "approve %s %s ? [y/N] ", name, shown)
	line, _ := bufio.NewReader(tty).ReadString('\n')
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
