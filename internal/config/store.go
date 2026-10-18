package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ackwrap/ssh-mcp/internal/secure"
	"github.com/gofrs/flock"
)

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

type RawConfig struct {
	Version    int    `json:"version"`
	Revision   int    `json:"revision"`
	Salt       string `json:"salt"`
	MasterHash string `json:"master_hash"`
	Payload    string `json:"payload"`
}

type Store struct {
	Path       string
	SecretPath string
	lock       *flock.Flock
	mu         sync.Mutex
}

func NewStore(path, secretPath string) (*Store, error) {
	var err error
	if path == "" {
		path, err = secure.ConfigPath()
		if err != nil {
			return nil, err
		}
	}
	if secretPath == "" {
		secretPath, err = secure.KeyFilePath()
		if err != nil {
			return nil, err
		}
	}
	return &Store{Path: path, SecretPath: secretPath, lock: flock.New(path + ".lock")}, nil
}

func (s *Store) Exists() bool {
	_, err := os.Stat(s.Path)
	return err == nil
}

func (s *Store) withLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	if err := s.lock.Lock(); err != nil {
		return fmt.Errorf("获取配置文件锁失败: %w", err)
	}
	defer s.lock.Unlock()
	return fn()
}

func (s *Store) loadRawUnlocked() (RawConfig, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return RawConfig{}, fmt.Errorf("配置文件读取失败: %w", err)
	}
	var raw RawConfig
	if err := decodeStrict(data, &raw); err != nil {
		return RawConfig{}, fmt.Errorf("配置文件读取失败: %w", err)
	}
	if raw.Revision < 0 {
		return RawConfig{}, errors.New("配置文件 revision 无效")
	}
	return raw, nil
}

func (s *Store) LoadRaw() (RawConfig, error) {
	var raw RawConfig
	err := s.withLock(func() error {
		var err error
		raw, err = s.loadRawUnlocked()
		return err
	})
	return raw, err
}

func (s *Store) currentRawUnlocked() (*RawConfig, int, error) {
	if _, err := os.Stat(s.Path); errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	} else if err != nil {
		return nil, 0, err
	}
	raw, err := s.loadRawUnlocked()
	if err != nil {
		return nil, 0, err
	}
	return &raw, raw.Revision, nil
}

func checkRevision(current int, expected *int) error {
	if expected != nil && current != *expected {
		return &ConflictError{Message: fmt.Sprintf("配置已被其他进程更新（期望 revision %d，当前为 %d），请刷新后重试", *expected, current)}
	}
	return nil
}

func (s *Store) saveRawUnlocked(raw RawConfig) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".ssh-mcp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("原子保存配置失败: %w", err)
	}
	return nil
}

func (s *Store) SaveEncrypted(cfg AppConfig, password string, expectedRevision *int) ([]byte, error) {
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	salt, err := secure.NewSalt()
	if err != nil {
		return nil, err
	}
	key := secure.DeriveKey(password, salt)
	payload, err := secure.Encrypt(key, plaintext)
	if err != nil {
		return nil, err
	}
	err = s.withLock(func() error {
		_, revision, err := s.currentRawUnlocked()
		if err != nil {
			return err
		}
		if err := checkRevision(revision, expectedRevision); err != nil {
			return err
		}
		return s.saveRawUnlocked(RawConfig{
			Version: 1, Revision: revision + 1, Salt: hex.EncodeToString(salt),
			MasterHash: secure.MakeMasterHash(password, salt), Payload: payload,
		})
	})
	return key, err
}

func (s *Store) Decrypt(key []byte) (AppConfig, int, error) {
	raw, err := s.LoadRaw()
	if err != nil {
		return AppConfig{}, 0, err
	}
	cfg, err := decryptRaw(raw, key)
	return cfg, raw.Revision, err
}

func decryptRaw(raw RawConfig, key []byte) (AppConfig, error) {
	if raw.Payload == "" {
		return AppConfig{}, errors.New("配置文件格式不正确")
	}
	plaintext, err := secure.Decrypt(key, raw.Payload)
	if err != nil {
		return AppConfig{}, err
	}
	return DecodeAppConfig(plaintext)
}

func (s *Store) GetRevision() (int, error) {
	raw, err := s.LoadRaw()
	return raw.Revision, err
}

func (s *Store) GetSaltHex() (string, error) {
	raw, err := s.LoadRaw()
	return raw.Salt, err
}

func (s *Store) GetMasterHash() (string, error) {
	raw, err := s.LoadRaw()
	return raw.MasterHash, err
}

func (s *Store) savePayload(payload string, expectedRevision int) (int, error) {
	newRevision := 0
	err := s.withLock(func() error {
		raw, revision, err := s.currentRawUnlocked()
		if err != nil {
			return err
		}
		if err := checkRevision(revision, &expectedRevision); err != nil {
			return err
		}
		if raw == nil {
			return &ConflictError{Message: "配置已被删除，请重新初始化或刷新后重试"}
		}
		newRevision = revision + 1
		raw.Revision = newRevision
		raw.Payload = payload
		return s.saveRawUnlocked(*raw)
	})
	return newRevision, err
}

func (s *Store) ResetAll() error {
	return s.withLock(func() error {
		for _, path := range []string{s.Path, s.SecretPath} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	})
}

type OpenStore struct {
	Store    *Store
	Key      []byte
	Config   AppConfig
	Revision int
}

func Open(store *Store, key []byte) (*OpenStore, error) {
	cfg, revision, err := store.Decrypt(key)
	if err != nil {
		return nil, err
	}
	return &OpenStore{Store: store, Key: append([]byte(nil), key...), Config: cfg, Revision: revision}, nil
}

func (o *OpenStore) commit(cfg AppConfig) error {
	if err := cfg.NormalizeAndValidate(); err != nil {
		return err
	}
	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	payload, err := secure.Encrypt(o.Key, plaintext)
	if err != nil {
		return err
	}
	revision, err := o.Store.savePayload(payload, o.Revision)
	if err != nil {
		return err
	}
	o.Config = cfg
	o.Revision = revision
	return nil
}

func (o *OpenStore) SetSettings(settings Settings) error {
	cfg := cloneConfig(o.Config)
	cfg.Settings = settings
	return o.commit(cfg)
}

func (o *OpenStore) GetServer(name string) (*Server, error) {
	for i := range o.Config.Servers {
		if o.Config.Servers[i].Name == name {
			return &o.Config.Servers[i], nil
		}
	}
	return nil, fmt.Errorf("服务器不存在: %s", name)
}

func (o *OpenStore) GetJumpHost(name string) (*JumpHost, error) {
	for i := range o.Config.JumpHosts {
		if o.Config.JumpHosts[i].Name == name {
			return &o.Config.JumpHosts[i], nil
		}
	}
	return nil, fmt.Errorf("跳板机不存在: %s", name)
}

func (o *OpenStore) AddServer(data map[string]any) (*Server, error) {
	prepared, err := prepareEndpoint(data, nil)
	if err != nil {
		return nil, err
	}
	server, err := decodeServerMap(prepared)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(o.Config)
	cfg.Servers = append(cfg.Servers, server)
	if err := o.commit(cfg); err != nil {
		return nil, err
	}
	return o.GetServer(server.Name)
}

func (o *OpenStore) UpdateServer(name string, changes map[string]any) (*Server, error) {
	old, err := o.GetServer(name)
	if err != nil {
		return nil, err
	}
	oldMap := structMap(*old)
	merged := mergeChanges(oldMap, changes)
	prepared, err := prepareEndpoint(merged, oldMap)
	if err != nil {
		return nil, err
	}
	server, err := decodeServerMap(prepared)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(o.Config)
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == name {
			cfg.Servers[i] = server
		}
	}
	if err := o.commit(cfg); err != nil {
		return nil, err
	}
	return o.GetServer(server.Name)
}

func (o *OpenStore) CopyServer(name string, changes map[string]any) (*Server, error) {
	old, err := o.GetServer(name)
	if err != nil {
		return nil, err
	}
	oldMap := structMap(*old)
	merged := mergeChanges(oldMap, changes)
	delete(merged, "id")
	prepared, err := prepareEndpoint(merged, oldMap)
	if err != nil {
		return nil, err
	}
	server, err := decodeServerMap(prepared)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(o.Config)
	cfg.Servers = append(cfg.Servers, server)
	if err := o.commit(cfg); err != nil {
		return nil, err
	}
	return o.GetServer(server.Name)
}

func (o *OpenStore) DeleteServer(name string) error {
	if _, err := o.GetServer(name); err != nil {
		return err
	}
	cfg := cloneConfig(o.Config)
	servers := cfg.Servers[:0]
	for _, server := range cfg.Servers {
		if server.Name != name {
			servers = append(servers, server)
		}
	}
	cfg.Servers = servers
	return o.commit(cfg)
}

func (o *OpenStore) AddJumpHost(data map[string]any) (*JumpHost, error) {
	prepared, err := prepareEndpoint(data, nil)
	if err != nil {
		return nil, err
	}
	jump, err := decodeJumpMap(prepared)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(o.Config)
	cfg.JumpHosts = append(cfg.JumpHosts, jump)
	if err := o.commit(cfg); err != nil {
		return nil, err
	}
	return o.GetJumpHost(jump.Name)
}

func (o *OpenStore) UpdateJumpHost(name string, changes map[string]any) (*JumpHost, error) {
	old, err := o.GetJumpHost(name)
	if err != nil {
		return nil, err
	}
	oldMap := structMap(*old)
	merged := mergeChanges(oldMap, changes)
	prepared, err := prepareEndpoint(merged, oldMap)
	if err != nil {
		return nil, err
	}
	jump, err := decodeJumpMap(prepared)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(o.Config)
	for i := range cfg.JumpHosts {
		if cfg.JumpHosts[i].Name == name {
			cfg.JumpHosts[i] = jump
		}
	}
	if err := o.commit(cfg); err != nil {
		return nil, err
	}
	return o.GetJumpHost(jump.Name)
}

func (o *OpenStore) DeleteJumpHost(name string) error {
	if _, err := o.GetJumpHost(name); err != nil {
		return err
	}
	for _, server := range o.Config.Servers {
		if server.JumpVia != nil && *server.JumpVia == name {
			return fmt.Errorf("跳板机 %s 仍被其他配置引用，请先解除引用", name)
		}
	}
	for _, jump := range o.Config.JumpHosts {
		if jump.JumpVia != nil && *jump.JumpVia == name {
			return fmt.Errorf("跳板机 %s 仍被其他配置引用，请先解除引用", name)
		}
	}
	cfg := cloneConfig(o.Config)
	jumps := cfg.JumpHosts[:0]
	for _, jump := range cfg.JumpHosts {
		if jump.Name != name {
			jumps = append(jumps, jump)
		}
	}
	cfg.JumpHosts = jumps
	return o.commit(cfg)
}

func (o *OpenStore) ExportFull() (RawConfig, error) { return o.Store.LoadRaw() }

func (o *OpenStore) ExportTemplate() map[string]any {
	servers := make([]map[string]any, 0, len(o.Config.Servers))
	for _, server := range o.Config.Servers {
		servers = append(servers, MaskEndpoint(structMap(server)))
	}
	jumps := make([]map[string]any, 0, len(o.Config.JumpHosts))
	for _, jump := range o.Config.JumpHosts {
		jumps = append(jumps, MaskEndpoint(structMap(jump)))
	}
	return map[string]any{"version": 1, "servers": servers, "jumphosts": jumps}
}

func (o *OpenStore) ImportFull(data map[string]any, sourcePassword string) (AppConfig, error) {
	rawBytes, err := json.Marshal(data)
	if err != nil {
		return AppConfig{}, errors.New("导入数据格式不正确，不是 ssh_mcp 的完整导出")
	}
	var raw RawConfig
	if err := decodeStrict(rawBytes, &raw); err != nil || raw.Salt == "" || raw.MasterHash == "" || raw.Payload == "" {
		return AppConfig{}, errors.New("导入数据格式不正确，不是 ssh_mcp 的完整导出")
	}
	salt, err := hex.DecodeString(raw.Salt)
	if err != nil {
		return AppConfig{}, errors.New("导入数据格式不正确，不是 ssh_mcp 的完整导出")
	}
	if !secure.VerifyMaster(sourcePassword, salt, raw.MasterHash) {
		return AppConfig{}, errors.New("主密码错误，无法解密导入的配置")
	}
	cfg, err := decryptRaw(raw, secure.DeriveKey(sourcePassword, salt))
	if err != nil {
		return AppConfig{}, err
	}
	// Address disclosure requires a local opt-in; imports must not change it.
	cfg.Settings = o.Config.Settings
	if err := o.commit(cfg); err != nil {
		return AppConfig{}, err
	}
	return o.Config, nil
}

func MaskEndpoint(data map[string]any) map[string]any {
	masked := cloneMap(data)
	if auth, ok := masked["auth"].(map[string]any); ok {
		for _, field := range []string{"password", "key_data", "passphrase"} {
			if value, exists := auth[field]; exists && value != nil && value != "" {
				auth[field] = Mask
			}
		}
	}
	if proxy, ok := masked["proxy"].(map[string]any); ok {
		if value, exists := proxy["password"]; exists && value != nil && value != "" {
			proxy["password"] = Mask
		}
	}
	return masked
}

func PublicEndpoint(data map[string]any, allowAddresses bool) map[string]any {
	public := cloneMap(data)
	if !allowAddresses {
		public["host"] = Mask
	}
	if auth, ok := public["auth"].(map[string]any); ok {
		public["auth"] = map[string]any{"type": auth["type"]}
	}
	if proxy, ok := public["proxy"].(map[string]any); ok {
		clean := map[string]any{}
		for _, key := range []string{"type", "host", "port", "username"} {
			if value, exists := proxy[key]; exists && value != nil {
				clean[key] = value
			}
		}
		if !allowAddresses {
			clean["host"] = Mask
		}
		public["proxy"] = clean
	}
	return public
}

func prepareEndpoint(data, old map[string]any) (map[string]any, error) {
	out := cloneMap(data)
	auth, _ := out["auth"].(map[string]any)
	oldAuth, _ := old["auth"].(map[string]any)
	authType, _ := auth["type"].(string)
	relevant := map[string][]string{
		"password": {"password"}, "key_data": {"key_data", "passphrase"}, "key_path": {"passphrase"},
	}[authType]
	for _, field := range relevant {
		if auth[field] == Mask {
			if oldAuth == nil || oldAuth[field] == nil || oldAuth[field] == "" {
				return nil, fmt.Errorf("请填写%s（当前无旧值可保留）", field)
			}
			auth[field] = oldAuth[field]
		}
	}
	if auth != nil {
		out["auth"] = auth
	}
	if proxy, ok := out["proxy"].(map[string]any); ok && proxy["password"] == Mask {
		oldProxy, _ := old["proxy"].(map[string]any)
		if oldProxy == nil || oldProxy["password"] == nil || oldProxy["password"] == "" {
			return nil, errors.New("请填写代理密码（当前无旧值可保留）")
		}
		proxy["password"] = oldProxy["password"]
	}
	return out, nil
}

func mergeChanges(old, changes map[string]any) map[string]any {
	merged := cloneMap(old)
	for key, value := range changes {
		incoming, incomingOK := value.(map[string]any)
		current, currentOK := merged[key].(map[string]any)
		if incomingOK && currentOK {
			nested := cloneMap(current)
			for nestedKey, nestedValue := range incoming {
				nested[nestedKey] = nestedValue
			}
			merged[key] = nested
		} else {
			merged[key] = value
		}
	}
	return merged
}

func decodeServerMap(data map[string]any) (Server, error) {
	bytes, _ := json.Marshal(data)
	return DecodeServer(bytes)
}

func decodeJumpMap(data map[string]any) (JumpHost, error) {
	bytes, _ := json.Marshal(data)
	return DecodeJumpHost(bytes)
}

func structMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result map[string]any
	_ = decoder.Decode(&result)
	return result
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	data, _ := json.Marshal(source)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var cloned map[string]any
	_ = decoder.Decode(&cloned)
	return cloned
}

func cloneConfig(source AppConfig) AppConfig {
	data, _ := json.Marshal(source)
	var cloned AppConfig
	_ = json.Unmarshal(data, &cloned)
	return cloned
}
