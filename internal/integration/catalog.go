package integration

import (
	"os"
	"path/filepath"
)

type descriptor struct {
	id           string
	name         string
	commands     []string
	method       string
	note         string
	restartHint  string
	configKind   string
	rootKey      string
	installPaths func(*Manager) []string
}

func toolDescriptors() []descriptor {
	return []descriptor{
		{id: "opencode", name: "OpenCode", commands: []string{"opencode"}, method: "官方 CLI", note: "写入全局 opencode.json/jsonc，保留注释和其他配置。", restartHint: "请完全退出并重新启动 OpenCode", configKind: "opencode"},
		{id: "claude-code", name: "Claude Code", commands: []string{"claude"}, method: "官方 CLI", note: "使用 user scope 配置，不修改项目级 .mcp.json。", restartHint: "请重新启动 Claude Code", configKind: "claude"},
		{id: "codex", name: "OpenAI Codex", commands: []string{"codex"}, method: "官方 CLI", note: "通过 codex mcp 管理 ~/.codex/config.toml。", restartHint: "请重新启动 Codex", configKind: "codex"},
		{id: "kimi-code", name: "Kimi Code CLI", commands: []string{"kimi"}, method: "配置文件", note: "写入用户级 ~/.kimi-code/mcp.json，不影响项目级配置。", restartHint: "请新建 Kimi Code 会话", configKind: "standard", rootKey: "mcpServers"},
		{id: "zcode", name: "ZCode", commands: []string{"zcode"}, method: "配置文件", note: "写入 ~/.zcode/cli/config.json 的 mcpServers。", restartHint: "请重新启动 ZCode", configKind: "standard", rootKey: "mcpServers"},
		{id: "cursor", name: "Cursor", commands: []string{"cursor"}, method: "配置文件", note: "写入用户级 ~/.cursor/mcp.json。", restartHint: "请重新启动 Cursor 或刷新 MCP", configKind: "standard", rootKey: "mcpServers", installPaths: cursorInstallPaths},
		{id: "vscode", name: "Visual Studio Code", commands: []string{"code"}, method: "配置文件", note: "写入用户级 mcp.json 的 servers。", restartHint: "请在 VS Code 中刷新 MCP 服务器", configKind: "vscode", rootKey: "servers", installPaths: vscodeInstallPaths},
		{id: "claude-desktop", name: "Claude Desktop", commands: []string{"claude-desktop"}, method: "配置文件", note: "写入 Claude Desktop 用户配置。", restartHint: "请完全退出并重新启动 Claude Desktop", configKind: "standard", rootKey: "mcpServers", installPaths: claudeDesktopInstallPaths},
	}
}

func (m *Manager) Detect() []ToolInfo {
	items := make([]ToolInfo, 0, len(toolDescriptors()))
	for _, item := range toolDescriptors() {
		items = append(items, m.detect(item))
	}
	return items
}

func (m *Manager) detect(item descriptor) ToolInfo {
	commandPath := ""
	for _, name := range item.commands {
		if path, err := m.lookPath(name); err == nil {
			commandPath = path
			break
		}
	}
	paths := m.configPaths(item.id)
	writePaths := paths
	if item.id == "opencode" && len(paths) > 2 {
		writePaths = paths[:2]
	}
	configPath := firstExisting(writePaths)
	if configPath == "" && len(paths) > 0 {
		configPath = writePaths[0]
	}
	installed := commandPath != ""
	if !installed && item.installPaths != nil {
		installed = anyExists(item.installPaths(m))
	}
	if !installed && (item.id == "kimi-code" || item.id == "zcode" || item.id == "claude-desktop") {
		installed = anyExists(paths)
	}
	return ToolInfo{
		ID:          item.id,
		Name:        item.name,
		Installed:   installed,
		Configured:  m.configured(item, paths),
		CommandPath: commandPath,
		ConfigPath:  configPath,
		Method:      item.method,
		Note:        item.note,
		RestartHint: item.restartHint,
	}
}

func (m *Manager) configPaths(id string) []string {
	switch id {
	case "opencode":
		base := m.getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(m.homeDir, ".config")
		}
		root := filepath.Join(base, "opencode")
		return []string{filepath.Join(root, "opencode.json"), filepath.Join(root, "opencode.jsonc"), filepath.Join(root, "config.json")}
	case "claude-code":
		return []string{filepath.Join(m.homeDir, ".claude.json")}
	case "codex":
		root := m.getenv("CODEX_HOME")
		if root == "" {
			root = filepath.Join(m.homeDir, ".codex")
		}
		return []string{filepath.Join(root, "config.toml")}
	case "kimi-code":
		root := m.getenv("KIMI_CODE_HOME")
		if root == "" {
			root = filepath.Join(m.homeDir, ".kimi-code")
		}
		return []string{filepath.Join(root, "mcp.json")}
	case "zcode":
		return []string{filepath.Join(m.homeDir, ".zcode", "cli", "config.json")}
	case "cursor":
		return []string{filepath.Join(m.homeDir, ".cursor", "mcp.json")}
	case "vscode":
		return []string{filepath.Join(m.appData, "Code", "User", "mcp.json")}
	case "claude-desktop":
		return []string{filepath.Join(m.appData, "Claude", "claude_desktop_config.json")}
	default:
		return nil
	}
}

func (m *Manager) configured(item descriptor, paths []string) bool {
	if item.configKind == "codex" {
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err == nil && codexSection.Match(data) {
				return true
			}
		}
		return false
	}
	rootKey := item.rootKey
	if item.configKind == "opencode" {
		rootKey = "mcp"
	} else if item.configKind == "claude" {
		rootKey = "mcpServers"
	}
	for _, path := range paths {
		root, err := readJSONObject(path)
		if err != nil {
			continue
		}
		entries, _ := root[rootKey].(map[string]any)
		if _, ok := entries[serverName]; ok {
			return true
		}
	}
	return false
}

func firstExisting(paths []string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func anyExists(paths []string) bool {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func cursorInstallPaths(m *Manager) []string {
	return []string{
		filepath.Join(m.localAppData, "Programs", "cursor", "Cursor.exe"),
		filepath.Join(m.localAppData, "Programs", "Cursor", "Cursor.exe"),
	}
}

func vscodeInstallPaths(m *Manager) []string {
	paths := []string{filepath.Join(m.localAppData, "Programs", "Microsoft VS Code", "Code.exe")}
	if programFiles := m.getenv("ProgramFiles"); programFiles != "" {
		paths = append(paths, filepath.Join(programFiles, "Microsoft VS Code", "Code.exe"))
	}
	return paths
}

func claudeDesktopInstallPaths(m *Manager) []string {
	return []string{
		filepath.Join(m.localAppData, "Programs", "claude", "Claude.exe"),
		filepath.Join(m.localAppData, "AnthropicClaude", "Claude.exe"),
	}
}
