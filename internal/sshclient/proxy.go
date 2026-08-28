package sshclient

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ssh-mcp/ssh-mcp-go/internal/config"
	"golang.org/x/net/idna"
)

const maxHTTPProxyHeader = 64 * 1024

func dialProxy(proxy *config.ProxySpec, targetHost string, targetPort int, timeout time.Duration) (net.Conn, error) {
	switch proxy.Type {
	case "socks5":
		return dialSOCKS5(proxy, targetHost, targetPort, timeout)
	case "http":
		return dialHTTPProxy(proxy, targetHost, targetPort, timeout)
	default:
		return nil, fmt.Errorf("不支持的代理类型: %s", proxy.Type)
	}
}

func dialSOCKS5(proxy *config.ProxySpec, targetHost string, targetPort int, timeout time.Duration) (conn net.Conn, err error) {
	conn, err = net.DialTimeout("tcp", net.JoinHostPort(proxy.Host, strconv.Itoa(proxy.Port)), timeout)
	if err != nil {
		return nil, fmt.Errorf("连接 SOCKS5 代理失败: %w", err)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	defer conn.SetDeadline(time.Time{})

	if proxy.Username != nil {
		if _, err = conn.Write([]byte{5, 2, 0, 2}); err != nil {
			return nil, fmt.Errorf("SOCKS5 代理握手失败: %w", err)
		}
	} else if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
		return nil, fmt.Errorf("SOCKS5 代理握手失败: %w", err)
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(conn, reply); err != nil {
		return nil, errors.New("代理连接在协议握手期间关闭")
	}
	if reply[0] != 5 {
		return nil, errors.New("SOCKS5 代理版本错误")
	}
	switch reply[1] {
	case 0:
	case 2:
		if proxy.Username == nil {
			return nil, errors.New("SOCKS5 代理要求用户名密码认证")
		}
		username := []byte(config.String(proxy.Username))
		password := []byte(config.String(proxy.Password))
		if len(username) > 255 || len(password) > 255 {
			return nil, errors.New("SOCKS5 用户名或密码过长")
		}
		packet := append([]byte{1, byte(len(username))}, username...)
		packet = append(packet, byte(len(password)))
		packet = append(packet, password...)
		if _, err = conn.Write(packet); err != nil {
			return nil, fmt.Errorf("SOCKS5 代理认证失败: %w", err)
		}
		if _, err = io.ReadFull(conn, reply); err != nil || reply[0] != 1 || reply[1] != 0 {
			return nil, errors.New("SOCKS5 代理认证失败")
		}
	case 0xff:
		return nil, errors.New("SOCKS5 代理不支持所需认证方式")
	default:
		return nil, fmt.Errorf("SOCKS5 代理选择了不支持的认证方式: %d", reply[1])
	}

	address, err := socksAddress(targetHost)
	if err != nil {
		return nil, err
	}
	request := append([]byte{5, 1, 0}, address...)
	request = append(request, byte(targetPort>>8), byte(targetPort))
	if _, err = conn.Write(request); err != nil {
		return nil, fmt.Errorf("SOCKS5 代理连接失败: %w", err)
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(conn, header); err != nil {
		return nil, errors.New("代理连接在协议握手期间关闭")
	}
	if header[0] != 5 || header[2] != 0 {
		return nil, errors.New("SOCKS5 代理响应格式错误")
	}
	if header[1] != 0 {
		names := map[byte]string{1: "一般性失败", 2: "规则禁止", 3: "网络不可达", 4: "主机不可达", 5: "连接被拒绝", 6: "TTL 超时", 7: "不支持的命令", 8: "不支持的地址类型"}
		return nil, fmt.Errorf("SOCKS5 代理连接失败: %s", names[header[1]])
	}
	var addressLength int
	switch header[3] {
	case 1:
		addressLength = 4
	case 3:
		length := make([]byte, 1)
		if _, err = io.ReadFull(conn, length); err != nil {
			return nil, errors.New("代理连接在协议握手期间关闭")
		}
		addressLength = int(length[0])
	case 4:
		addressLength = 16
	default:
		return nil, fmt.Errorf("SOCKS5 代理返回未知地址类型: %d", header[3])
	}
	if _, err = io.CopyN(io.Discard, conn, int64(addressLength+2)); err != nil {
		return nil, errors.New("代理连接在协议握手期间关闭")
	}
	return conn, nil
}

func socksAddress(host string) ([]byte, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return append([]byte{1}, v4...), nil
		}
		return append([]byte{4}, ip.To16()...), nil
	}
	encoded, err := idnaASCII(host)
	if err != nil || len(encoded) == 0 || len(encoded) > 255 {
		return nil, errors.New("SOCKS5 目标域名长度无效")
	}
	return append([]byte{3, byte(len(encoded))}, encoded...), nil
}

func idnaASCII(host string) ([]byte, error) {
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return nil, err
	}
	return []byte(ascii), nil
}

func dialHTTPProxy(proxy *config.ProxySpec, targetHost string, targetPort int, timeout time.Duration) (conn net.Conn, err error) {
	conn, err = net.DialTimeout("tcp", net.JoinHostPort(proxy.Host, strconv.Itoa(proxy.Port)), timeout)
	if err != nil {
		return nil, fmt.Errorf("连接 HTTP 代理失败: %w", err)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	defer conn.SetDeadline(time.Time{})

	target := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	var request strings.Builder
	fmt.Fprintf(&request, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if proxy.Username != nil {
		token := base64.StdEncoding.EncodeToString([]byte(config.String(proxy.Username) + ":" + config.String(proxy.Password)))
		fmt.Fprintf(&request, "Proxy-Authorization: Basic %s\r\n", token)
	}
	request.WriteString("\r\n")
	if _, err = io.WriteString(conn, request.String()); err != nil {
		return nil, fmt.Errorf("HTTP 代理握手失败: %w", err)
	}

	reader := bufio.NewReaderSize(conn, 4096)
	total := 0
	firstLine := ""
	for {
		line, readErr := reader.ReadString('\n')
		total += len(line)
		if total > maxHTTPProxyHeader {
			return nil, fmt.Errorf("HTTP 代理响应头超过 %d 字节上限", maxHTTPProxyHeader)
		}
		if readErr != nil {
			return nil, errors.New("HTTP 代理连接被关闭")
		}
		if firstLine == "" {
			firstLine = strings.TrimSpace(line)
		}
		if line == "\r\n" {
			break
		}
	}
	parts := strings.Fields(firstLine)
	if len(parts) < 2 || parts[1] != "200" {
		return nil, fmt.Errorf("HTTP 代理 CONNECT 失败: %s", firstLine)
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(data []byte) (int, error) { return c.reader.Read(data) }
