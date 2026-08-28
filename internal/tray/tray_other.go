//go:build !windows

package tray

import "errors"

func Run(openWebUI bool) error {
	return errors.New("SSH-MCP 当前仅支持 Windows 托盘模式；Linux/macOS 请使用 `--mcp` 模式")
}
