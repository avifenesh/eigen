// Command eigen is a coding agent you own end to end.
//
// This entrypoint is currently a connectivity smoke test: it sends one prompt
// to the configured provider and prints the reply, proving the Go provider
// path works before the tool layer and agent loop are wired in.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/avifenesh/eigen/internal/llm"
)

func main() {
	provider, err := llm.NewMantle("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen: "+err.Error())
		os.Exit(1)
	}

	prompt := "Reply with exactly: eigen online"
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	resp, err := provider.Complete(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Text: prompt}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen: "+err.Error())
		os.Exit(1)
	}

	fmt.Printf("[%s]\n%s\n", provider.Name(), resp.Text)
}
