package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Mask = "******"

type AuthSpec struct {
	Type       string  `json:"type"`
	Password   *string `json:"password"`
	KeyData    *string `json:"key_data"`
	KeyPath    *string `json:"key_path"`
	Passphrase *string `json:"passphrase"`
}

type ProxySpec struct {
	Type     string  `json:"type"`
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Username *string `json:"username"`
	Password *string `json:"password"`
}

type Endpoint struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Auth     AuthSpec `json:"auth"`
	JumpVia  *string  `json:"jump_via"`
}

type Server struct {
	Endpoint
	Proxy *ProxySpec `json:"proxy"`
}

type JumpHost struct {
	Endpoint
}

type AppConfig struct {
	Version   int        `json:"version"`
	Servers   []Server   `json:"servers"`
	JumpHosts []JumpHost `json:"jumphosts"`
}

func NewAppConfig() AppConfig {
	return AppConfig{Version: 1, Servers: []Server{}, JumpHosts: []JumpHost{}}
}

func (s *Server) UnmarshalJSON(data []byte) error {
	type alias Server
	value := alias{Endpoint: Endpoint{Port: 22}}
	if err := decodeStrict(data, &value); err != nil {
		return err
	}
	*s = Server(value)
	return nil
}

func (j *JumpHost) UnmarshalJSON(data []byte) error {
	type alias JumpHost
	value := alias{Endpoint: Endpoint{Port: 22}}
	if err := decodeStrict(data, &value); err != nil {
		return err
	}
	*j = JumpHost(value)
	return nil
}

func DecodeServer(data []byte) (Server, error) {
	var server Server
	if err := decodeStrict(data, &server); err != nil {
		return Server{}, fmt.Errorf("服务器配置格式不正确: %w", err)
	}
	if err := server.NormalizeAndValidate(); err != nil {
		return Server{}, err
	}
	return server, nil
}

func DecodeJumpHost(data []byte) (JumpHost, error) {
	var jump JumpHost
	if err := decodeStrict(data, &jump); err != nil {
		return JumpHost{}, fmt.Errorf("跳板机配置格式不正确: %w", err)
	}
	if err := jump.NormalizeAndValidate(); err != nil {
		return JumpHost{}, err
	}
	return jump, nil
}

func DecodeAppConfig(data []byte) (AppConfig, error) {
	var cfg AppConfig
	if err := decodeStrict(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("配置内容格式不正确: %w", err)
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return AppConfig{}, err
	}
	return cfg, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("包含多个 JSON 值")
		}
		return err
	}
	return nil
}

func (a *AuthSpec) normalizeAndValidate() error {
	switch a.Type {
	case "password":
		a.KeyData, a.KeyPath, a.Passphrase = nil, nil, nil
		if blank(a.Password) {
			return errors.New("password 认证需要填写密码")
		}
	case "key_data":
		a.Password, a.KeyPath = nil, nil
		if blank(a.KeyData) {
			return errors.New("key_data 认证需要填写私钥内容")
		}
	case "key_path":
		a.Password, a.KeyData = nil, nil
		if blank(a.KeyPath) {
			return errors.New("key_path 认证需要填写私钥路径")
		}
	default:
		return fmt.Errorf("不支持的认证类型: %s", a.Type)
	}
	return nil
}

func (p *ProxySpec) normalizeAndValidate() error {
	p.Host = strings.TrimSpace(p.Host)
	if p.Host == "" {
		return errors.New("代理主机不能为空")
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("代理端口必须是 1 到 65535 之间的整数")
	}
	if p.Type != "socks5" && p.Type != "http" {
		return fmt.Errorf("不支持的代理类型: %s", p.Type)
	}
	p.Username = trimOptional(p.Username)
	return nil
}

func (e *Endpoint) normalizeAndValidate() error {
	e.Name = strings.TrimSpace(e.Name)
	e.Host = strings.TrimSpace(e.Host)
	e.Username = strings.TrimSpace(e.Username)
	e.JumpVia = trimOptional(e.JumpVia)
	if e.Name == "" {
		return errors.New("名称不能为空")
	}
	if strings.ContainsAny(e.Name, `/\`) {
		return errors.New("名称不能包含 / 或 \\")
	}
	if e.Host == "" {
		return errors.New("主机不能为空")
	}
	if e.Username == "" {
		return errors.New("用户名不能为空")
	}
	if e.Port < 1 || e.Port > 65535 {
		return errors.New("端口必须是 1 到 65535 之间的整数")
	}
	if e.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		e.ID = id
	}
	return e.Auth.normalizeAndValidate()
}

func (s *Server) NormalizeAndValidate() error {
	if err := s.Endpoint.normalizeAndValidate(); err != nil {
		return err
	}
	if s.Proxy != nil {
		return s.Proxy.normalizeAndValidate()
	}
	return nil
}

func (j *JumpHost) NormalizeAndValidate() error {
	return j.Endpoint.normalizeAndValidate()
}

func (c *AppConfig) NormalizeAndValidate() error {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Version != 1 {
		return fmt.Errorf("不支持的配置版本: %d", c.Version)
	}
	if c.Servers == nil {
		c.Servers = []Server{}
	}
	if c.JumpHosts == nil {
		c.JumpHosts = []JumpHost{}
	}
	for i := range c.Servers {
		if err := c.Servers[i].NormalizeAndValidate(); err != nil {
			return fmt.Errorf("服务器 %d: %w", i+1, err)
		}
	}
	for i := range c.JumpHosts {
		if err := c.JumpHosts[i].NormalizeAndValidate(); err != nil {
			return fmt.Errorf("跳板机 %d: %w", i+1, err)
		}
	}
	return c.validateInvariants()
}

func (c AppConfig) validateInvariants() error {
	names := make(map[string]struct{}, len(c.Servers)+len(c.JumpHosts))
	jumpNames := make(map[string]struct{}, len(c.JumpHosts))
	for _, jump := range c.JumpHosts {
		jumpNames[jump.Name] = struct{}{}
	}
	for _, endpoint := range appendEndpoints(c) {
		if _, exists := names[endpoint.Name]; exists {
			return fmt.Errorf("名称重复: %s", endpoint.Name)
		}
		names[endpoint.Name] = struct{}{}
	}
	graph := make(map[string]*string, len(c.JumpHosts))
	for _, jump := range c.JumpHosts {
		graph[jump.Name] = jump.JumpVia
		if jump.JumpVia != nil {
			if _, ok := jumpNames[*jump.JumpVia]; !ok {
				return fmt.Errorf("跳板机 %s 引用了不存在的跳板机 %s", jump.Name, *jump.JumpVia)
			}
		}
	}
	for _, server := range c.Servers {
		if server.JumpVia != nil {
			if _, ok := jumpNames[*server.JumpVia]; !ok {
				return fmt.Errorf("服务器 %s 引用了不存在的跳板机 %s", server.Name, *server.JumpVia)
			}
		}
	}
	for start := range graph {
		seen := map[string]struct{}{}
		current := start
		for current != "" {
			if _, ok := seen[current]; ok {
				return fmt.Errorf("跳板链存在环路: %s", start)
			}
			seen[current] = struct{}{}
			next := graph[current]
			if next == nil {
				break
			}
			current = *next
		}
	}
	return nil
}

func appendEndpoints(c AppConfig) []Endpoint {
	all := make([]Endpoint, 0, len(c.Servers)+len(c.JumpHosts))
	for _, server := range c.Servers {
		all = append(all, server.Endpoint)
	}
	for _, jump := range c.JumpHosts {
		all = append(all, jump.Endpoint)
	}
	return all
}

func newID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成配置 ID 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func blank(value *string) bool { return value == nil || strings.TrimSpace(*value) == "" }

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func String(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
