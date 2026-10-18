package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAllAdaptersUseIsolatedTestConfig(t *testing.T) {
	manager := testManager(t, nil, nil)
	for _, tool := range manager.Detect() {
		if tool.Installed || tool.Present || tool.Configured {
			t.Fatalf("test sandbox detected a real tool or config: %#v", tool)
		}
		for _, path := range manager.configPaths(tool.ID) {
			relative, err := filepath.Rel(manager.homeDir, path)
			if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				t.Fatalf("%s config escapes test sandbox: %q, err = %v", tool.ID, path, err)
			}
		}
	}
}

func TestClaudeConfigureFailureRestoresSandboxConfig(t *testing.T) {
	var configPath string
	var calls [][]string
	manager := testManager(t, func(name string) (string, error) {
		if name == "claude" {
			return "mock-claude", nil
		}
		return "", errors.New("not found")
	}, func(_ context.Context, command string, args ...string) (string, error) {
		if command != "mock-claude" {
			t.Fatalf("unexpected CLI: %q", command)
		}
		calls = append(calls, append([]string(nil), args...))
		if err := os.WriteFile(configPath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if args[1] == "add" {
			return "mock add failed", errors.New("exit 1")
		}
		return "ok", nil
	})
	configPath = manager.configPaths("claude-code")[0]
	original := []byte(`{"mcpServers":{"other":{"command":"other"}},"other_setting":true}`)
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Configure(context.Background(), []string{"claude-code"})[0]
	if result.OK || !strings.Contains(result.Message, "原配置已恢复") {
		t.Fatalf("configure result = %#v", result)
	}
	want := [][]string{
		{"mcp", "remove", "--scope", "user", serverName},
		{"mcp", "add", "--scope", "user", serverName, "--", manager.serverCommand[0]},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("mock CLI calls = %#v, want %#v", calls, want)
	}
	for _, path := range []string{configPath, result.BackupPath} {
		data, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(data, original) {
			t.Fatalf("sandbox config or backup changed: %q, err = %v", data, err)
		}
	}
}
