package sshclient

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"net/http"
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

func TestJumpChainAndProxies(t *testing.T) {
	targetPort, stopTarget := startTestSSHServer(t)
	defer stopTarget()
	jumpPort, stopJump := startTestSSHServer(t)
	defer stopJump()
	password := "test-password"
	jumpName := "jump"
	baseEndpoint := config.Endpoint{ID: "target", Name: "target", Host: "127.0.0.1", Port: targetPort, Username: "tester", Auth: config.AuthSpec{Type: "password", Password: &password}}
	opened := &config.OpenStore{Config: config.AppConfig{Version: 1, Servers: []config.Server{{Endpoint: baseEndpoint}}, JumpHosts: []config.JumpHost{{Endpoint: config.Endpoint{ID: "jump", Name: jumpName, Host: "127.0.0.1", Port: jumpPort, Username: "tester", Auth: config.AuthSpec{Type: "password", Password: &password}}}}}}
	opened.Config.Servers[0].JumpVia = &jumpName
	manager := NewManager(opened)
	server, _ := opened.GetServer("target")
	result, err := manager.Exec(server, "echo via-jump", 5*time.Second, "", nil)
	manager.CloseAll()
	if err != nil || !strings.Contains(result.Stdout, "via-jump") {
		t.Fatalf("jump exec stdout=%q err=%v", result.Stdout, err)
	}

	for _, proxyType := range []string{"socks5", "http"} {
		proxyPort, stopProxy := startTestProxy(t, proxyType)
		opened.Config.Servers[0].JumpVia = nil
		opened.Config.Servers[0].Proxy = &config.ProxySpec{Type: proxyType, Host: "127.0.0.1", Port: proxyPort}
		manager = NewManager(opened)
		result, err = manager.Exec(&opened.Config.Servers[0], "echo via-"+proxyType, 5*time.Second, "", nil)
		manager.CloseAll()
		stopProxy()
		if err != nil || !strings.Contains(result.Stdout, "via-"+proxyType) {
			t.Fatalf("%s exec stdout=%q err=%v", proxyType, result.Stdout, err)
		}
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
		if channelRequest.ChannelType() == "direct-tcpip" {
			go serveTestTunnel(channelRequest)
			continue
		}
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

func serveTestTunnel(request ssh.NewChannel) {
	var payload struct {
		DestHost   string
		DestPort   uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(request.ExtraData(), &payload); err != nil {
		_ = request.Reject(ssh.ConnectionFailed, "bad target")
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(payload.DestHost, fmt.Sprint(payload.DestPort)))
	if err != nil {
		_ = request.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, requests, err := request.Accept()
	if err != nil {
		target.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	go relayConnections(channel, target)
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

func startTestProxy(t *testing.T, proxyType string) (int, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				if proxyType == "socks5" {
					serveTestSOCKS5(conn)
				} else {
					serveTestHTTPProxy(conn)
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, func() { _ = listener.Close() }
}

func serveTestSOCKS5(conn net.Conn) {
	defer conn.Close()
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return
	}
	var host string
	switch request[3] {
	case 1:
		address := make([]byte, 4)
		if _, err := io.ReadFull(conn, address); err != nil {
			return
		}
		host = net.IP(address).String()
	case 3:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		address := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, address); err != nil {
			return
		}
		host = string(address)
	case 4:
		address := make([]byte, 16)
		if _, err := io.ReadFull(conn, address); err != nil {
			return
		}
		host = net.IP(address).String()
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])
	target, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		target.Close()
		return
	}
	relayConnections(conn, target)
}

func serveTestHTTPProxy(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	request, err := http.ReadRequest(reader)
	if err != nil || request.Method != http.MethodConnect {
		return
	}
	target, err := net.Dial("tcp", request.Host)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		target.Close()
		return
	}
	relayConnections(conn, target)
}

func relayConnections(left, right io.ReadWriteCloser) {
	defer left.Close()
	defer right.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(left, right); done <- struct{}{} }()
	go func() { _, _ = io.Copy(right, left); done <- struct{}{} }()
	<-done
}
