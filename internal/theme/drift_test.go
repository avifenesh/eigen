package theme_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoRawColorLiteralsOutsideTheme is the design-system drift guard: color
// must come from a theme role, never a raw lipgloss.Color("#…") / AdaptiveColor
// literal at a call site (the "roles, not hues" rule). Only internal/theme is
// allowed to define raw colors. If this fails, add a role to internal/theme and
// reference it instead.
func TestNoRawColorLiteralsOutsideTheme(t *testing.T) {
	// Walk up to the repo root (this test runs in internal/theme).
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// lipgloss.Color("…") or AdaptiveColor{ … } — the two ways to mint a color.
	rawColor := regexp.MustCompile(`lipgloss\.Color\(|lipgloss\.AdaptiveColor\{|[^.\w]AdaptiveColor\{`)
	var offenders []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "node_modules" || base == "target" || base == "vendor" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil // tests may fabricate colors
		}
		// internal/theme is the ONE place raw colors are allowed.
		if strings.Contains(filepath.ToSlash(path), "internal/theme/") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if rawColor.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("raw color literals outside internal/theme (use a theme role instead):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
