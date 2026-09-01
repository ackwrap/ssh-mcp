package integration

import (
	"context"
	"errors"
	"fmt"
)

func (m *Manager) Configure(ctx context.Context, ids []string) []Result {
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
		if !info.Installed {
			result.Message = "未检测到该工具，未修改任何配置"
			results = append(results, result)
			continue
		}
		if info.Configured {
			result.OK = true
			result.Message = "已存在相同的 " + item.transport + " ssh-mcp 配置，已跳过"
			results = append(results, result)
			continue
		}
		backup, err := m.configureOne(ctx, item, info)
		result.BackupPath = backup
		if err != nil {
			result.Message = err.Error()
		} else {
			result.OK = true
			result.Message = "已通过 " + item.transport + " 写入 ssh-mcp 配置；" + item.restartHint
		}
		results = append(results, result)
	}
	return results
}

func (m *Manager) configureOne(ctx context.Context, item descriptor, info ToolInfo) (string, error) {
	switch item.configKind {
	case "opencode":
		add := append([]string{"mcp", "add", serverName, "--"}, m.serverCommand...)
		return m.configureCLI(ctx, item, info, nil, add)
	case "claude":
		remove := []string{"mcp", "remove", "--scope", "user", serverName}
		add := append([]string{"mcp", "add", "--scope", "user", serverName, "--"}, m.serverCommand...)
		return m.configureCLI(ctx, item, info, remove, add)
	case "codex":
		remove := []string{"mcp", "remove", serverName}
		add := append([]string{"mcp", "add", serverName, "--"}, m.serverCommand...)
		return m.configureCLI(ctx, item, info, remove, add)
	case "standard", "vscode":
		return mergeJSONConfig(info.ConfigPath, item.rootPath, m.configEntry(item))
	default:
		return "", errors.New("该工具尚未实现配置适配器")
	}
}

func (m *Manager) configEntry(item descriptor) map[string]any {
	entry := map[string]any{"command": m.serverCommand[0]}
	if len(m.serverCommand) > 1 {
		entry["args"] = m.serverCommand[1:]
	}
	if item.id == "zcode" || item.id == "vscode" {
		entry["type"] = "stdio"
	}
	return entry
}

func (m *Manager) configureCLI(ctx context.Context, item descriptor, info ToolInfo, remove, add []string) (string, error) {
	snapshots, err := captureFiles(m.configPaths(item.id))
	if err != nil {
		return "", err
	}
	backup, err := writeBackups(snapshots)
	if err != nil {
		return "", err
	}
	if len(remove) > 0 {
		_, _ = m.run(ctx, info.CommandPath, remove...)
	}
	output, err := m.run(ctx, info.CommandPath, add...)
	if err != nil {
		restoreErr := restoreSnapshots(snapshots)
		message := safeCommandError(output, err)
		if restoreErr != nil {
			return backup, fmt.Errorf("%s 配置失败（%s），自动回滚也失败: %v；请从备份手动恢复", item.name, message, restoreErr)
		}
		return backup, fmt.Errorf("%s 配置失败（%s），原配置已恢复", item.name, message)
	}
	return backup, nil
}
