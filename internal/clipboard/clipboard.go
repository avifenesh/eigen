// Package clipboard provides best-effort copy-to-clipboard for eigen by piping
// text to an external command's stdin (EIGEN_CLIPBOARD_CMD, or wl-copy / xclip /
// xsel / pbcopy). A missing command is simply unavailable, never an error.
package clipboard

import (
	"os"
	"os/exec"
	"strings"
)

// Copier copies text to the system clipboard via an external command.
type Copier struct {
	argv []string
}

// Detect resolves a clipboard command: EIGEN_CLIPBOARD_CMD (run via `sh -c`)
// wins; otherwise the first available of wl-copy, xclip, xsel, pbcopy.
func Detect() *Copier {
	if c := strings.TrimSpace(os.Getenv("EIGEN_CLIPBOARD_CMD")); c != "" {
		return &Copier{argv: []string{"sh", "-c", c}}
	}
	candidates := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	}
	for _, argv := range candidates {
		if p, err := exec.LookPath(argv[0]); err == nil {
			argv[0] = p
			return &Copier{argv: argv}
		}
	}
	return &Copier{}
}

// Available reports whether a clipboard command was resolved.
func (c *Copier) Available() bool { return c != nil && len(c.argv) > 0 }

// Copy writes text to the clipboard. It is a no-op (nil error) when unavailable.
func (c *Copier) Copy(text string) error {
	if !c.Available() {
		return nil
	}
	cmd := exec.Command(c.argv[0], c.argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
