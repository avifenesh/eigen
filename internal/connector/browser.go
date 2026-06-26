package connector

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser opens url in the user's default browser. Used to start the OAuth
// authorization flow. On a headless host the spawn may fail; the caller surfaces
// the URL so the user can open it manually.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, *bsd
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	// Reap the launcher process so it doesn't linger as a zombie.
	go func() { _ = cmd.Wait() }()
	return nil
}
