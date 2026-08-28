package sshclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/ssh-mcp/ssh-mcp-go/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	ConnectTimeout   = 15 * time.Second
	PoolTTL          = 5 * time.Minute
	MaxCommandOutput = 4 * 1024 * 1024
	MaxFileBytes     = 4 * 1024 * 1024
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

type CommandError struct {
	Message  string
	Stdout   string
	Stderr   string
	ExitCode *int
}

func (e *CommandError) Error() string { return e.Message }

type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type pooledConn struct {
	target  *ssh.Client
	clients []*ssh.Client
	last    time.Time
	done    chan struct{}
}

type Manager struct {
	store      *config.OpenStore
	poolTTL    time.Duration
	strict     bool
	knownHosts string

	mu         sync.Mutex
	pool       map[string]*pooledConn
	keyLocks   map[string]*sync.Mutex
	generation uint64
	closing    bool
}

func NewManager(store *config.OpenStore) *Manager {
	return &Manager{
		store: store, poolTTL: PoolTTL, strict: envBool("SSH_MCP_STRICT_HOST_KEY", false),
		knownHosts: os.Getenv("SSH_MCP_KNOWN_HOSTS"), pool: map[string]*pooledConn{}, keyLocks: map[string]*sync.Mutex{},
	}
}

func (m *Manager) SetStore(store *config.OpenStore) {
	m.mu.Lock()
	m.store = store
	m.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	m.closing = true
	m.generation++
	locks := make([]*sync.Mutex, 0, len(m.keyLocks))
	for _, lock := range m.keyLocks {
		locks = append(locks, lock)
	}
	m.mu.Unlock()
	for _, lock := range locks {
		lock.Lock()
	}
	m.mu.Lock()
	connections := make([]*pooledConn, 0, len(m.pool))
	for _, conn := range m.pool {
		connections = append(connections, conn)
	}
	m.pool = map[string]*pooledConn{}
	m.mu.Unlock()
	for _, conn := range connections {
		closePooled(conn)
	}
	for i := len(locks) - 1; i >= 0; i-- {
		locks[i].Unlock()
	}
	m.mu.Lock()
	m.closing = false
	m.mu.Unlock()
}

func closePooled(conn *pooledConn) {
	for i := len(conn.clients) - 1; i >= 0; i-- {
		_ = conn.clients[i].Close()
	}
}

func (m *Manager) connectionFingerprint(server *config.Server) (string, error) {
	chain, err := m.chain(server)
	if err != nil {
		return "", err
	}
	payload := struct {
		Server *config.Server    `json:"server"`
		Jumps  []config.JumpHost `json:"jumphosts"`
	}{server, chain}
	data, _ := json.Marshal(payload)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (m *Manager) chain(server *config.Server) ([]config.JumpHost, error) {
	m.mu.Lock()
	jumpHosts := append([]config.JumpHost(nil), m.store.Config.JumpHosts...)
	m.mu.Unlock()
	chain := []config.JumpHost{}
	current := server.JumpVia
	for current != nil {
		var found *config.JumpHost
		for i := range jumpHosts {
			if jumpHosts[i].Name == *current {
				found = &jumpHosts[i]
				break
			}
		}
		if found == nil {
			return nil, &Error{Message: "跳板机不存在: " + *current}
		}
		chain = append(chain, *found)
		current = found.JumpVia
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain, nil
}

func (m *Manager) GetClient(server *config.Server) (*ssh.Client, error) {
	fingerprint, err := m.connectionFingerprint(server)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	keyLock := m.keyLocks[fingerprint]
	if keyLock == nil {
		keyLock = &sync.Mutex{}
		m.keyLocks[fingerprint] = keyLock
	}
	m.mu.Unlock()
	keyLock.Lock()
	defer keyLock.Unlock()

	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return nil, &Error{Message: "连接池正在关闭"}
	}
	if existing := m.pool[fingerprint]; existing != nil {
		if time.Since(existing.last) <= m.poolTTL {
			select {
			case <-existing.done:
			default:
				existing.last = time.Now()
				m.mu.Unlock()
				return existing.target, nil
			}
		}
		delete(m.pool, fingerprint)
		m.mu.Unlock()
		closePooled(existing)
		m.mu.Lock()
	}
	generation := m.generation
	m.mu.Unlock()

	created, err := m.connect(server)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	discard := m.closing || generation != m.generation
	if !discard {
		m.pool[fingerprint] = created
	}
	m.mu.Unlock()
	if discard {
		closePooled(created)
		return nil, &Error{Message: "连接池在连接建立期间已关闭"}
	}
	return created.target, nil
}

func (m *Manager) connect(server *config.Server) (*pooledConn, error) {
	chain, err := m.chain(server)
	if err != nil {
		return nil, err
	}
	clients := []*ssh.Client{}
	cleanup := func() {
		for i := len(clients) - 1; i >= 0; i-- {
			_ = clients[i].Close()
		}
	}

	var raw net.Conn
	if len(chain) > 0 {
		first := chain[0]
		raw, err = m.dialFirst(server.Proxy, first.Host, first.Port)
		if err == nil {
			var client *ssh.Client
			client, err = m.newSSHClient(raw, first.Endpoint)
			if err == nil {
				clients = append(clients, client)
			}
		}
		if err != nil {
			if raw != nil {
				raw.Close()
			}
			cleanup()
			return nil, err
		}
		for _, jump := range chain[1:] {
			raw, err = clients[len(clients)-1].Dial("tcp", endpointAddress(jump.Host, jump.Port))
			if err != nil {
				cleanup()
				return nil, &Error{Message: fmt.Sprintf("经跳板转发到 %s:%d 失败: %v", jump.Host, jump.Port, err)}
			}
			client, connectErr := m.newSSHClient(raw, jump.Endpoint)
			if connectErr != nil {
				raw.Close()
				cleanup()
				return nil, connectErr
			}
			clients = append(clients, client)
		}
		raw, err = clients[len(clients)-1].Dial("tcp", endpointAddress(server.Host, server.Port))
		if err != nil {
			cleanup()
			return nil, &Error{Message: fmt.Sprintf("经跳板转发到 %s:%d 失败: %v", server.Host, server.Port, err)}
		}
	} else {
		raw, err = m.dialFirst(server.Proxy, server.Host, server.Port)
		if err != nil {
			return nil, err
		}
	}
	target, err := m.newSSHClient(raw, server.Endpoint)
	if err != nil {
		raw.Close()
		cleanup()
		return nil, err
	}
	clients = append(clients, target)
	pooled := &pooledConn{target: target, clients: clients, last: time.Now(), done: make(chan struct{})}
	go func() {
		_ = target.Wait()
		close(pooled.done)
	}()
	return pooled, nil
}

func (m *Manager) dialFirst(proxy *config.ProxySpec, host string, port int) (net.Conn, error) {
	if proxy != nil {
		return dialProxy(proxy, host, port, ConnectTimeout)
	}
	conn, err := net.DialTimeout("tcp", endpointAddress(host, port), ConnectTimeout)
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("连接 %s:%d 失败: %v", host, port, err)}
	}
	return conn, nil
}

func (m *Manager) newSSHClient(conn net.Conn, endpoint config.Endpoint) (*ssh.Client, error) {
	auth, err := authMethod(endpoint.Auth)
	if err != nil {
		return nil, err
	}
	hostKey, err := m.hostKeyCallback()
	if err != nil {
		return nil, err
	}
	clientConfig := &ssh.ClientConfig{User: endpoint.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: hostKey, Timeout: ConnectTimeout}
	_ = conn.SetDeadline(time.Now().Add(ConnectTimeout))
	clientConn, channels, requests, err := ssh.NewClientConn(conn, endpointAddress(endpoint.Host, endpoint.Port), clientConfig)
	_ = conn.SetDeadline(time.Time{})
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("连接 %s:%d 失败: %v", endpoint.Host, endpoint.Port, err)}
	}
	return ssh.NewClient(clientConn, channels, requests), nil
}

func (m *Manager) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if !m.strict {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	paths := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		defaultPath := filepath.Join(home, ".ssh", "known_hosts")
		if _, err := os.Stat(defaultPath); err == nil {
			paths = append(paths, defaultPath)
		}
	}
	if m.knownHosts != "" {
		paths = append(paths, m.knownHosts)
	}
	if len(paths) == 0 {
		return nil, &Error{Message: "严格主机密钥验证已启用，但未找到 known_hosts 文件"}
	}
	callback, err := knownhosts.New(paths...)
	if err != nil {
		return nil, &Error{Message: "加载 known_hosts 失败: " + err.Error()}
	}
	return callback, nil
}

func authMethod(auth config.AuthSpec) (ssh.AuthMethod, error) {
	if auth.Type == "password" {
		return ssh.Password(config.String(auth.Password)), nil
	}
	var keyData []byte
	var err error
	if auth.Type == "key_data" {
		keyData = []byte(config.String(auth.KeyData))
	} else {
		keyData, err = os.ReadFile(config.String(auth.KeyPath))
	}
	if err != nil {
		return nil, &Error{Message: "无法读取私钥文件: " + err.Error()}
	}
	var signer ssh.Signer
	if auth.Passphrase != nil && *auth.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(*auth.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyData)
	}
	if err != nil {
		return nil, &Error{Message: "私钥解析失败: " + err.Error()}
	}
	return ssh.PublicKeys(signer), nil
}

func (m *Manager) Exec(server *config.Server, command string, timeout time.Duration, cwd string, env map[string]string) (ExecResult, error) {
	client, err := m.GetClient(server)
	if err != nil {
		return ExecResult{}, err
	}
	full := command
	if cwd != "" {
		full = "cd " + shellQuote(cwd) + " && " + full
	}
	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for key := range env {
			if !envName.MatchString(key) {
				return ExecResult{}, &CommandError{Message: fmt.Sprintf("环境变量名无效: %q", key)}
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		exports := make([]string, 0, len(keys))
		for _, key := range keys {
			exports = append(exports, "export "+key+"="+shellQuote(env[key]))
		}
		full = strings.Join(exports, " ") + " && " + full
	}
	session, err := client.NewSession()
	if err != nil {
		return ExecResult{}, &Error{Message: "无法创建 SSH 执行 channel: " + err.Error()}
	}
	defer session.Close()
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return ExecResult{}, &Error{Message: "读取 stdout 失败: " + err.Error()}
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return ExecResult{}, &Error{Message: "读取 stderr 失败: " + err.Error()}
	}
	var stdout, stderr bytes.Buffer
	overflow := make(chan string, 2)
	readDone := make(chan struct{}, 2)
	go copyLimited(&stdout, stdoutPipe, "stdout", overflow, readDone)
	go copyLimited(&stderr, stderrPipe, "stderr", overflow, readDone)
	if err := session.Start(full); err != nil {
		return ExecResult{}, &Error{Message: "执行命令失败: " + err.Error()}
	}
	wait := make(chan error, 1)
	go func() { wait <- session.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-wait:
	case stream := <-overflow:
		_ = session.Close()
		waitErr = <-wait
		<-readDone
		<-readDone
		return ExecResult{}, &CommandError{Message: fmt.Sprintf("%s 输出超过 %d 字节上限", stream, MaxCommandOutput), Stdout: validUTF8(stdout.String()), Stderr: validUTF8(stderr.String())}
	case <-timer.C:
		_ = session.Close()
		waitErr = <-wait
		<-readDone
		<-readDone
		return ExecResult{}, &CommandError{Message: fmt.Sprintf("命令执行超时（%s）", timeout), Stdout: validUTF8(stdout.String()), Stderr: validUTF8(stderr.String())}
	}
	<-readDone
	<-readDone
	result := ExecResult{Stdout: validUTF8(stdout.String()), Stderr: validUTF8(stderr.String()), ExitCode: 0}
	if waitErr != nil {
		var exitErr *ssh.ExitError
		if errors.As(waitErr, &exitErr) {
			code := exitErr.ExitStatus()
			result.ExitCode = code
			message := strings.TrimSpace(result.Stderr)
			if message == "" {
				message = truncate(strings.TrimSpace(result.Stdout), 200)
			}
			return ExecResult{}, &CommandError{Message: fmt.Sprintf("命令退出码 %d: %s", code, message), Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: &code}
		}
		return ExecResult{}, &Error{Message: "执行命令失败: " + waitErr.Error()}
	}
	return result, nil
}

func copyLimited(dst *bytes.Buffer, src io.Reader, name string, overflow chan<- string, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	_, err := io.CopyN(dst, src, MaxCommandOutput+1)
	if err == nil && dst.Len() > MaxCommandOutput {
		dst.Truncate(MaxCommandOutput)
		select {
		case overflow <- name:
		default:
		}
	}
}

func (m *Manager) ExecMulti(servers []*config.Server, command string, timeout time.Duration) []map[string]any {
	results := make([]map[string]any, 0, len(servers))
	for _, server := range servers {
		result, err := m.Exec(server, command, timeout, "", nil)
		if err == nil {
			results = append(results, map[string]any{"server": server.Name, "ok": true, "stdout": result.Stdout, "stderr": result.Stderr, "exit_code": result.ExitCode})
			continue
		}
		item := map[string]any{"server": server.Name, "ok": false, "error": err.Error()}
		var commandErr *CommandError
		if errors.As(err, &commandErr) {
			item["stdout"], item["stderr"], item["exit_code"] = commandErr.Stdout, commandErr.Stderr, commandErr.ExitCode
		}
		results = append(results, item)
	}
	return results
}

func (m *Manager) Test(server *config.Server, timeout time.Duration) (bool, string) {
	result, err := m.Exec(server, "printf '%s\\n' 'ssh_mcp_ok'", timeout, "", nil)
	if err != nil {
		return false, err.Error()
	}
	if strings.TrimSpace(result.Stderr) != "" {
		return false, "连接测试失败：stderr " + truncate(strings.TrimSpace(result.Stderr), 200)
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.TrimSpace(line) == "ssh_mcp_ok" {
			return true, "连接成功"
		}
	}
	return false, "连接测试失败：未收到标记"
}

func (m *Manager) sftp(server *config.Server) (*sftp.Client, error) {
	client, err := m.GetClient(server)
	if err != nil {
		return nil, err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, &Error{Message: "创建 SFTP 客户端失败: " + err.Error()}
	}
	return sftpClient, nil
}

func (m *Manager) Upload(server *config.Server, localPath, remotePath string) (map[string]any, error) {
	client, err := m.sftp(server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	source, err := os.Open(localPath)
	if err != nil {
		return nil, &Error{Message: "上传失败: " + err.Error()}
	}
	defer source.Close()
	target, err := client.Create(remotePath)
	if err != nil {
		return nil, &Error{Message: "上传失败: " + err.Error()}
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return nil, &Error{Message: "上传失败: " + copyErr.Error()}
	}
	if closeErr != nil {
		return nil, &Error{Message: "上传失败: " + closeErr.Error()}
	}
	info, err := client.Stat(remotePath)
	if err != nil {
		return nil, &Error{Message: "上传失败: " + err.Error()}
	}
	return map[string]any{"ok": true, "size": info.Size()}, nil
}

func (m *Manager) Download(server *config.Server, remotePath, localPath string) (map[string]any, error) {
	client, err := m.sftp(server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	source, err := client.Open(remotePath)
	if err != nil {
		return nil, &Error{Message: "下载失败: " + err.Error()}
	}
	defer source.Close()
	target, err := os.Create(localPath)
	if err != nil {
		return nil, &Error{Message: "下载失败: " + err.Error()}
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return nil, &Error{Message: "下载失败: " + copyErr.Error()}
	}
	if closeErr != nil {
		return nil, &Error{Message: "下载失败: " + closeErr.Error()}
	}
	return map[string]any{"ok": true, "local_path": localPath}, nil
}

func (m *Manager) ReadFile(server *config.Server, path string, maxBytes int) (string, error) {
	if maxBytes < 1 || maxBytes > MaxFileBytes {
		return "", &Error{Message: fmt.Sprintf("max_bytes 必须是 1 到 %d 之间的整数", MaxFileBytes)}
	}
	client, err := m.sftp(server)
	if err != nil {
		return "", err
	}
	defer client.Close()
	info, err := client.Stat(path)
	if err != nil {
		return "", &Error{Message: "读取文件失败: " + err.Error()}
	}
	if info.Size() > int64(maxBytes) {
		return "", &Error{Message: fmt.Sprintf("文件过大（%d 字节），上限 %d 字节", info.Size(), maxBytes)}
	}
	file, err := client.Open(path)
	if err != nil {
		return "", &Error{Message: "读取文件失败: " + err.Error()}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes+1)))
	if err != nil {
		return "", &Error{Message: "读取文件失败: " + err.Error()}
	}
	if len(data) > maxBytes {
		return "", &Error{Message: fmt.Sprintf("文件读取超过上限 %d 字节", maxBytes)}
	}
	return validUTF8(string(data)), nil
}

func (m *Manager) WriteFile(server *config.Server, path, content string) (map[string]any, error) {
	data := []byte(content)
	if len(data) > MaxFileBytes {
		return nil, &Error{Message: fmt.Sprintf("写入内容超过 %d 字节上限", MaxFileBytes)}
	}
	client, err := m.sftp(server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	file, err := client.Create(path)
	if err != nil {
		return nil, &Error{Message: "写入文件失败: " + err.Error()}
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, &Error{Message: "写入文件失败: " + writeErr.Error()}
	}
	if closeErr != nil {
		return nil, &Error{Message: "写入文件失败: " + closeErr.Error()}
	}
	return map[string]any{"ok": true, "path": path}, nil
}

func (m *Manager) ListDir(server *config.Server, path string) ([]map[string]any, error) {
	client, err := m.sftp(server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	infos, err := client.ReadDir(path)
	if err != nil {
		return nil, &Error{Message: "列目录失败: " + err.Error()}
	}
	items := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		kind := "file"
		if info.IsDir() {
			kind = "dir"
		}
		items = append(items, map[string]any{"name": info.Name(), "type": kind, "size": info.Size(), "mtime": info.ModTime().Unix()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i]["type"] != items[j]["type"] {
			return items[i]["type"] == "dir"
		}
		return strings.ToLower(items[i]["name"].(string)) < strings.ToLower(items[j]["name"].(string))
	})
	return items, nil
}

func (m *Manager) Stat(server *config.Server, path string) (map[string]any, error) {
	client, err := m.sftp(server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	info, err := client.Stat(path)
	if err != nil {
		return nil, &Error{Message: "stat 失败: " + err.Error()}
	}
	return map[string]any{"path": path, "size": info.Size(), "mtime": info.ModTime().Unix(), "mode": "0o" + strconv.FormatUint(uint64(info.Mode()), 8)}, nil
}

func endpointAddress(host string, port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
func shellQuote(value string) string               { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func envBool(name string, fallback bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
func validUTF8(value string) string { return strings.ToValidUTF8(value, "�") }
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
