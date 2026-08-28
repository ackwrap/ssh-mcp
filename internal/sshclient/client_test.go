package sshclient

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/ssh-mcp/ssh-mcp-go/internal/config"
	"golang.org/x/crypto/ssh"
)

func TestExecAndSFTP(t *testing.T) {
	port, stop := startTestSSHServer(t)
	defer stop()
	password := "test-password"
	opened := &config.OpenStore{Config: config.AppConfig{Version: 1, Servers: []config.Server{{Endpoint: config.Endpoint{ID: "test", Name: "local", Host: "127.0.0.1", Port: port, Username: "tester", Auth: config.AuthSpec{Type: "password", Password: &password}}}}, JumpHosts: []config.JumpHost{}}}
	manager := NewManager(opened)
	defer manager.CloseAll()
	server, _ := opened.GetServer("local")

	result, err := manager.Exec(server, "echo hello-ssh", 5*time.Second, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "hello-ssh") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	ok, message := manager.Test(server, 5*time.Second)
	if !ok {
		t.Fatalf("connection test: %s", message)
	}

	remote := filepath.Join(t.TempDir(), "remote.txt")
	if _, err := manager.WriteFile(server, remote, "SFTP 内容"); err != nil {
		t.Fatal(err)
	}
	content, err := manager.ReadFile(server, remote, 1024)
	if err != nil || content != "SFTP 内容" {
		t.Fatalf("read %q: %v", content, err)
	}
	items, err := manager.ListDir(server, filepath.Dir(remote))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item["name"] == "remote.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("directory items = %#v", items)
	}
	stat, err := manager.Stat(server, remote)
	if err != nil {
		t.Fatal(err)
	}
	if stat["size"] != int64(len([]byte("SFTP 内容"))) {
		t.Fatalf("stat = %#v", stat)
	}
}

func startTestSSHServer(t *testing.T) (int, func()) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if metadata.User() == "tester" && string(password) == "test-password" {
			return nil, nil
		}
		return nil, fmt.Errorf("authentication failed")
	}}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var connections sync.WaitGroup
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() { defer connections.Done(); serveTestSSH(conn, serverConfig) }()
		}
	}()
	stop := func() { _ = listener.Close(); connections.Wait() }
	return listener.Addr().(*net.TCPAddr).Port, stop
}

func serveTestSSH(raw net.Conn, serverConfig *ssh.ServerConfig) {
	connection, channels, requests, err := ssh.NewServerConn(raw, serverConfig)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer connection.Close()
	go ssh.DiscardRequests(requests)
	for channelRequest := range channels {
		if channelRequest.ChannelType() != "session" {
			_ = channelRequest.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := channelRequest.Accept()
		if err != nil {
			continue
		}
		go serveTestSession(channel, requests)
	}
}

func serveTestSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "subsystem":
			var payload struct{ Name string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			if payload.Name != "sftp" {
				_ = request.Reply(false, nil)
				continue
			}
			_ = request.Reply(true, nil)
			server, err := sftp.NewServer(channel)
			if err == nil {
				_ = server.Serve()
				_ = server.Close()
			}
			return
		case "exec":
			var payload struct{ Command string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			_ = request.Reply(true, nil)
			output := ""
			if strings.Contains(payload.Command, "ssh_mcp_ok") {
				output = "ssh_mcp_ok\n"
			} else if index := strings.Index(payload.Command, "echo "); index >= 0 {
				output = strings.TrimSpace(payload.Command[index+5:]) + "\n"
			}
			_, _ = io.WriteString(channel, output)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}
