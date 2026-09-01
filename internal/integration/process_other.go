//go:build !windows

package integration

import (
	"context"
	"os/exec"
	"time"
)

func runCommand(parent context.Context, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, command, args...)
	output, err := combinedOutputLimited(process)
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}

func directCommandPath(command string) string { return command }
