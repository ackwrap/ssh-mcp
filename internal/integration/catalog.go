package integration

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type descriptor struct {
	id           string
	name         string
	commands     []string
	method       string
	note         string
	restartHint  string
	configKind   string
	rootPath     []string
	transport    string
	commandPaths func(*Manager) []string
	installPaths func(*Manager) []string
}

func toolDescriptors() []descriptor {
	return []descriptor{
		{id: "opencode", name: "OpenCode", commands: []string{"opencode"}, method: "官方 CLI", note: "优先直接启动 Scoop 或 npm 安装的真实 OpenCode，不经过 shim。", restartHint: "请完全退出并重新启动 OpenCode", configKind: "opencode", transport: "stdio", commandPaths: opencodeCommandPaths},
		{id: "claude-code", name: "Claude Code", commands: []string{"claude"}, method: "官方 CLI", note: "使用 user scope 配置，不修改项目级 .mcp.json。", restartHint: "请重新启动 Claude Code", configKind: "claude", transport: "stdio"},
		{id: "codex", name: "OpenAI Codex", commands: []string{"codex"}, method: "官方 CLI", note: "直接检测 npm 用户安装路径，并通过 codex mcp 管理 ~/.codex/config.toml。", restartHint: "请重新启动 Codex", configKind: "codex", transport: "stdio", commandPaths: codexCommandPaths},
		{id: "kimi-code", name: "Kimi Code CLI", commands: []string{"kimi"}, method: "配置文件", note: "写入用户级 ~/.kimi/mcp.json，不影响项目级配置。", restartHint: "请新建 Kimi Code 会话", configKind: "standard", rootPath: []string{"mcpServers"}, transport: "stdio"},
		{id: "zcode", name: "ZCode", commands: []string{"zcode"}, method: "配置文件", note: "写入 ~/.zcode/cli/config.json 的 mcp.servers。", restartHint: "请重新启动 ZCode", configKind: "standard", rootPath: []string{"mcp", "servers"}, transport: "stdio"},
		{id: "cursor", name: "Cursor", commands: []string{"cursor"}, method: "配置文件", note: "写入用户级 ~/.cursor/mcp.json。", restartHint: "请重新启动 Cursor 或刷新 MCP", configKind: "standard", rootPath: []string{"mcpServers"}, transport: "stdio", installPaths: cursorInstallPaths},
		{id: "vscode", name: "Visual Studio Code", commands: []string{"code"}, method: "配置文件", note: "写入用户级 mcp.json 的 servers。", restartHint: "请在 VS Code 中刷新 MCP 服务器", configKind: "vscode", rootPath: []string{"servers"}, transport: "stdio", installPaths: vscodeInstallPaths},
		{id: "claude-desktop", name: "Claude Desktop", commands: []string{"claude-desktop"}, method: "配置文件", note: "写入 Claude Desktop 用户级 stdio 配置。", restartHint: "请完全退出并重新启动 Claude Desktop", configKind: "standard", rootPath: []string{"mcpServers"}, transport: "stdio", installPaths: claudeDesktopInstallPaths},
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
	if item.commandPaths != nil {
		commandPath = firstRegularFile(item.commandPaths(m))
	}
	if commandPath == "" {
		for _, name := range item.commands {
			if path, err := m.lookPath(name); err == nil {
				commandPath = directCommandPath(path)
				break
			}
		}
	}
	paths := m.configPaths(item.id)
	writePaths := paths
	if item.id == "opencode" && len(paths) > 2 {
		writePaths = paths[:2]
	}
	configPath := firstExisting(writePaths)
	if configPath == "" {
		configPath = firstExisting(paths)
	}
	if configPath == "" && len(paths) > 0 {
		configPath = writePaths[0]
	}
	installed := commandPath != ""
	if !installed && item.installPaths != nil {
		installed = anyExists(item.installPaths(m))
	}
	present, configured := m.configurationState(item, paths)
	return ToolInfo{
		ID:          item.id,
		Name:        item.name,
		Installed:   installed,
		Present:     present,
		Configured:  configured,
		CommandPath: commandPath,
		ConfigPath:  configPath,
		Method:      item.method,
		Transport:   item.transport,
		Note:        item.note,
		RestartHint: item.restartHint,
	}
}

func opencodeCommandPaths(m *Manager) []string {
	return []string{
		filepath.Join(m.homeDir, "scoop", "apps", "opencode", "current", "opencode.exe"),
		filepath.Join(m.homeDir, "scoop", "persist", "nodejs", "bin", "node_modules", "opencode-ai", "bin", "opencode.exe"),
		filepath.Join(m.appData, "npm", "node_modules", "opencode-ai", "bin", "opencode.exe"),
	}
}

func codexCommandPaths(m *Manager) []string {
	return []string{
		filepath.Join(m.homeDir, "scoop", "persist", "nodejs", "bin", "codex.cmd"),
		filepath.Join(m.homeDir, "scoop", "apps", "nodejs", "current", "bin", "codex.cmd"),
		filepath.Join(m.appData, "npm", "codex.cmd"),
	}
}

func firstRegularFile(paths []string) string {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
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
		root := m.getenv("KIMI_SHARE_DIR")
		if root == "" {
			root = filepath.Join(m.homeDir, ".kimi")
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

func (m *Manager) configurationState(item descriptor, paths []string) (bool, bool) {
	if item.configKind == "codex" {
		present := false
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			found, configured := codexConfigState(data, m.serverCommand)
			present = present || found
			if configured {
				return true, true
			}
		}
		return present, false
	}
	rootPath := item.rootPath
	if item.configKind == "opencode" {
		rootPath = []string{"mcp"}
	} else if item.configKind == "claude" {
		rootPath = []string{"mcpServers"}
	}
	present := false
	for _, path := range paths {
		root, err := readJSONObject(path)
		if err != nil {
			continue
		}
		entries, _ := objectAtPath(root, rootPath)
		value, exists := entries[serverName]
		if !exists {
			continue
		}
		present = true
		entry, _ := value.(map[string]any)
		if m.entryConfigured(item, entry) {
			return true, true
		}
	}
	return present, false
}

func (m *Manager) entryConfigured(item descriptor, entry map[string]any) bool {
	if entry == nil {
		return false
	}
	switch item.id {
	case "opencode":
		command, _ := entry["command"].([]any)
		return entry["type"] == "local" && commandMatches(command, m.serverCommand)
	case "zcode", "vscode":
		return entry["type"] == "stdio" && stdioEntryMatches(entry, m.serverCommand)
	default:
		return stdioEntryMatches(entry, m.serverCommand)
	}
}

func stdioEntryMatches(entry map[string]any, command []string) bool {
	executable, _ := entry["command"].(string)
	if len(command) == 0 || executable != command[0] {
		return false
	}
	args, _ := entry["args"].([]any)
	return commandMatches(args, command[1:])
}

func commandMatches(actual []any, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index, value := range expected {
		if actual[index] != value {
			return false
		}
	}
	return true
}

func codexConfigState(data []byte, command []string) (bool, bool) {
	inServerSection := false
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			if inServerSection {
				break
			}
			header := strings.TrimSpace(strings.SplitN(trimmed, "#", 2)[0])
			inServerSection = codexSection.MatchString(header)
			continue
		}
		if !inServerSection {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if !inServerSection && len(values) == 0 {
		return false, false
	}
	executable, ok := parseTOMLString(values["command"])
	if !ok || len(command) == 0 || executable != command[0] {
		return true, false
	}
	args, ok := parseTOMLStringArray(values["args"])
	if values["args"] == "" {
		args, ok = nil, true
	}
	if !ok || len(args) != len(command)-1 {
		return true, false
	}
	for index, expected := range command[1:] {
		if args[index] != expected {
			return true, false
		}
	}
	return true, true
}

func parseTOMLStringArray(value string) ([]string, bool) {
	value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
	if value == "[]" {
		return nil, true
	}
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, false
	}
	var result []string
	remaining := strings.TrimSpace(value[1 : len(value)-1])
	for remaining != "" {
		if remaining[0] != '"' && remaining[0] != '\'' {
			return nil, false
		}
		end := tomlStringEnd(remaining)
		if end < 0 {
			return nil, false
		}
		parsed, ok := parseTOMLString(remaining[:end+1])
		if !ok {
			return nil, false
		}
		result = append(result, parsed)
		remaining = strings.TrimSpace(remaining[end+1:])
		if remaining == "" {
			break
		}
		if remaining[0] != ',' {
			return nil, false
		}
		remaining = strings.TrimSpace(remaining[1:])
	}
	return result, true
}

func tomlStringEnd(value string) int {
	quote := value[0]
	escaped := false
	for index := 1; index < len(value); index++ {
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && value[index] == '\\' {
			escaped = true
			continue
		}
		if value[index] == quote {
			return index
		}
	}
	return -1
}

func parseTOMLString(value string) (string, bool) {
	if len(value) < 2 || (value[0] != '"' && value[0] != '\'') {
		return "", false
	}
	quote := value[0]
	escaped := false
	for index := 1; index < len(value); index++ {
		current := value[index]
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && current == '\\' {
			escaped = true
			continue
		}
		if current != quote {
			continue
		}
		rest := strings.TrimSpace(value[index+1:])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", false
		}
		if quote == '\'' {
			return value[1:index], true
		}
		parsed, err := strconv.Unquote(value[:index+1])
		return parsed, err == nil
	}
	return "", false
}

func objectAtPath(root map[string]any, path []string) (map[string]any, bool) {
	current := root
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
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
