package integration

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	codexSectionLine = regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*mcp_servers\.(?:ssh-mcp|"ssh-mcp")[ \t]*\][ \t]*(?:#[^\r\n]*)?(?:\r?\n|$)`)
	tomlSectionLine  = regexp.MustCompile(`(?m)^[ \t]*\[[^\r\n]+\][ \t]*(?:#[^\r\n]*)?(?:\r?\n|$)`)
)

func removeCodexConfig(path string) (string, bool, error) {
	snapshot, err := captureFile(path)
	if err != nil {
		return "", false, err
	}
	if !snapshot.exists {
		return "", false, nil
	}
	section := codexSectionLine.FindIndex(snapshot.data)
	if section == nil {
		return "", false, nil
	}
	end := len(snapshot.data)
	if next := tomlSectionLine.FindIndex(snapshot.data[section[1]:]); next != nil {
		end = section[1] + next[0]
	}
	data := make([]byte, 0, len(snapshot.data)-(end-section[0]))
	data = append(data, snapshot.data[:section[0]]...)
	data = append(data, snapshot.data[end:]...)
	backup, err := snapshot.writeBackup()
	if err != nil {
		return backup, false, err
	}
	unchanged, err := snapshot.unchanged()
	if err != nil {
		return backup, false, fmt.Errorf("重新检查配置失败: %w", err)
	}
	if !unchanged {
		return backup, false, errors.New("配置已被其他进程更新，请刷新检测后重试")
	}
	if err := atomicWrite(path, data, snapshot.mode); err != nil {
		return backup, false, fmt.Errorf("移除 Codex 配置失败，原文件未替换: %w", err)
	}
	return backup, true, nil
}
