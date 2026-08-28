package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ssh-mcp/ssh-mcp-go/internal/integration"
)

func newIntegrationManager() (*integration.Manager, error) {
	command, err := integrationServerCommand()
	if err != nil {
		return nil, err
	}
	return integration.NewManager(integration.Options{ServerCommand: command})
}

func integrationServerCommand() ([]string, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := []string{executable, "--mcp"}
	if strings.Contains(filepath.Clean(executable), string(filepath.Separator)+"go-build") {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			command = []string{"go", "-C", cwd, "run", ".", "--mcp"}
		}
	}
	return command, nil
}

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) error {
	if err := s.requireAuth(r); err != nil {
		return err
	}
	command := s.integrations.ServerCommand()
	writeJSON(w, http.StatusOK, map[string]any{
		"executable": strings.Join(command, " "),
		"tools":      s.integrations.Detect(),
	})
	return nil
}

func (s *Server) configureIntegrations(w http.ResponseWriter, r *http.Request) error {
	if err := s.requireAuth(r); err != nil {
		return err
	}
	var input struct {
		Tools []string `json:"tools"`
	}
	if err := decodeIntegrationRequest(r, &input); err != nil {
		return err
	}
	if len(input.Tools) == 0 {
		return errors.New("请至少选择一个已安装的 AI 工具")
	}
	if len(input.Tools) > 16 {
		return errors.New("一次最多配置 16 个 AI 工具")
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": s.integrations.Configure(r.Context(), input.Tools)})
	return nil
}

func decodeIntegrationRequest(r *http.Request, target any) error {
	const maxBody = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("读取请求失败: %w", err)
	}
	if len(data) > maxBody {
		return errors.New("请求内容过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求 JSON 格式不正确: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("请求 JSON 包含多余内容")
	}
	return nil
}
