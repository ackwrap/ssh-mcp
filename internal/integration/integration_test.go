package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const testServerURL = "http://127.0.0.1:18877/mcp/"

func TestNewManagerRejectsNonLoopbackServerURL(t *testing.T) {
	_, err := NewManager(Options{ServerCommand: []string{"ssh-mcp"}, ServerURL: "https://example.com/mcp/"})
	if err == nil {
		t.Fatal("non-loopback MCP URL was accepted")
	}
}

func testManager(t *testing.T, lookPath func(string) (string, error), run func(context.Context, string, ...string) (string, error)) *Manager {
	t.Helper()
	home := t.TempDir()
	manager, err := NewManager(Options{
		ServerCommand: []string{`C:\Program Files\SSH-MCP\ssh-mcp.exe`},
		ServerURL:     testServerURL,
		HomeDir:       home,
		AppData:       filepath.Join(home, "AppData", "Roaming"),
		LocalAppData:  filepath.Join(home, "AppData", "Local"),
		Getenv:        func(string) string { return "" },
		LookPath:      lookPath,
		Run:           run,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestDetectInstalledAndConfiguredJSONC(t *testing.T) {
	manager := testManager(t, func(name string) (string, error) {
		if name == "opencode" {
			return `C:\bin\opencode.exe`, nil
		}
		return "", errors.New("not found")
	}, nil)
	path := manager.configPaths("opencode")[1]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	command, _ := json.Marshal(manager.serverCommand)
	content := `{
  // Existing user comment
  "mcp": {
    "ssh-mcp": {"type": "local", "command": ` + string(command) + `,},
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := manager.Detect()
	openCode := findTool(t, tools, "opencode")
	if !openCode.Installed || !openCode.Present || !openCode.Configured || openCode.ConfigPath != path {
		t.Fatalf("OpenCode detection = %#v", openCode)
	}
	if findTool(t, tools, "codex").Installed {
		t.Fatal("Codex should not be installed")
	}
	content = `{"mcp":{"ssh-mcp":{"type":"remote","url":"` + testServerURL + `"}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := findTool(t, manager.Detect(), "opencode")
	if !stale.Present || stale.Configured {
		t.Fatalf("legacy HTTP OpenCode config should be migrated to stdio: %#v", stale)
	}
}

func TestDetectOpenCodePrefersDirectInstallOverPATHShim(t *testing.T) {
	manager := testManager(t, func(name string) (string, error) {
		if name == "opencode" {
			return `C:\broken\scoop\shims\opencode.exe`, nil
		}
		return "", errors.New("not found")
	}, nil)
	direct := filepath.Join(manager.homeDir, "scoop", "apps", "opencode", "current", "opencode.exe")
	if err := os.MkdirAll(filepath.Dir(direct), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(direct, []byte("opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	tool := findTool(t, manager.Detect(), "opencode")
	if tool.CommandPath != direct {
		t.Fatalf("command path = %q, want direct install %q", tool.CommandPath, direct)
	}
}

func TestDetectCodexFromKnownNPMPathWithoutPATH(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	direct := filepath.Join(manager.homeDir, "scoop", "persist", "nodejs", "bin", "codex.cmd")
	if err := os.MkdirAll(filepath.Dir(direct), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(direct, []byte("@echo off"), 0o700); err != nil {
		t.Fatal(err)
	}
	tool := findTool(t, manager.Detect(), "codex")
	if !tool.Installed || tool.CommandPath != direct {
		t.Fatalf("Codex detection = %#v, want command %q", tool, direct)
	}
}

func TestResidualConfigDoesNotMarkToolInstalledOrAutoEligible(t *testing.T) {
	tests := []struct {
		id      string
		content string
	}{
		{id: "kimi-code", content: `{"mcpServers":{"ssh-mcp":{"url":"` + testServerURL + `"}}}`},
		{id: "zcode", content: `{"mcp":{"servers":{"ssh-mcp":{"type":"http","url":"` + testServerURL + `"}}}}`},
		{id: "claude-desktop", content: `{"mcpServers":{"ssh-mcp":{"url":"` + testServerURL + `"}}}`},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
			path := manager.configPaths(test.id)[0]
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			tool := findTool(t, manager.Detect(), test.id)
			if tool.Installed || !tool.Present || tool.Configured {
				t.Fatalf("residual state = %#v", tool)
			}
		})
	}
}

func TestCodexConfigStateRequiresCurrentStdioCommand(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	current := []byte("[mcp_servers.ssh-mcp] # SSH-MCP\ncommand = " + strconv.Quote(manager.serverCommand[0]) + "\n")
	present, configured := codexConfigState(current, manager.serverCommand)
	if !present || !configured {
		t.Fatalf("current Codex stdio config state = %v, %v", present, configured)
	}
	legacyHTTP := []byte("[mcp_servers.ssh-mcp]\nurl = \"" + testServerURL + "\"\n")
	present, configured = codexConfigState(legacyHTTP, manager.serverCommand)
	if !present || configured {
		t.Fatalf("legacy Codex HTTP config state = %v, %v", present, configured)
	}
}

func TestDirectConfigDetectionRequiresMatchingStdioCommand(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	stdio := map[string]any{"command": manager.serverCommand[0]}
	tests := []struct {
		id       string
		matching map[string]any
		stale    map[string]any
	}{
		{id: "kimi-code", matching: stdio, stale: map[string]any{"transport": "http", "url": testServerURL}},
		{id: "zcode", matching: map[string]any{"type": "stdio", "command": manager.serverCommand[0]}, stale: map[string]any{"type": "http", "url": testServerURL}},
		{id: "vscode", matching: map[string]any{"type": "stdio", "command": manager.serverCommand[0]}, stale: map[string]any{"type": "http", "url": testServerURL}},
		{id: "cursor", matching: stdio, stale: map[string]any{"url": testServerURL}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			item := descriptor{id: test.id}
			if !manager.entryConfigured(item, test.matching) {
				t.Fatalf("matching entry was not detected: %#v", test.matching)
			}
			if manager.entryConfigured(item, test.stale) {
				t.Fatalf("stale entry was incorrectly accepted: %#v", test.stale)
			}
		})
	}
}

func TestConfigureKimiPreservesExistingConfigAndBacksUp(t *testing.T) {
	manager := testManager(t, func(name string) (string, error) {
		if name == "kimi" {
			return `C:\bin\kimi.exe`, nil
		}
		return "", errors.New("not found")
	}, nil)
	path := manager.configPaths("kimi-code")[0]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcpServers":{"existing":{"url":"https://example.com/mcp"}},"other":true}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	results := manager.Configure(context.Background(), []string{"kimi-code"})
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("results = %#v", results)
	}
	backup, err := os.ReadFile(results[0].BackupPath)
	if err != nil || !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %q, err = %v", backup, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["existing"]; !ok {
		t.Fatal("existing MCP server was removed")
	}
	sshMCP := servers[serverName].(map[string]any)
	if sshMCP["command"] != manager.serverCommand[0] {
		t.Fatalf("ssh-mcp entry = %#v", sshMCP)
	}
	if _, exists := sshMCP["args"]; exists {
		t.Fatalf("default stdio entry should not include args: %#v", sshMCP)
	}
}

func TestDirectConfigAdaptersWriteExpectedRootShape(t *testing.T) {
	tests := []struct {
		id       string
		command  string
		rootPath []string
		typed    bool
	}{
		{id: "zcode", command: "zcode", rootPath: []string{"mcp", "servers"}, typed: true},
		{id: "cursor", command: "cursor", rootPath: []string{"mcpServers"}},
		{id: "vscode", command: "code", rootPath: []string{"servers"}, typed: true},
		{id: "claude-desktop", command: "claude-desktop", rootPath: []string{"mcpServers"}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			manager := testManager(t, func(name string) (string, error) {
				if name == test.command {
					return `C:\bin\` + name + ".exe", nil
				}
				return "", errors.New("not found")
			}, nil)
			result := manager.Configure(context.Background(), []string{test.id})[0]
			if !result.OK {
				t.Fatalf("result = %#v", result)
			}
			data, err := os.ReadFile(result.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatal(err)
			}
			entries, ok := objectAtPath(root, test.rootPath)
			if !ok {
				t.Fatalf("missing config path %v in %#v", test.rootPath, root)
			}
			entry := entries[serverName].(map[string]any)
			if entry["command"] != manager.serverCommand[0] {
				t.Fatalf("stdio entry = %#v", entry)
			}
			if _, exists := entry["args"]; exists {
				t.Fatalf("default stdio entry should not include args: %#v", entry)
			}
			if test.typed && entry["type"] != "stdio" {
				t.Fatalf("typed stdio entry = %#v", entry)
			}
		})
	}
}

func TestOfficialCLIArgumentsAreFixed(t *testing.T) {
	type call struct {
		command string
		args    []string
	}
	calls := []call{}
	manager := testManager(t, func(name string) (string, error) {
		return `C:\bin\` + name + ".exe", nil
	}, func(ctx context.Context, command string, args ...string) (string, error) {
		calls = append(calls, call{command: command, args: append([]string(nil), args...)})
		return "ok", nil
	})
	results := manager.Configure(context.Background(), []string{"opencode", "claude-code", "codex"})
	for _, result := range results {
		if !result.OK {
			t.Fatalf("result = %#v", result)
		}
	}
	want := [][]string{
		{"mcp", "add", serverName, "--", manager.serverCommand[0]},
		{"mcp", "remove", "--scope", "user", serverName},
		{"mcp", "add", "--scope", "user", serverName, "--", manager.serverCommand[0]},
		{"mcp", "remove", serverName},
		{"mcp", "add", serverName, "--", manager.serverCommand[0]},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	for index := range want {
		if !reflect.DeepEqual(calls[index].args, want[index]) {
			t.Fatalf("call %d args = %#v, want %#v", index, calls[index].args, want[index])
		}
	}
}

func TestConfigureSkipsMatchingOpenCodeConfig(t *testing.T) {
	calls := 0
	manager := testManager(t, func(name string) (string, error) {
		if name == "opencode" {
			return `C:\bin\opencode.exe`, nil
		}
		return "", errors.New("not found")
	}, func(context.Context, string, ...string) (string, error) {
		calls++
		return "", nil
	})
	path := manager.configPaths("opencode")[0]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	command, _ := json.Marshal(manager.serverCommand)
	content := []byte(`{"mcp":{"ssh-mcp":{"type":"local","command":` + string(command) + `}}}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Configure(context.Background(), []string{"opencode"})[0]
	if !result.OK || !strings.Contains(result.Message, "已跳过") || result.BackupPath != "" {
		t.Fatalf("result = %#v", result)
	}
	if calls != 0 {
		t.Fatalf("matching config executed CLI %d time(s)", calls)
	}
	if _, err := os.Stat(path + ".ssh-mcp.bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skip unexpectedly created backup: %v", err)
	}
}

func TestCLIFailureRestoresOriginalConfig(t *testing.T) {
	var configPath string
	manager := testManager(t, func(name string) (string, error) {
		if name == "codex" {
			return `C:\bin\codex.exe`, nil
		}
		return "", errors.New("not found")
	}, func(ctx context.Context, command string, args ...string) (string, error) {
		if len(args) > 1 && args[1] == "add" {
			if err := os.WriteFile(configPath, []byte("broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "add failed TOP_SECRET_TOKEN", errors.New("exit 1")
		}
		return "", nil
	})
	configPath = manager.configPaths("codex")[0]
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("[mcp_servers.existing]\ncommand = \"example\"\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Configure(context.Background(), []string{"codex"})[0]
	if result.OK || !strings.Contains(result.Message, "原配置已恢复") || strings.Contains(result.Message, "TOP_SECRET_TOKEN") {
		t.Fatalf("result = %#v", result)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil || !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored = %q, err = %v", restored, err)
	}
}

func TestOpenCodeFailureRestoresEveryPossibleConfigTarget(t *testing.T) {
	var paths []string
	manager := testManager(t, func(name string) (string, error) {
		if name == "opencode" {
			return `C:\bin\opencode.exe`, nil
		}
		return "", errors.New("not found")
	}, func(ctx context.Context, command string, args ...string) (string, error) {
		if err := os.MkdirAll(filepath.Dir(paths[0]), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[0], []byte("new broken file"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[2], []byte("overwritten secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		return "API_KEY=TOP_SECRET", errors.New("exit 1")
	})
	paths = manager.configPaths("opencode")
	if err := os.MkdirAll(filepath.Dir(paths[2]), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcp":{"existing":{"url":"https://example.com"}}}`)
	if err := os.WriteFile(paths[2], original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Configure(context.Background(), []string{"opencode"})[0]
	if result.OK || strings.Contains(result.Message, "TOP_SECRET") {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new OpenCode config was not removed: %v", err)
	}
	restored, err := os.ReadFile(paths[2])
	if err != nil || !reflect.DeepEqual(restored, original) {
		t.Fatalf("config.json restored = %q, err=%v", restored, err)
	}
	backup, err := os.ReadFile(paths[2] + ".ssh-mcp.bak")
	if err != nil || !reflect.DeepEqual(backup, original) {
		t.Fatalf("config.json backup = %q, err=%v", backup, err)
	}
}

func TestRemoveDirectConfigPreservesOtherServersAndBacksUp(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	path := manager.configPaths("kimi-code")[0]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcpServers":{"existing":{"url":"https://example.com"},"ssh-mcp":{"command":"ssh-mcp.exe"}},"other":true}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Remove(context.Background(), []string{"kimi-code"})[0]
	if !result.OK || result.BackupPath == "" {
		t.Fatalf("result = %#v", result)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %q, err=%v", backup, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, exists := servers[serverName]; exists {
		t.Fatalf("ssh-mcp was not removed: %#v", servers)
	}
	if _, exists := servers["existing"]; !exists || root["other"] != true {
		t.Fatalf("unrelated config was changed: %#v", root)
	}
}

func TestRemoveOfficialCLIArgumentsAreFixed(t *testing.T) {
	type call struct {
		command string
		args    []string
	}
	var calls []call
	manager := testManager(t, func(name string) (string, error) {
		return `C:\bin\` + name + ".exe", nil
	}, func(_ context.Context, command string, args ...string) (string, error) {
		calls = append(calls, call{command: command, args: append([]string(nil), args...)})
		return "ok", nil
	})
	claudePath := manager.configPaths("claude-code")[0]
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(`{"mcpServers":{"ssh-mcp":{"type":"http","url":"`+testServerURL+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	codexPath := manager.configPaths("codex")[0]
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte("[mcp_servers.ssh-mcp]\nurl = \""+testServerURL+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := manager.Remove(context.Background(), []string{"claude-code", "codex"})
	for _, result := range results {
		if !result.OK {
			t.Fatalf("result = %#v", result)
		}
	}
	want := [][]string{
		{"mcp", "remove", "--scope", "user", serverName},
		{"mcp", "remove", serverName},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	for index := range want {
		if !reflect.DeepEqual(calls[index].args, want[index]) {
			t.Fatalf("call %d args = %#v, want %#v", index, calls[index].args, want[index])
		}
	}
}

func TestRemoveCLIFailureDoesNotOverwriteConcurrentUpdate(t *testing.T) {
	var configPath string
	manager := testManager(t, func(name string) (string, error) {
		if name == "codex" {
			return `C:\bin\codex.exe`, nil
		}
		return "", errors.New("not found")
	}, func(_ context.Context, _ string, _ ...string) (string, error) {
		if err := os.WriteFile(configPath, []byte("[mcp_servers.external]\ncommand = \"new\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return "TOKEN=TOP_SECRET", errors.New("exit 1")
	})
	configPath = manager.configPaths("codex")[0]
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("[mcp_servers.ssh-mcp]\ncommand = \"ssh-mcp.exe\"\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Remove(context.Background(), []string{"codex"})[0]
	if result.OK || !strings.Contains(result.Message, "未自动回滚") || strings.Contains(result.Message, "TOP_SECRET") {
		t.Fatalf("result = %#v", result)
	}
	current, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(current), "mcp_servers.external") {
		t.Fatalf("concurrent update was overwritten: %q, err=%v", current, err)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %q, err=%v", backup, err)
	}
}

func TestRemoveClaudeAndCodexResidualConfigWithoutInstalledCLI(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	claudePath := manager.configPaths("claude-code")[0]
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(`{"mcpServers":{"other":{"command":"other"},"ssh-mcp":{"url":"`+testServerURL+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	codexPath := manager.configPaths("codex")[0]
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatal(err)
	}
	codexOriginal := "[mcp_servers.other]\ncommand = \"other\"\n\n[mcp_servers.ssh-mcp] # remove\nurl = \"" + testServerURL + "\"\n\n[features]\nexample = true\n"
	if err := os.WriteFile(codexPath, []byte(codexOriginal), 0o600); err != nil {
		t.Fatal(err)
	}
	results := manager.Remove(context.Background(), []string{"claude-code", "codex"})
	for _, result := range results {
		if !result.OK || result.BackupPath == "" {
			t.Fatalf("result = %#v", result)
		}
	}
	claudeData, err := os.ReadFile(claudePath)
	if err != nil || strings.Contains(string(claudeData), serverName) || !strings.Contains(string(claudeData), "other") {
		t.Fatalf("Claude config = %q, err=%v", claudeData, err)
	}
	codexData, err := os.ReadFile(codexPath)
	if err != nil || strings.Contains(string(codexData), "mcp_servers.ssh-mcp") || !strings.Contains(string(codexData), "mcp_servers.other") || !strings.Contains(string(codexData), "[features]") {
		t.Fatalf("Codex config = %q, err=%v", codexData, err)
	}
}

func TestRemoveOpenCodeJSONCPreservesCommentsOnFailure(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	path := manager.configPaths("opencode")[1]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // keep\n  \"mcp\": {\"ssh-mcp\": {\"type\": \"remote\", \"url\": \"" + testServerURL + "\"}},\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Remove(context.Background(), []string{"opencode"})[0]
	if result.OK || !strings.Contains(result.Message, "注释或尾逗号") {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(data, original) {
		t.Fatalf("config changed: %q, err=%v", data, err)
	}
}

func TestRemoveOpenCodeStandardJSONWithoutInstalledCLI(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	path := manager.configPaths("opencode")[2]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcp":{"other":{"type":"remote","url":"https://example.com"},"ssh-mcp":{"type":"remote","url":"`+testServerURL+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := findTool(t, manager.Detect(), "opencode")
	if before.Installed || !before.Present || before.ConfigPath != path {
		t.Fatalf("unexpected state before removal: %#v", before)
	}
	result := manager.Remove(context.Background(), []string{"opencode"})[0]
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), serverName) || !strings.Contains(string(data), "other") {
		t.Fatalf("unexpected OpenCode config: %s", data)
	}
}

func TestUnknownAndMissingToolsAreRejected(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	results := manager.Configure(context.Background(), []string{"unknown", "zcode"})
	if len(results) != 2 || results[0].OK || results[1].OK {
		t.Fatalf("results = %#v", results)
	}
}

func TestNullJSONConfigIsRejectedWithoutModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeJSONConfig(path, []string{"mcpServers"}, map[string]any{"command": "ssh-mcp.exe"}); err == nil {
		t.Fatal("null config was accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "null\n" {
		t.Fatalf("config changed: %q, err=%v", data, err)
	}
}

func TestJSONCConfigIsRejectedWithoutLosingComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	original := []byte("{\n  // keep this comment\n  \"mcpServers\": {},\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeJSONConfig(path, []string{"mcpServers"}, map[string]any{"command": "ssh-mcp.exe"}); err == nil {
		t.Fatal("JSONC config was rewritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(data, original) {
		t.Fatalf("config changed: %q, err=%v", data, err)
	}
}

func TestSnapshotDetectsExternalUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"external\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unchanged, err := snapshot.unchanged()
	if err != nil || unchanged {
		t.Fatalf("unchanged=%v err=%v", unchanged, err)
	}
}

func findTool(t *testing.T, tools []ToolInfo, id string) ToolInfo {
	t.Helper()
	for _, tool := range tools {
		if tool.ID == id {
			return tool
		}
	}
	t.Fatalf("tool not found: %s", id)
	return ToolInfo{}
}
