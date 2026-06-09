// Package clipboard provides best-effort copy/paste to the system clipboard for
// eigen by piping text through an external command (EIGEN_CLIPBOARD_CMD, or
// wl-copy/wl-paste, xclip, xsel, pbcopy/pbpaste). A missing command is simply
// unavailable, never an error.
package clipboard

import (
	"os"
	"os/exec"
	"strings"
)

// Copier copies/pastes text via external commands.
type Copier struct {
	argv  []string // copy (write to clipboard via stdin)
	paste []string // paste (read clipboard to stdout); may be nil
}

// Detect resolves clipboard commands: EIGEN_CLIPBOARD_CMD (run via `sh -c`)
// wins for copy (with EIGEN_CLIPBOARD_PASTE_CMD for paste if set); otherwise
// the first available of wl-copy/wl-paste, xclip, xsel, pbcopy/pbpaste.
func Detect() *Copier {
	if c := strings.TrimSpace(os.Getenv("EIGEN_CLIPBOARD_CMD")); c != "" {
		cp := &Copier{argv: []string{"sh", "-c", c}}
		if pst := strings.TrimSpace(os.Getenv("EIGEN_CLIPBOARD_PASTE_CMD")); pst != "" {
			cp.paste = []string{"sh", "-c", pst}
		}
		return cp
	}
	// Each candidate: {copy-argv..., "|", paste-argv...} encoded as two slices.
	candidates := []struct{ copy, paste []string }{
		{[]string{"wl-copy"}, []string{"wl-paste", "--no-newline"}},
		{[]string{"xclip", "-selection", "clipboard"}, []string{"xclip", "-selection", "clipboard", "-o"}},
		{[]string{"xsel", "--clipboard", "--input"}, []string{"xsel", "--clipboard", "--output"}},
		{[]string{"pbcopy"}, []string{"pbpaste"}},
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c.copy[0]); err == nil {
			c.copy[0] = p
			cp := &Copier{argv: c.copy}
			if pp, perr := exec.LookPath(c.paste[0]); perr == nil {
				c.paste[0] = pp
				cp.paste = c.paste
			}
			return cp
		}
	}
	return &Copier{}
}

// Available reports whether a clipboard (copy) command was resolved.
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

// CanPaste reports whether a paste command was resolved.
func (c *Copier) CanPaste() bool { return c != nil && len(c.paste) > 0 }

// Paste reads the clipboard contents (empty string when unavailable).
func (c *Copier) Paste() (string, error) {
	if !c.CanPaste() {
		return "", nil
	}
	out, err := exec.Command(c.paste[0], c.paste[1:]...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
