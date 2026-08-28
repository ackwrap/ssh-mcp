package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/ssh-mcp/ssh-mcp-go/internal/config"
	"github.com/ssh-mcp/ssh-mcp-go/internal/mcpserver"
)

func TestAuthAndServerFlow(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	store, err := config.NewStore("", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewServer(store, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	status := requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/auth/status", nil, http.StatusOK)
	if status["configured"] != false || status["authenticated"] != false {
		t.Fatalf("initial status = %#v", status)
	}
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/auth/setup", map[string]any{"password": "master123"}, http.StatusOK)
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/servers", map[string]any{"name": "web", "host": "host", "username": "root", "auth": map[string]any{"type": "password", "password": "secret"}}, http.StatusOK)
	listed := requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/servers", nil, http.StatusOK)
	servers := listed["servers"].([]any)
	auth := servers[0].(map[string]any)["auth"].(map[string]any)
	if auth["password"] != config.Mask {
		t.Fatalf("password was not masked: %#v", auth)
	}
	mcpConfig := requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/mcp-configs", nil, http.StatusOK)
	httpConfig := mcpConfig["http"].(map[string]any)["mcpServers"].(map[string]any)["ssh-mcp"].(map[string]any)
	if httpConfig["url"] == nil {
		t.Fatal("missing HTTP MCP URL")
	}
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/auth/lock", map[string]any{}, http.StatusOK)
	requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/servers", nil, http.StatusUnauthorized)
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body any, status int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	request, _ := http.NewRequest(method, url, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != status {
		t.Fatalf("%s %s: status %d, body %s", method, url, response.StatusCode, data)
	}
	result := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
	}
	return result
}
