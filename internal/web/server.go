package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ssh-mcp/ssh-mcp-go/internal/config"
	"github.com/ssh-mcp/ssh-mcp-go/internal/mcpserver"
	"github.com/ssh-mcp/ssh-mcp-go/internal/secure"
	"github.com/ssh-mcp/ssh-mcp-go/internal/sshclient"
)

const (
	SessionTTL  = 7 * 24 * time.Hour
	SessionName = "ssh_mcp_session"
)

//go:embed ui/*
var uiFiles embed.FS

type Server struct {
	store   *config.Store
	runtime *mcpserver.Runtime
	mux     *http.ServeMux
}

func NewServer(store *config.Store, runtime *mcpserver.Runtime) (*Server, error) {
	var err error
	if store == nil {
		store, err = config.NewStore("", "")
		if err != nil {
			return nil, err
		}
	}
	if runtime == nil {
		runtime = mcpserver.NewRuntime()
	}
	server := &Server{store: store, runtime: runtime, mux: http.NewServeMux()}
	server.routes()
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

func GetPort() (int, error) {
	raw := os.Getenv("SSH_MCP_PORT")
	if raw == "" {
		raw = "8877"
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("SSH_MCP_PORT 必须是 1 到 65535 之间的整数")
	}
	return port, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/auth/status", s.handle(s.authStatus))
	s.mux.HandleFunc("POST /api/auth/setup", s.handle(s.authSetup))
	s.mux.HandleFunc("POST /api/auth/login", s.handle(s.authLogin))
	s.mux.HandleFunc("POST /api/auth/logout", s.handle(s.authLogout))
	s.mux.HandleFunc("POST /api/auth/lock", s.handle(s.authLogout))
	s.mux.HandleFunc("POST /api/auth/change_password", s.handle(s.authChangePassword))
	s.mux.HandleFunc("POST /api/auth/forgot", s.handle(s.authForgot))

	s.mux.HandleFunc("GET /api/servers", s.handle(s.listServers))
	s.mux.HandleFunc("POST /api/servers", s.handle(s.addServer))
	s.mux.HandleFunc("PUT /api/servers/{name}", s.handle(s.updateServer))
	s.mux.HandleFunc("POST /api/servers/{name}/copy", s.handle(s.copyServer))
	s.mux.HandleFunc("DELETE /api/servers/{name}", s.handle(s.deleteServer))
	s.mux.HandleFunc("POST /api/servers/{name}/test", s.handle(s.testServer))

	s.mux.HandleFunc("GET /api/jumphosts", s.handle(s.listJumpHosts))
	s.mux.HandleFunc("POST /api/jumphosts", s.handle(s.addJumpHost))
	s.mux.HandleFunc("PUT /api/jumphosts/{name}", s.handle(s.updateJumpHost))
	s.mux.HandleFunc("DELETE /api/jumphosts/{name}", s.handle(s.deleteJumpHost))

	s.mux.HandleFunc("GET /api/export", s.handle(s.exportConfig))
	s.mux.HandleFunc("POST /api/import", s.handle(s.importConfig))
	s.mux.HandleFunc("GET /api/mcp-configs", s.handle(s.mcpConfigs))
	s.mux.HandleFunc("GET /api/health", s.handle(s.health))

	mcpServer := mcpserver.NewServer(s.runtime)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	s.mux.Handle("GET /mcp/", mcpHandler)
	s.mux.Handle("POST /mcp/", mcpHandler)
	s.mux.Handle("DELETE /mcp/", mcpHandler)
	ui, _ := fs.Sub(uiFiles, "ui")
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(ui))))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, "WebUI 资源不可用", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

type endpointHandler func(http.ResponseWriter, *http.Request) error

func (s *Server) handle(handler endpointHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := handler(w, r); err != nil {
			writeAPIError(w, err)
		}
	}
}

type authError struct{ message string }

func (e *authError) Error() string { return e.message }

func writeAPIError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var auth *authError
	var conflict *config.ConflictError
	if errors.As(err, &auth) {
		status = http.StatusUnauthorized
	}
	if errors.As(err, &conflict) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"detail": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func decodeJSON(r *http.Request, target any) error {
	const maxBody = 16 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("读取请求失败: %w", err)
	}
	if len(data) > maxBody {
		return errors.New("请求内容过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求 JSON 格式不正确: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("请求 JSON 包含多余内容")
	}
	return nil
}

func (s *Server) secret() ([]byte, error) { return secure.LoadOrCreateSessionSecret() }

func makeSession(secret []byte) string {
	payload, _ := json.Marshal(map[string]any{"u": "local", "exp": time.Now().Add(SessionTTL).Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := hmac.New(sha256.New, secret)
	_, _ = signature.Write([]byte(encoded))
	return encoded + "." + hex.EncodeToString(signature.Sum(nil))
}

func verifySession(token string, secret []byte) bool {
	encoded, signatureHex, ok := strings.Cut(token, ".")
	if !ok || strings.Contains(signatureHex, ".") {
		return false
	}
	signature, err := hex.DecodeString(signatureHex)
	if err != nil || len(signature) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	var payload struct {
		User string      `json:"u"`
		Exp  json.Number `json:"exp"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(payloadBytes)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload.User != "local" {
		return false
	}
	expires, err := payload.Exp.Int64()
	return err == nil && expires > time.Now().Unix()
}

func (s *Server) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(SessionName)
	if err != nil {
		return false
	}
	secret, err := s.secret()
	return err == nil && verifySession(cookie.Value, secret)
}

func (s *Server) requireAuth(r *http.Request) error {
	if !s.authenticated(r) {
		return &authError{message: "未登录或会话已过期"}
	}
	return nil
}

func (s *Server) unlock(r *http.Request) (*config.OpenStore, error) {
	if err := s.requireAuth(r); err != nil {
		return nil, err
	}
	key, err := secure.ReadKeyFile()
	if err != nil {
		return nil, err
	}
	if len(key) == 0 {
		return nil, &authError{message: "已锁定，请重新登录解锁"}
	}
	return config.Open(s.store, key)
}

func setSessionCookie(w http.ResponseWriter, secret []byte) {
	http.SetCookie(w, &http.Cookie{Name: SessionName, Value: makeSession(secret), Path: "/", MaxAge: int(SessionTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *Server) invalidate(w http.ResponseWriter) error {
	_ = secure.DeleteKeyFile()
	if _, err := secure.RotateSessionSecret(); err != nil {
		return err
	}
	s.runtime.Reset()
	clearSessionCookie(w)
	return nil
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]any{"configured": s.store.Exists(), "authenticated": s.authenticated(r)})
	return nil
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) error {
	if s.store.Exists() {
		return errors.New("已经初始化，不能重复设置主密码")
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	if len(input.Password) < 6 {
		return errors.New("主密码至少 6 位")
	}
	key, err := s.store.SaveEncrypted(config.NewAppConfig(), input.Password, nil)
	if err != nil {
		return err
	}
	if err := secure.WriteKeyFile(key); err != nil {
		return err
	}
	secret, err := secure.RotateSessionSecret()
	if err != nil {
		return err
	}
	setSessionCookie(w, secret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) error {
	if !s.store.Exists() {
		return errors.New("尚未初始化，请先设置主密码")
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	saltHex, err := s.store.GetSaltHex()
	if err != nil {
		return err
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return errors.New("配置 salt 无效")
	}
	masterHash, err := s.store.GetMasterHash()
	if err != nil {
		return err
	}
	if !secure.VerifyMaster(input.Password, salt, masterHash) {
		return errors.New("主密码错误")
	}
	if err := secure.WriteKeyFile(secure.DeriveKey(input.Password, salt)); err != nil {
		return err
	}
	secret, err := s.secret()
	if err != nil {
		return err
	}
	setSessionCookie(w, secret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) error {
	if err := s.requireAuth(r); err != nil {
		return err
	}
	if err := s.invalidate(w); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) error {
	if err := s.requireAuth(r); err != nil {
		return err
	}
	var input struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	if len(input.NewPassword) < 6 {
		return errors.New("新主密码至少 6 位")
	}
	saltHex, err := s.store.GetSaltHex()
	if err != nil {
		return err
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return errors.New("配置 salt 无效")
	}
	hash, err := s.store.GetMasterHash()
	if err != nil {
		return err
	}
	if !secure.VerifyMaster(input.OldPassword, salt, hash) {
		return errors.New("旧主密码错误")
	}
	opened, err := config.Open(s.store, secure.DeriveKey(input.OldPassword, salt))
	if err != nil {
		return err
	}
	key, err := s.store.SaveEncrypted(opened.Config, input.NewPassword, &opened.Revision)
	if err != nil {
		return err
	}
	if err := secure.WriteKeyFile(key); err != nil {
		return err
	}
	s.runtime.Reset()
	secret, err := secure.RotateSessionSecret()
	if err != nil {
		return err
	}
	setSessionCookie(w, secret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) authForgot(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	if input.Confirm != "DELETE" {
		return errors.New("请输入 DELETE 确认清空")
	}
	if err := s.store.ResetAll(); err != nil {
		return err
	}
	if err := s.invalidate(w); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(opened.Config.Servers))
	for _, endpoint := range opened.Config.Servers {
		items = append(items, config.MaskEndpoint(toMap(endpoint)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": items})
	return nil
}

func (s *Server) addServer(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	var input map[string]any
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	endpoint, err := opened.AddServer(input)
	if err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": endpoint.Name})
	return nil
}

func (s *Server) updateServer(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	var input map[string]any
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	endpoint, err := opened.UpdateServer(r.PathValue("name"), input)
	if err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": endpoint.Name})
	return nil
}

func (s *Server) copyServer(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	var input map[string]any
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	endpoint, err := opened.CopyServer(r.PathValue("name"), input)
	if err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": endpoint.Name})
	return nil
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	if err := opened.DeleteServer(r.PathValue("name")); err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) testServer(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	endpoint, err := opened.GetServer(r.PathValue("name"))
	if err != nil {
		return err
	}
	manager := sshclient.NewManager(opened)
	start := time.Now()
	ok, message := manager.Test(endpoint, 10*time.Second)
	manager.CloseAll()
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": message, "latency_ms": time.Since(start).Milliseconds()})
	return nil
}

func (s *Server) listJumpHosts(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(opened.Config.JumpHosts))
	for _, endpoint := range opened.Config.JumpHosts {
		items = append(items, config.MaskEndpoint(toMap(endpoint)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jumphosts": items})
	return nil
}

func (s *Server) addJumpHost(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	var input map[string]any
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	endpoint, err := opened.AddJumpHost(input)
	if err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": endpoint.Name})
	return nil
}

func (s *Server) updateJumpHost(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	var input map[string]any
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	endpoint, err := opened.UpdateJumpHost(r.PathValue("name"), input)
	if err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": endpoint.Name})
	return nil
}

func (s *Server) deleteJumpHost(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	if err := opened.DeleteJumpHost(r.PathValue("name")); err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (s *Server) exportConfig(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	if r.URL.Query().Get("mode") == "template" {
		writeJSON(w, http.StatusOK, opened.ExportTemplate())
		return nil
	}
	raw, err := opened.ExportFull()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, raw)
	return nil
}

func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) error {
	opened, err := s.unlock(r)
	if err != nil {
		return err
	}
	var input struct {
		Data           map[string]any `json:"data"`
		SourcePassword string         `json:"source_password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	if input.Data == nil {
		return errors.New("导入数据格式不正确")
	}
	cfg, err := opened.ImportFull(input.Data, input.SourcePassword)
	if err != nil {
		return err
	}
	s.runtime.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "servers": len(cfg.Servers), "jumphosts": len(cfg.JumpHosts)})
	return nil
}

func (s *Server) mcpConfigs(w http.ResponseWriter, r *http.Request) error {
	port, err := GetPort()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	commandName := executable
	var args []string
	if strings.Contains(filepath.Clean(executable), string(filepath.Separator)+"go-build") {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			commandName = "go"
			args = []string{"-C", cwd, "run", "."}
		}
	}
	command := map[string]any{"command": commandName}
	vscodeCommand := map[string]any{"type": "stdio", "command": commandName}
	if len(args) > 0 {
		command["args"] = args
		vscodeCommand["args"] = args
	}
	executableCommand := strings.TrimSpace(commandName + " " + strings.Join(args, " "))
	url := fmt.Sprintf("http://127.0.0.1:%d/mcp/", port)
	writeJSON(w, http.StatusOK, map[string]any{"executable": executableCommand, "port": port, "claude": map[string]any{"mcpServers": map[string]any{"ssh-mcp": command}}, "cursor": map[string]any{"mcpServers": map[string]any{"ssh-mcp": command}}, "vscode": map[string]any{"servers": map[string]any{"ssh-mcp": vscodeCommand}}, "http": map[string]any{"mcpServers": map[string]any{"ssh-mcp": map[string]any{"type": "streamable-http", "url": url}}}})
	return nil
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "configured": s.store.Exists(), "time": time.Now().UTC().Format(time.RFC3339Nano)})
	return nil
}

func toMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return result
}
