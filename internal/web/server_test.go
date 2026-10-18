package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ackwrap/ssh-mcp/internal/config"
	"github.com/ackwrap/ssh-mcp/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	integrations := requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/integrations", nil, http.StatusOK)
	if len(integrations["tools"].([]any)) < 5 {
		t.Fatal("missing AI tool integrations")
	}
	assetResponse, err := client.Get(httpServer.URL + "/assets/integrations.js")
	if err != nil {
		t.Fatal(err)
	}
	assetResponse.Body.Close()
	if assetResponse.StatusCode != http.StatusOK {
		t.Fatalf("integrations.js status = %d", assetResponse.StatusCode)
	}
	requestJSON(t, client, http.MethodPost, httpServer.URL+"/api/auth/lock", map[string]any{}, http.StatusOK)
	requestJSON(t, client, http.MethodGet, httpServer.URL+"/api/servers", nil, http.StatusUnauthorized)
}

func TestStreamableHTTPMCP(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	app, err := NewServer(nil, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "web-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp/", MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 16 {
		t.Fatalf("tool count = %d", len(tools.Tools))
	}
}

func TestWebServerAvailabilityAndHTTPMCP(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	app, err := NewServer(nil, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	webClient := &http.Client{Jar: jar}
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/auth/setup", map[string]any{"password": "master123"}, http.StatusOK)
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/servers", map[string]any{
		"name": "web", "host": "192.0.2.50", "username": "root", "group": "生产环境", "disabled": true,
		"auth": map[string]any{"type": "password", "password": "secret"},
	}, http.StatusOK)
	listed := requestJSON(t, webClient, http.MethodGet, httpServer.URL+"/api/servers", nil, http.StatusOK)
	server := listed["servers"].([]any)[0].(map[string]any)
	if server["host"] != "192.0.2.50" || server["group"] != "生产环境" || server["disabled"] != true {
		t.Fatalf("WebUI cannot manage disabled server: %#v", server)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "availability-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp/", MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, disabled := range []bool{true, false, true} {
		requestJSON(t, webClient, http.MethodPut, httpServer.URL+"/api/servers/web", map[string]any{"disabled": disabled}, http.StatusOK)
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ssh_list_servers", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("list failed: %v, %#v", err, result)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "192.0.2.50") || strings.Contains(string(encoded), "secret") {
			t.Fatalf("MCP leaked address or credential: %s", encoded)
		}
		var output struct {
			Servers []config.Server `json:"servers"`
		}
		data, _ := json.Marshal(result.StructuredContent)
		if err := json.Unmarshal(data, &output); err != nil {
			t.Fatal(err)
		}
		if disabled && len(output.Servers) != 0 || !disabled && (len(output.Servers) != 1 || output.Servers[0].Group != "生产环境") {
			t.Fatalf("disabled=%v, list=%s", disabled, data)
		}
		if disabled {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ssh_read_file", Arguments: map[string]any{"server": "web", "path": "/tmp/test"}})
			if err != nil || !result.IsError || result.Content[0].(*mcp.TextContent).Text != "服务器不存在或不可用" {
				t.Fatalf("disabled server accessible: %v, %#v", err, result)
			}
		}
	}
}

func TestAddressPrivacySettingControlsMCP(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	app, err := NewServer(nil, mcpserver.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	webClient := &http.Client{Jar: jar}
	settingsURL := httpServer.URL + "/api/settings"
	requestJSON(t, webClient, http.MethodGet, settingsURL, nil, http.StatusUnauthorized)
	requestJSON(t, webClient, http.MethodPut, settingsURL, map[string]any{"hide_ip": false}, http.StatusUnauthorized)
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/auth/setup", map[string]any{"password": "master123"}, http.StatusOK)
	settings := requestJSON(t, webClient, http.MethodGet, settingsURL, nil, http.StatusOK)
	if settings["hide_ip"] != true {
		t.Fatalf("fresh settings do not hide IPs: %#v", settings)
	}
	auth := map[string]any{"type": "password", "password": "server-secret"}
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/jumphosts", map[string]any{
		"name": "jump", "host": "2001:db8::50", "username": "root", "auth": auth,
	}, http.StatusOK)
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/servers", map[string]any{
		"name": "web", "host": "192.0.2.50", "username": "root", "auth": auth, "jump_via": "jump",
		"proxy": map[string]any{"type": "http", "host": "proxy.example", "port": 8080, "password": "proxy-secret"},
	}, http.StatusOK)
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/servers", map[string]any{
		"name": "hidden-server", "host": "hidden.example", "username": "root", "auth": auth, "disabled": true,
	}, http.StatusOK)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "privacy-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp/", MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, hideIP := range []bool{true, false, true} {
		requestJSON(t, webClient, http.MethodPut, settingsURL, map[string]any{"hide_ip": hideIP}, http.StatusOK)
		settings := requestJSON(t, webClient, http.MethodGet, settingsURL, nil, http.StatusOK)
		if settings["hide_ip"] != hideIP {
			t.Fatal("setting did not persist")
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ssh_list_servers", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("list failed: %v, %#v", err, result)
		}
		encoded, _ := json.Marshal(result)
		for _, hidden := range []string{"server-secret", "proxy-secret", "hidden-server", "hidden.example"} {
			if strings.Contains(string(encoded), hidden) {
				t.Fatalf("list leaked %q: %s", hidden, encoded)
			}
		}
		for _, payload := range []any{result.StructuredContent, json.RawMessage(result.Content[0].(*mcp.TextContent).Text)} {
			data, _ := json.Marshal(payload)
			var output struct {
				Servers []config.Server   `json:"servers"`
				Jumps   []config.JumpHost `json:"jumphosts"`
			}
			if err := json.Unmarshal(data, &output); err != nil {
				t.Fatal(err)
			}
			if len(output.Servers) != 1 || output.Servers[0].Proxy == nil || len(output.Jumps) != 1 {
				t.Fatalf("unexpected list: %s", data)
			}
			for _, addresses := range [][2]string{{output.Servers[0].Host, "192.0.2.50"}, {output.Servers[0].Proxy.Host, "proxy.example"}, {output.Jumps[0].Host, "2001:db8::50"}} {
				want := addresses[1]
				if hideIP {
					want = config.Mask
				}
				if addresses[0] != want {
					t.Fatalf("hide_ip=%v: address=%q, want=%q", hideIP, addresses[0], want)
				}
			}
		}
	}
	for _, invalid := range []map[string]any{{}, {"hide_ip": nil}, {"hide_ip": "false"}, {"hide_ip": false, "extra": true}} {
		requestJSON(t, webClient, http.MethodPut, settingsURL, invalid, http.StatusBadRequest)
	}
	settings = requestJSON(t, webClient, http.MethodGet, settingsURL, nil, http.StatusOK)
	if settings["hide_ip"] != true {
		t.Fatal("invalid settings disabled address masking")
	}
	requestJSON(t, webClient, http.MethodPost, httpServer.URL+"/api/auth/lock", map[string]any{}, http.StatusOK)
	requestJSON(t, webClient, http.MethodPut, settingsURL, map[string]any{"hide_ip": false}, http.StatusUnauthorized)
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
