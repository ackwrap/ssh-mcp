//go:build windows

package integration

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const volumeNameDOS = 0

func runCommand(parent context.Context, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, resolveScoopShim(command), args...)
	process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := combinedOutputLimited(process)
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	return output, err
}

func directCommandPath(command string) string { return resolveScoopShim(command) }

func resolveScoopShim(command string) string {
	if !strings.EqualFold(filepath.Ext(command), ".exe") || !strings.EqualFold(filepath.Base(filepath.Dir(command)), "shims") {
		return command
	}
	shimPath := strings.TrimSuffix(command, filepath.Ext(command)) + ".shim"
	file, err := os.Open(shimPath)
	if err != nil {
		return command
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return command
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "path" {
			continue
		}
		target := strings.Trim(strings.TrimSpace(value), `"`)
		if resolved, ok := validatedScoopTarget(command, target); ok {
			return resolved
		}
		return command
	}
	return command
}

func validatedScoopTarget(command, target string) (string, bool) {
	if !localDrivePath(command) || !localDrivePath(target) || !strings.EqualFold(filepath.Base(command), filepath.Base(target)) {
		return "", false
	}
	extension := filepath.Ext(target)
	if !strings.EqualFold(extension, ".exe") && !strings.EqualFold(extension, ".com") {
		return "", false
	}
	appsRoot := filepath.Join(filepath.Dir(filepath.Dir(command)), "apps")
	resolvedRoot, err := finalWindowsPath(appsRoot)
	if err != nil {
		return "", false
	}
	resolvedTarget, err := finalWindowsPath(target)
	if err != nil || !localDrivePath(resolvedTarget) {
		return "", false
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	packageName := strings.TrimSuffix(filepath.Base(command), filepath.Ext(command))
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) < 2 || !strings.EqualFold(parts[0], packageName) {
		return "", false
	}
	info, err := os.Stat(resolvedTarget)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return resolvedTarget, true
}

func localDrivePath(path string) bool {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	return filepath.IsAbs(clean) && len(volume) == 2 && volume[1] == ':'
}

func finalWindowsPath(path string) (string, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 256)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), volumeNameDOS)
		if err != nil {
			return "", err
		}
		if length < uint32(len(buffer)) {
			resolved := windows.UTF16ToString(buffer[:length])
			switch {
			case strings.HasPrefix(resolved, `\\?\UNC\`):
				return `\\` + resolved[len(`\\?\UNC\`):], nil
			case strings.HasPrefix(resolved, `\\?\`):
				return filepath.Clean(resolved[len(`\\?\`):]), nil
			default:
				return "", fmt.Errorf("无法识别 Windows 最终路径: %s", resolved)
			}
		}
		buffer = make([]uint16, length+1)
	}
}
