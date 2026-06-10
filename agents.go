package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// agentsGuidance reads a repo's agent instructions file and renders it for the
// system prompt. It honors the AGENTS.md convention (and the common CLAUDE.md /
// .eigen/AGENTS.md variants), walking up from the working directory to the repo
// root so eigen picks up the nearest one like the other tools do. Returns ""
// when none is found. The file is project-authored guidance — distinct from
// learned memory — so it rides in ExtraSystem, not the memory section.
func agentsGuidance(cwd string) string {
	names := []string{"AGENTS.md", ".eigen/AGENTS.md", "CLAUDE.md"}
	seen := map[string]bool{}
	var blocks []string
	dir := cwd
	for {
		for _, n := range names {
			p := filepath.Join(dir, n)
			if seen[p] {
				continue
			}
			seen[p] = true
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			body := strings.TrimSpace(string(data))
			if body == "" {
				continue
			}
			// Cap a single file so an oversized AGENTS.md can't dominate the
			// prompt; it rides on every request.
			const maxAgentsBytes = 12 * 1024
			if len(body) > maxAgentsBytes {
				body = body[:maxAgentsBytes] + "\n…[truncated]"
			}
			rel, _ := filepath.Rel(cwd, p)
			if rel == "" {
				rel = n
			}
			blocks = append(blocks, fmt.Sprintf("# Repository guidance (%s)\n%s", rel, body))
		}
		parent := filepath.Dir(dir)
		if parent == dir || isRepoRoot(dir) {
			break
		}
		dir = parent
	}
	if len(blocks) == 0 {
		return ""
	}
	// Nearest-first already (we walked up from cwd); join with a separator.
	return "The project ships these instructions for agents — follow them:\n\n" + strings.Join(blocks, "\n\n")
}

// isRepoRoot reports whether dir contains a .git entry (stop the upward walk).
func isRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
