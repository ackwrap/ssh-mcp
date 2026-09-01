package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	content := `{
  // Existing user comment
  "mcp": {
    "ssh-mcp": {"type": "remote", "url": "` + testServerURL + `",},
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := manager.Detect()
	openCode := findTool(t, tools, "opencode")
	if !openCode.Installed || !openCode.Configured || openCode.ConfigPath != path {
		t.Fatalf("OpenCode detection = %#v", openCode)
	}
	if findTool(t, tools, "codex").Installed {
		t.Fatal("Codex should not be installed")
	}
	content = `{"mcp":{"ssh-mcp":{"type":"local","command":["old.exe","--mcp"]}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if findTool(t, manager.Detect(), "opencode").Configured {
		t.Fatal("legacy stdio OpenCode config should be upgraded to HTTP")
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

func TestCodexConfiguredRequiresCurrentHTTPURL(t *testing.T) {
	current := []byte("[mcp_servers.ssh-mcp] # SSH-MCP\nurl = \"" + testServerURL + "\" # local endpoint\n")
	if !codexHTTPConfigured(current, testServerURL) {
		t.Fatal("current Codex HTTP config was not detected")
	}
	legacy := []byte("[mcp_servers.ssh-mcp]\ncommand = \"ssh-mcp.exe\"\nargs = [\"--mcp\"]\n")
	if codexHTTPConfigured(legacy, testServerURL) {
		t.Fatal("legacy Codex stdio config should be upgraded to HTTP")
	}
}

func TestDirectConfigDetectionRequiresMatchingTransport(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	tests := []struct {
		id       string
		matching map[string]any
		stale    map[string]any
	}{
		{id: "kimi-code", matching: map[string]any{"transport": "http", "url": testServerURL}, stale: map[string]any{"transport": "stdio", "url": testServerURL}},
		{id: "zcode", matching: map[string]any{"type": "http", "url": testServerURL}, stale: map[string]any{"type": "sse", "url": testServerURL}},
		{id: "vscode", matching: map[string]any{"type": "http", "url": testServerURL}, stale: map[string]any{"type": "stdio", "url": testServerURL}},
		{id: "cursor", matching: map[string]any{"url": testServerURL}, stale: map[string]any{"url": "http://127.0.0.1:9999/mcp/"}},
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
	if sshMCP["url"] != testServerURL || sshMCP["transport"] != "http" {
		t.Fatalf("ssh-mcp entry = %#v", sshMCP)
	}
}

func TestDirectConfigAdaptersWriteExpectedRootShape(t *testing.T) {
	tests := []struct {
		id       string
		command  string
		rootPath []string
		typeHTTP bool
		stdio    bool
	}{
		{id: "zcode", command: "zcode", rootPath: []string{"mcp", "servers"}, typeHTTP: true},
		{id: "cursor", command: "cursor", rootPath: []string{"mcpServers"}},
		{id: "vscode", command: "code", rootPath: []string{"servers"}, typeHTTP: true},
		{id: "claude-desktop", command: "claude-desktop", rootPath: []string{"mcpServers"}, stdio: true},
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
			if test.stdio {
				if entry["command"] != manager.serverCommand[0] {
					t.Fatalf("stdio entry = %#v", entry)
				}
				if _, exists := entry["args"]; exists {
					t.Fatalf("default stdio entry should not include args: %#v", entry)
				}
			} else if entry["url"] != testServerURL {
				t.Fatalf("HTTP entry = %#v", entry)
			}
			if test.typeHTTP && entry["type"] != "http" {
				t.Fatalf("typed HTTP entry = %#v", entry)
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
		{"mcp", "add", serverName, "--url", testServerURL},
		{"mcp", "remove", "--scope", "user", serverName},
		{"mcp", "add", "--transport", "http", "--scope", "user", serverName, testServerURL},
		{"mcp", "remove", serverName},
		{"mcp", "add", serverName, "--url", testServerURL},
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
	content := []byte(`{"mcp":{"ssh-mcp":{"type":"remote","url":"` + testServerURL + `"}}}`)
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
