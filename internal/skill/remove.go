package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Remove deletes an installed skill by name from dir (typically ~/.eigen/skills).
// It removes the skill directory (name/SKILL.md and siblings). Proposed skills
// under ~/.eigen/skills/proposed are not affected — use Reject for those.
func Remove(dir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid skill name %q", name)
	}
	sd := filepath.Join(dir, name)
	info, err := os.Stat(sd)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill %q is not installed at %s", name, dir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a skill directory", sd)
	}
	skillMD := filepath.Join(sd, "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		return fmt.Errorf("skill %q has no SKILL.md at %s", name, sd)
	}
	return os.RemoveAll(sd)
}
