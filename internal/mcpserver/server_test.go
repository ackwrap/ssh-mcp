package mcpserver

import (
	"context"
	"sort"
	"testing"

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
