//go:build windows

package integration

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

func runCommand(parent context.Context, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, command, args...)
	process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := process.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), ctx.Err()
	}
	return string(output), err
}
