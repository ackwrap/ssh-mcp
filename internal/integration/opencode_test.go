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

func TestDetectOpenCodeV2Config(t *testing.T) {
	tests := []struct {
		name       string
		nested     bool
		legacy     bool
		stale      bool
		configured bool
	}{
		{name: "v2", nested: true, configured: true},
		{name: "legacy alongside other v2 servers", legacy: true, configured: true},
		{name: "v2 overrides matching legacy entry", nested: true, legacy: true, stale: true},
		{name: "v2 stale command", nested: true, stale: true},
		{name: "other servers only"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := testManager(t, func(string) (string, error) { return "opencode", nil }, nil)
			entry := map[string]any{"type": "local", "command": manager.serverCommand}
			servers := map[string]any{"other": map[string]any{"type": "local", "command": []string{"other"}}}
			mcp := map[string]any{"servers": servers}
			if test.legacy {
				mcp[serverName] = entry
			}
			if test.nested {
				if test.stale {
					servers[serverName] = map[string]any{"type": "local", "command": []string{"old-ssh-mcp"}}
				} else {
					servers[serverName] = entry
				}
			}
			data, err := json.Marshal(map[string]any{"mcp": mcp})
			if err != nil {
				t.Fatal(err)
			}
			path := manager.configPaths("opencode")[1]
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			// OpenCode also accepts JSONC for its global configuration.
			data = append([]byte("// Global OpenCode configuration\n"), data...)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			tool := findTool(t, manager.Detect(), "opencode")
			if !tool.Installed || tool.Present != (test.nested || test.legacy) || tool.Configured != test.configured || tool.ConfigPath != path {
				t.Fatalf("OpenCode detection = %#v", tool)
			}
		})
	}
}

func TestConfigureOpenCodeGlobalV2Lifecycle(t *testing.T) {
	var configPath string
	var serverCommand []string
	calls := 0
	manager := testManager(t, func(name string) (string, error) {
		if name == "opencode" {
			return "opencode", nil
		}
		return "", errors.New("not found")
	}, func(_ context.Context, command string, args ...string) (string, error) {
		calls++
		want := append([]string{"mcp", "add", serverName, "--global", "--"}, serverCommand...)
		if command != "opencode" || !reflect.DeepEqual(args, want) {
			t.Fatalf("CLI call = %q %q, want opencode %q", command, args, want)
		}
		root, err := readJSONObject(configPath)
		if err != nil {
			t.Fatal(err)
		}
		servers, ok := objectAtPath(root, []string{"mcp", "servers"})
		if !ok {
			t.Fatal("missing V2 servers object")
		}
		servers[serverName] = map[string]any{"type": "local", "command": serverCommand}
		data, err := json.Marshal(root)
		if err != nil {
			t.Fatal(err)
		}
		return "ok", os.WriteFile(configPath, data, 0o600)
	})
	configPath = manager.configPaths("opencode")[0]
	serverCommand = manager.ServerCommand()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcp":{"servers":{"other":{"type":"local","command":["other"]}},"timeout":{"startup":45000}},"other_setting":true}`)
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Configure(context.Background(), []string{"opencode"})[0]
	if !result.OK || result.ConfigPath != configPath || calls != 1 {
		t.Fatalf("configure result = %#v, calls = %d", result, calls)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %q, err = %v", backup, err)
	}
	tool := findTool(t, manager.Detect(), "opencode")
	if !tool.Present || !tool.Configured {
		t.Fatalf("configured V2 entry not detected: %#v", tool)
	}
	result = manager.Configure(context.Background(), []string{"opencode"})[0]
	if !result.OK || !strings.Contains(result.Message, "已跳过") || calls != 1 {
		t.Fatalf("repeat configure result = %#v, calls = %d", result, calls)
	}
	result = manager.Remove(context.Background(), []string{"opencode"})[0]
	if !result.OK || calls != 1 {
		t.Fatalf("remove result = %#v, calls = %d", result, calls)
	}
	root, err := readJSONObject(configPath)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := decodeJSONObject(original)
	if err != nil || !reflect.DeepEqual(root, wantRoot) {
		t.Fatalf("unrelated config changed: %#v, err = %v", root, err)
	}
	if tool := findTool(t, manager.Detect(), "opencode"); tool.Present || tool.Configured {
		t.Fatalf("removed V2 entry still detected: %#v", tool)
	}
}

func TestRemoveOpenCodeMixedConfigPreservesOtherEntries(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	path := manager.configPaths("opencode")[0]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"mcp":{"servers":{"ssh-mcp":{"type":"local","command":["ssh-mcp"]},"v2-other":{"type":"local","command":["other"]}},"ssh-mcp":{"type":"local","command":["old-ssh-mcp"]},"legacy-other":{"type":"local","command":["legacy"]},"timeout":{"startup":45000}},"other_setting":true}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Remove(context.Background(), []string{"opencode"})[0]
	if !result.OK || result.BackupPath == "" {
		t.Fatalf("remove result = %#v", result)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %q, err = %v", backup, err)
	}
	root, err := readJSONObject(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := decodeJSONObject(original)
	if err != nil {
		t.Fatal(err)
	}
	mcp := want["mcp"].(map[string]any)
	delete(mcp, serverName)
	delete(mcp["servers"].(map[string]any), serverName)
	if !reflect.DeepEqual(root, want) {
		t.Fatalf("unrelated config changed: %#v, want %#v", root, want)
	}
}

func TestRemoveOpenCodeV2JSONCPreservesComments(t *testing.T) {
	manager := testManager(t, func(string) (string, error) { return "", errors.New("not found") }, nil)
	path := manager.configPaths("opencode")[1]
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // keep\n  \"mcp\": {\"servers\": {\"ssh-mcp\": {\"type\": \"local\", \"command\": [\"ssh-mcp\"]}}},\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	result := manager.Remove(context.Background(), []string{"opencode"})[0]
	if result.OK || !strings.Contains(result.Message, "注释或尾逗号") {
		t.Fatalf("remove result = %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(data, original) {
		t.Fatalf("config changed: %q, err = %v", data, err)
	}
}
