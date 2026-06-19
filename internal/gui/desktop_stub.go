//go:build !wails || (!dev && !production)

package gui

import (
	"context"
	"fmt"
)

// DesktopAvailable reports whether this binary was built with the Wails desktop
// shell. Normal Eigen builds keep working without GTK/WebKit development
// packages; desktop builds use the Wails CLI, which supplies wails + dev or
// production tags.
const DesktopAvailable = false

func RunDesktop(context.Context, *Service) error {
	return fmt.Errorf("Wails desktop shell is not compiled into this binary (use the Wails CLI, or run `eigen gui --browser`)")
}
