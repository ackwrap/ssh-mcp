package integration

import (
	"context"
	"errors"
	"fmt"
)

func (m *Manager) Remove(ctx context.Context, ids []string) []Result {
	m.mu.Lock()
	defer m.mu.Unlock()

	descriptors := make(map[string]descriptor, len(toolDescriptors()))
	for _, item := range toolDescriptors() {
		descriptors[item.id] = item
	}
	seen := make(map[string]struct{}, len(ids))
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		item, ok := descriptors[id]
		if !ok {
			results = append(results, Result{ID: id, Name: id, Message: "不支持的 AI 工具"})
			continue
		}
		info := m.detect(item)
		result := Result{ID: item.id, Name: item.name, ConfigPath: info.ConfigPath, Transport: item.transport}
		if !info.Present {
			result.OK = true
			result.Message = "未发现 ssh-mcp 配置，已跳过"
			results = append(results, result)
			continue
		}
		backup, err := m.removeOne(ctx, item, info)
		result.BackupPath = backup
		if err != nil {
			result.Message = err.Error()
		} else {
			result.OK = true
			result.Message = "已移除 ssh-mcp 配置；" + item.restartHint
		}
		results = append(results, result)
	}
	return results
}

func (m *Manager) removeOne(ctx context.Context, item descriptor, info ToolInfo) (string, error) {
	switch item.configKind {
	case "opencode":
		backup, _, err := removeJSONConfigs(m.configPaths(item.id), []string{"mcp", "servers"}, []string{"mcp"})
		return backup, err
	case "claude":
		if !info.Installed {
			backup, _, err := removeJSONConfigs(m.configPaths(item.id), []string{"mcpServers"})
			return backup, err
		}
		return m.removeCLI(ctx, item, info, []string{"mcp", "remove", "--scope", "user", serverName})
	case "codex":
		if !info.Installed {
			backup, _, err := removeCodexConfig(info.ConfigPath)
			return backup, err
		}
		return m.removeCLI(ctx, item, info, []string{"mcp", "remove", serverName})
	case "standard", "vscode":
		backup, _, err := removeJSONConfigs([]string{info.ConfigPath}, item.rootPath)
		return backup, err
	default:
		return "", errors.New("该工具尚未实现取消接入适配器")
	}
}

func (m *Manager) removeCLI(ctx context.Context, item descriptor, info ToolInfo, args []string) (string, error) {
	snapshots, err := captureFiles(m.configPaths(item.id))
	if err != nil {
		return "", err
	}
	backup, err := writeBackups(snapshots)
	if err != nil {
		return backup, err
	}
	output, err := m.run(ctx, info.CommandPath, args...)
	if err != nil {
		return backup, fmt.Errorf("%s 取消接入失败（%s）；为避免覆盖并发更新未自动回滚，请检查当前配置并按需从备份恢复", item.name, safeCommandError(output, err))
	}
	return backup, nil
}
