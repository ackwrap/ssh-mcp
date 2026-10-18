package mcpserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ackwrap/ssh-mcp/internal/config"
	"github.com/ackwrap/ssh-mcp/internal/secure"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(NewRuntime()).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	want := []string{"ssh_add_jumphost", "ssh_add_server", "ssh_delete_jumphost", "ssh_delete_server", "ssh_download", "ssh_exec", "ssh_exec_multi", "ssh_list_dir", "ssh_list_servers", "ssh_read_file", "ssh_stat", "ssh_test_connection", "ssh_update_jumphost", "ssh_update_server", "ssh_upload", "ssh_write_file"}
	if len(got) != len(want) {
		t.Fatalf("tools = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tools = %v", got)
		}
	}
}

func TestServerVisibilityAndAccess(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	store, err := config.NewStore("", "")
	if err != nil {
		t.Fatal(err)
	}
	password, privateKey, passphrase := "server-secret", "private-key-secret", "passphrase-secret"
	proxyPassword, proxyUser, jumpName := "proxy-secret", "proxy-user", "bastion"
	cfg := config.NewAppConfig()
	cfg.Servers = []config.Server{
		{Endpoint: config.Endpoint{Name: "active", Host: "192.0.2.40", Port: 22, Username: "root", Auth: config.AuthSpec{Type: "password", Password: &password}, JumpVia: &jumpName}, Group: "production", Proxy: &config.ProxySpec{Type: "socks5", Host: "proxy.example", Port: 1080, Username: &proxyUser, Password: &proxyPassword}},
		{Endpoint: config.Endpoint{Name: "hidden-server", Host: "hidden.example", Port: 22, Username: "hidden-user", Auth: config.AuthSpec{Type: "password", Password: &password}}, Group: "hidden-group", Disabled: true},
	}
	cfg.JumpHosts = []config.JumpHost{{Endpoint: config.Endpoint{Name: jumpName, Host: "2001:db8::40", Port: 22, Username: "root", Auth: config.AuthSpec{Type: "key_data", KeyData: &privateKey, Passphrase: &passphrase}}}}
	key, err := store.SaveEncrypted(cfg, "master123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := secure.WriteKeyFile(key); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime := NewRuntime()
	defer runtime.Reset()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(runtime).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "visibility-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	listed := call("ssh_list_servers", map[string]any{})
	if listed.IsError {
		t.Fatalf("list failed: %#v", listed)
	}
	// Inspect both text and structured responses: neither may expose addresses or hidden servers.
	encoded, _ := json.Marshal(listed)
	for _, hidden := range []string{"192.0.2.40", "2001:db8::40", "proxy.example", "hidden.example", "hidden-server", "hidden-user", "hidden-group", password, privateKey, passphrase, proxyPassword} {
		if strings.Contains(string(encoded), hidden) {
			t.Fatalf("list leaked %q: %s", hidden, encoded)
		}
	}
	var output struct {
		Servers   []config.Server   `json:"servers"`
		JumpHosts []config.JumpHost `json:"jumphosts"`
	}
	structured, _ := json.Marshal(listed.StructuredContent)
	if err := json.Unmarshal(structured, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Servers) != 1 || output.Servers[0].Name != "active" || output.Servers[0].Group != "production" || output.Servers[0].Host != config.Mask || output.Servers[0].Proxy.Host != config.Mask || len(output.JumpHosts) != 1 || output.JumpHosts[0].Host != config.Mask {
		t.Fatalf("unexpected list: %s", structured)
	}
	for _, name := range []string{"hidden-server", "nonexistent"} {
		for _, tool := range []struct {
			name string
			args map[string]any
		}{
			{"ssh_exec", map[string]any{"server": name, "command": "echo test"}},
			{"ssh_exec_multi", map[string]any{"servers": []string{"active", name}, "command": "echo test"}},
			{"ssh_test_connection", map[string]any{"server": name}},
			{"ssh_upload", map[string]any{"server": name, "local_path": "missing", "remote_path": "/tmp/test"}},
			{"ssh_download", map[string]any{"server": name, "remote_path": "/tmp/test", "local_path": "missing"}},
			{"ssh_read_file", map[string]any{"server": name, "path": "/tmp/test"}},
			{"ssh_write_file", map[string]any{"server": name, "path": "/tmp/test", "content": "test"}},
			{"ssh_list_dir", map[string]any{"server": name}},
			{"ssh_stat", map[string]any{"server": name, "path": "/tmp/test"}},
			{"ssh_update_server", map[string]any{"name": name, "changes": map[string]any{"disabled": false, "name": "renamed"}}},
			{"ssh_delete_server", map[string]any{"name": name}},
		} {
			t.Run(name+"/"+tool.name, func(t *testing.T) {
				result := call(tool.name, tool.args)
				encoded, _ := json.Marshal(result)
				if !result.IsError || len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != "服务器不存在或不可用" {
					t.Fatalf("tool did not deny access: %s", encoded)
				}
			})
		}
	}
	for _, disabled := range []any{true, false, nil} {
		result := call("ssh_update_server", map[string]any{"name": "active", "changes": map[string]any{"disabled": disabled}})
		if !result.IsError {
			t.Fatal("MCP allowed changing the availability flag")
		}
		result = call("ssh_add_server", map[string]any{"config": map[string]any{"disabled": disabled}})
		if !result.IsError {
			t.Fatal("MCP allowed setting the availability flag")
		}
	}
	if result := call("ssh_update_server", map[string]any{"name": "active", "changes": map[string]any{"group": "testing"}}); result.IsError {
		t.Fatal("active server group could not be updated")
	}
	// The same MCP session must see a local disable and re-enable without reconnecting.
	if _, err := runtime.Manager(); err != nil {
		t.Fatal(err)
	}
	opened, err := config.Open(store, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.UpdateServer("active", map[string]any{"disabled": true}); err != nil {
		t.Fatal(err)
	}
	listed = call("ssh_list_servers", map[string]any{})
	structured, _ = json.Marshal(listed.StructuredContent)
	if listed.IsError || json.Unmarshal(structured, &output) != nil || len(output.Servers) != 0 {
		t.Fatalf("disabled servers remain visible: %s", structured)
	}
	if result := call("ssh_read_file", map[string]any{"server": "active", "path": "/tmp/test"}); !result.IsError || result.Content[0].(*mcp.TextContent).Text != "服务器不存在或不可用" {
		t.Fatal("existing runtime bypassed the disabled check")
	}
	if _, err := opened.UpdateServer("active", map[string]any{"disabled": false}); err != nil {
		t.Fatal(err)
	}
	listed = call("ssh_list_servers", map[string]any{})
	structured, _ = json.Marshal(listed.StructuredContent)
	if listed.IsError || json.Unmarshal(structured, &output) != nil || len(output.Servers) != 1 || output.Servers[0].Group != "testing" {
		t.Fatalf("re-enabled server missing: %s", structured)
	}
	stored, err := config.Open(store, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Config.Servers) != 2 || stored.Config.Servers[0].Host != cfg.Servers[0].Host || !stored.Config.Servers[1].Disabled {
		t.Fatal("MCP calls changed hidden data or real addresses")
	}
}
