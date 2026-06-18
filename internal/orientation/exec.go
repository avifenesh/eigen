package orientation

import (
	"context"
	"os/exec"
)

func osExecOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
