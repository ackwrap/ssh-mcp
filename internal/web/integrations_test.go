package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ssh-mcp/ssh-mcp-go/internal/integration"
	"github.com/ssh-mcp/ssh-mcp-go/internal/mcpserver"
)

func TestNewIntegrationManagerUsesConfiguredPort(t *testing.T) {
	t.Setenv("SSH_MCP_PORT", "18878")
	manager, err := newIntegrationManager()
	if err != nil {
		t.Fatal(err)
	}
	if manager.ServerURL() != "http://127.0.0.1:18878/mcp/" {
		t.Fatalf("server URL = %q", manager.ServerURL())
	}
	if strings.Contains(strings.Join(manager.ServerCommand(), " "), "--mcp") {
		t.Fatalf("new stdio fallback should use the default mode: %#v", manager.ServerCommand())
	}
}

func TestIntegrationAPIRequiresAuthAndUsesAllowlistedCommand(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	app, err := NewServer(nil, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	manager, err := integration.NewManager(integration.Options{
		ServerCommand: []string{`C:\Program Files\SSH-MCP\ssh-mcp.exe`},
		ServerURL:     "http://127.0.0.1:18877/mcp/",
		HomeDir:       filepath.Join(dataDir, "home"),
		AppData:       filepath.Join(dataDir, "appdata"),
		LocalAppData:  filepath.Join(dataDir, "localappdata"),
		Getenv:        func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "opencode" {
				return `C:\bin\opencode.exe`, nil
			}
			return "", errors.New("not found")
		},
		Run: func(ctx context.Context, command string, args ...string) (string, error) {
			calls = append(calls, append([]string{command}, args...))
			return "ok", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.integrations = manager
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/integrations", nil, http.StatusUnauthorized)
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/configure", map[string]any{"tools": []string{"opencode"}}, http.StatusUnauthorized)
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/auth/setup", map[string]any{"password": "master123"}, http.StatusOK)
	detected := requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/integrations", nil, http.StatusOK)
	if detected["endpoint"] != "http://127.0.0.1:18877/mcp/" {
		t.Fatalf("endpoint = %#v", detected["endpoint"])
	}
	tools := detected["tools"].([]any)
	if !tools[0].(map[string]any)["installed"].(bool) {
		t.Fatal("OpenCode was not detected")
	}
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/configure", map[string]any{"tools": []string{"opencode"}, "command": "evil.exe"}, http.StatusBadRequest)
	if len(calls) != 0 {
		t.Fatalf("unknown request field executed command: %#v", calls)
	}
	configured := requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/configure", map[string]any{"tools": []string{"opencode"}}, http.StatusOK)
	results := configured["results"].([]any)
	if !results[0].(map[string]any)["ok"].(bool) {
		t.Fatalf("result = %#v", results[0])
	}
	want := []string{`C:\bin\opencode.exe`, "mcp", "add", "ssh-mcp", "--", `C:\Program Files\SSH-MCP\ssh-mcp.exe`}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRemoveIntegrationAPIRequiresAuthAndPreservesOtherConfig(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	home := filepath.Join(dataDir, "home")
	app, err := NewServer(nil, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := integration.NewManager(integration.Options{
		ServerCommand: []string{`C:\Program Files\SSH-MCP\ssh-mcp.exe`},
		ServerURL:     "http://127.0.0.1:18877/mcp/",
		HomeDir:       home,
		AppData:       filepath.Join(dataDir, "appdata"),
		LocalAppData:  filepath.Join(dataDir, "localappdata"),
		Getenv:        func(string) string { return "" },
		LookPath:      func(string) (string, error) { return "", errors.New("not found") },
	})
	if err != nil {
		t.Fatal(err)
	}
	app.integrations = manager
	configPath := filepath.Join(home, ".kimi", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"other":{"command":"other"},"ssh-mcp":{"url":"http://127.0.0.1:8877/mcp/"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/remove", map[string]any{"tools": []string{"kimi-code"}}, http.StatusUnauthorized)
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/auth/setup", map[string]any{"password": "master123"}, http.StatusOK)
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/remove", map[string]any{"tools": []string{"kimi-code"}, "command": "evil.exe"}, http.StatusBadRequest)
	removed := requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/remove", map[string]any{"tools": []string{"kimi-code"}}, http.StatusOK)
	if !removed["results"].([]any)[0].(map[string]any)["ok"].(bool) {
		t.Fatalf("result = %#v", removed)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ssh-mcp") || !strings.Contains(string(data), "other") {
		t.Fatalf("unexpected config after removal: %s", data)
	}
}

func TestIntegrationAPIRedactsCLIErrorOutput(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	app, err := NewServer(nil, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := integration.NewManager(integration.Options{
		ServerCommand: []string{`C:\Program Files\SSH-MCP\ssh-mcp.exe`},
		ServerURL:     "http://127.0.0.1:18877/mcp/",
		HomeDir:       filepath.Join(dataDir, "home"),
		AppData:       filepath.Join(dataDir, "appdata"),
		LocalAppData:  filepath.Join(dataDir, "localappdata"),
		Getenv:        func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "opencode" {
				return `C:\bin\opencode.exe`, nil
			}
			return "", errors.New("not found")
		},
		Run: func(context.Context, string, ...string) (string, error) {
			return "Invalid URL option\n{'Authorization': 'Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ=='}", errors.New("exit status 1")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.integrations = manager
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/auth/setup", map[string]any{"password": "master123"}, http.StatusOK)
	configured := requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/integrations/configure", map[string]any{"tools": []string{"opencode"}}, http.StatusOK)
	message := configured["results"].([]any)[0].(map[string]any)["message"].(string)
	if !strings.Contains(message, "Invalid URL option") || strings.Contains(message, "QWxhZGRpbjpvcGVuIHNlc2FtZQ==") {
		t.Fatalf("unsafe API error message: %q", message)
	}
}
